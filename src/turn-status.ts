/**
 * How each conversation's last turn ended, read straight from the store.
 *
 * The rail can say "this chat is working" from the task list and "an answer arrived" from the unread
 * set, but a turn that failed looks exactly like one that answered: both end with something the
 * client has not read yet. The client only holds entries for the chat it has open, so a background
 * chat that blew up leaves no sign of it on the row. One indexed query over the newest entries of
 * every listed conversation is what lets the rail mark the failure.
 *
 * Read-only, one connection per call: the bridge never writes here, and a stale read only costs a
 * row that keeps its old mark.
 */
import { DatabaseSync } from "node:sqlite";

/** The stop reasons worth showing on a row: the turn ended without an answer. */
export type LastStop = "error" | "aborted";

/**
 * How many of a conversation's newest entries to look through for its last assistant reply. A turn
 * can end with a handful of tool results after the assistant message, so the newest entry is not the
 * one that says how it went.
 */
const TAIL = 8;

/** The assistant message's stopReason: the last message in the list that carries one. */
function stopOf(model: unknown): string | null {
  if (!Array.isArray(model)) return null;
  for (let index = model.length - 1; index >= 0; index -= 1) {
    const message = model[index] as { stopReason?: unknown } | null;
    const reason = typeof message?.stopReason === "string" ? message.stopReason : "";
    if (reason.length > 0) return reason;
  }
  return null;
}

/**
 * The conversations whose last assistant reply stopped on an error or an abort. Anything else — a
 * finished answer, a turn still calling tools, no reply at all — is absent from the map.
 */
export function lastStops(dbPath: string, conversationIds: readonly number[]): Map<number, LastStop> {
  const stops = new Map<number, LastStop>();
  if (conversationIds.length === 0) return stops;
  let db: DatabaseSync;
  try {
    db = new DatabaseSync(dbPath, { readOnly: true });
  } catch {
    // No store yet (or a locked file): no marks, which is what the client saw before this existed.
    return stops;
  }
  try {
    const placeholders = conversationIds.map(() => "?").join(",");
    const rows = db
      .prepare(
        `select e.conversation_id as conversationId,
                json_extract(e.record, '$.kind') as kind,
                json_extract(e.record, '$.model') as model
           from entries e
          where e.conversation_id in (${placeholders})
            and e.id in (select x.id from entries x
                          where x.conversation_id = e.conversation_id
                          order by x.id desc
                          limit ${TAIL})
          order by e.conversation_id, e.id desc`,
      )
      .all(...conversationIds) as Array<{ conversationId: number; kind?: string; model?: string }>;

    // Newest first per conversation, so the first assistant entry met is the one that ended the turn.
    const seen = new Set<number>();
    for (const row of rows) {
      if (row.kind !== "pi.assistant" || seen.has(row.conversationId)) continue;
      seen.add(row.conversationId);
      let model: unknown;
      try {
        model = row.model === undefined ? undefined : JSON.parse(row.model);
      } catch {
        continue;
      }
      const reason = stopOf(model);
      if (reason === "error" || reason === "aborted") stops.set(row.conversationId, reason);
    }
  } catch {
    // A schema this build does not know is not worth a 500 on the state endpoint.
    return stops;
  } finally {
    db.close();
  }
  return stops;
}
