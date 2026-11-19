/**
 * Which conversations still exist, read straight from the store.
 *
 * The boot sweep deletes checkouts, so the list of conversations it keeps has to be the list of
 * conversations that exist — not the ones on a Feishu chat, not the ones with a window open. Those
 * are views: a conversation started from the desktop client has neither, and a sweep that asked a
 * view would call it gone and drop its checkout from under a live agent.
 *
 * `undefined` means the store could not be read. That is not the same as "no conversations": every
 * row being unknown is the one answer that must not turn into deleting everything.
 */
import { DatabaseSync } from "node:sqlite";

export function liveConversationIds(dbPath: string): Set<number> | undefined {
  let db: DatabaseSync;
  try {
    db = new DatabaseSync(dbPath, { readOnly: true });
  } catch {
    return undefined;
  }
  try {
    const rows = db.prepare("select id from conversations").all() as Array<{ id: number }>;
    return new Set(rows.map((row) => Number(row.id)));
  } catch {
    // No such table yet: a fresh store, which means nothing old to clean up either way.
    return undefined;
  } finally {
    db.close();
  }
}
