/**
 * The text a turn produced.
 *
 * One turn is not one entry. The model writes a text block, calls a tool, writes another, and the
 * submission's `answer` points at the *last* of those entries — so reading that one entry alone
 * drops everything the model said before its final tool call. A turn that ends with
 * "report_done" plus a one-line follow-up would deliver the one line and swallow the analysis.
 *
 * Hence: read the run as a range, from the entry it started at to the answer entry.
 */

import { AssistantEntry, type ConversationId, type Cursor, type EntryId, type Tx } from "@earendil-works/pi-durable";

/** An entry as stored by pi-durable: the messages a step appended, if it appended any. */
export type AnswerEntry = { readonly model?: unknown };

/** The text blocks of one entry, in order. Tool results and user text are not answer text. */
export function textPartsOf(entry: AnswerEntry | undefined): string {
  const messages = (entry?.model ?? []) as unknown as Array<Record<string, unknown>>;
  const parts: string[] = [];
  for (const message of messages) {
    if (message["role"] !== "assistant") continue;
    const content = message["content"];
    if (!Array.isArray(content)) continue;
    for (const part of content as Array<Record<string, unknown>>) {
      if (part["type"] === "text" && typeof part["text"] === "string") parts.push(part["text"]);
    }
  }
  return parts.join("").trim();
}

/** Every text block of a run, oldest first, blank line between blocks. */
export function answerTextOf(entries: readonly AnswerEntry[]): string {
  const blocks: string[] = [];
  for (const entry of entries) {
    const text = textPartsOf(entry);
    if (text.length > 0) blocks.push(text);
  }
  return blocks.join("\n\n").trim();
}

/**
 * One run's text, read inside an open transaction. `fromId` is the entry the run started at (the
 * message that triggered it): with it, every text block from there to the answer is joined.
 * Without it — a "continue" run places no entry of its own — only the answer entry is read, which
 * is right because the earlier text was already delivered.
 *
 * Entries are paged: a step can append several (text, tool call, tool result) and a run that hit
 * the step limit is longer still.
 */
export async function answerTextInTx(
  tx: Tx,
  conversationId: ConversationId,
  answerId: EntryId,
  fromId: EntryId | undefined,
): Promise<string> {
  const last = answerId as unknown as number;
  const first = fromId === undefined ? undefined : (fromId as unknown as number);
  if (first === undefined || first >= last) {
    const entry = await tx.entry(AssistantEntry, answerId);
    return textPartsOf(entry as unknown as AnswerEntry);
  }
  const entries: AnswerEntry[] = [];
  let cursor: Cursor | undefined;
  for (let page = 0; page < 100; page += 1) {
    const scanned = await tx.scanEntries(
      { conversationId, minEntryId: first as never, maxEntryId: last as never, order: "ascending" },
      200,
      cursor,
    );
    entries.push(...(scanned.items as unknown as AnswerEntry[]));
    cursor = scanned.next;
    if (cursor === undefined) break;
  }
  return answerTextOf(entries);
}
