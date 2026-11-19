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

/** Tool calls one entry asked for, in order. A block followed by calls is narration, not prose. */
export function toolCallsOf(entry: AnswerEntry | undefined): string[] {
  const messages = (entry?.model ?? []) as unknown as Array<Record<string, unknown>>;
  const names: string[] = [];
  for (const message of messages) {
    if (message["role"] !== "assistant") continue;
    const content = message["content"];
    if (!Array.isArray(content)) continue;
    for (const part of content as Array<Record<string, unknown>>) {
      if (part["type"] === "toolCall" && typeof part["name"] === "string") names.push(part["name"]);
    }
  }
  return names;
}

/** A run's text blocks, each with the tool calls it was followed by. */
export type AnswerBlock = { readonly text: string; readonly tools: readonly string[] };

/** The run as blocks, oldest first, entries without text dropped. */
export function answerBlocksOf(entries: readonly AnswerEntry[]): AnswerBlock[] {
  const blocks: AnswerBlock[] = [];
  for (const entry of entries) {
    const text = textPartsOf(entry);
    if (text.length > 0) blocks.push({ text, tools: toolCallsOf(entry) });
  }
  return blocks;
}

/**
 * First line of a block, without the colon it was about to explain.
 *
 * A hard cut at 70 characters ended mid-sentence — "…超时（8 秒上限…" with the bold marker left
 * dangling — which reads as broken output rather than a summary. So emphasis is stripped first and
 * the cut lands on a sentence end when the line has one, otherwise on the last clause boundary.
 */
function headline(text: string, limit = 120): string {
  const plain = text.replace(/\*\*/g, "").replace(/`/g, "");
  const line = plain.split("\n").map((item) => item.trim()).find((item) => item.length > 0) ?? "";
  const bare = line.replace(/[：:]$/, "");
  if (bare.length <= limit) return bare;
  // A sentence that ends in the first ~two thirds is the whole point of the line; keep it intact.
  const sentences = [...bare.matchAll(/[。！？!?]/g)].map((hit) => hit.index ?? 0);
  const inside = sentences.filter((at) => at + 1 <= limit && at + 1 >= limit / 3).pop();
  if (inside !== undefined) return bare.slice(0, inside + 1);
  const clause = [...bare.slice(0, limit).matchAll(/[，、；,;]/g)].map((hit) => hit.index ?? 0).pop();
  const cut = clause !== undefined && clause >= limit / 2 ? clause : limit - 1;
  return `${bare.slice(0, cut)}…`;
}

/**
 * The answer as the card should read it: conclusion first.
 *
 * A turn narrates while it works — "现在把清理返回值改成实际删除数：" — and calls a tool, so the
 * joined text opens with four headings whose bodies were tool calls. Next to the user's question
 * that reads as a broken answer. So the last block becomes the answer, and the narration follows
 * as one condensed line per block, naming the call it led to.
 *
 * When the last block is short ("已在 release/test 上"), the earlier text is the substance — the
 * shape that used to swallow answers — and nothing is reordered.
 */
export function composeAnswer(blocks: readonly AnswerBlock[]): string {
  const withText = blocks.filter((block) => block.text.length > 0);
  if (withText.length <= 1) return withText.map((block) => block.text).join("\n\n").trim();
  const last = withText[withText.length - 1] as AnswerBlock;
  // Below this the final block is a sign-off ("已部署"), not an answer: the text above it is what
  // the user is waiting for, so it keeps its place.
  if (last.text.length < 40) return withText.map((block) => block.text).join("\n\n").trim();
  const narration = withText.slice(0, -1);
  const lines = narration.map((block) => {
    const did = block.tools.length > 0 ? `（${block.tools.join(" / ")}）` : "";
    return `· ${headline(block.text)}${did}`;
  });
  // "过程 1 段：" in front of a single line is a header for nothing.
  const label = narration.length === 1 ? "_过程：_" : `_过程 ${narration.length} 段：_`;
  return `${last.text}\n\n${label}\n${lines.join("\n")}`;
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
  return answerTextOf(await entriesInTx(tx, conversationId, answerId, fromId as EntryId));
}

/** The entries of a run, oldest first, across pages. */
async function entriesInTx(
  tx: Tx,
  conversationId: ConversationId,
  answerId: EntryId,
  fromId: EntryId,
): Promise<AnswerEntry[]> {
  const last = answerId as unknown as number;
  const first = fromId as unknown as number;
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
  return entries;
}

/** One run's blocks, read inside an open transaction: what composeAnswer needs to put the
 * conclusion first. */
export async function answerBlocksInTx(
  tx: Tx,
  conversationId: ConversationId,
  answerId: EntryId,
  fromId: EntryId | undefined,
): Promise<AnswerBlock[]> {
  const last = answerId as unknown as number;
  if (fromId === undefined || (fromId as unknown as number) >= last) {
    const entry = await tx.entry(AssistantEntry, answerId);
    return answerBlocksOf([entry as unknown as AnswerEntry]);
  }
  return answerBlocksOf(await entriesInTx(tx, conversationId, answerId, fromId));
}

/**
 * The one line a notification can carry.
 *
 * A card patch raises no Feishu notification, so the ping is all that reaches a locked phone —
 * and "✅ 答完了 · 62s" says something finished without saying what it said, which is the reason
 * to look. Take the first line worth reading, drop the markup a plain text message would print
 * literally, and cut it to what a lock screen shows.
 */
export function answerHead(answer: string, limit = 40): string {
  let inFence = false;
  for (const raw of answer.split("\n")) {
    const line = raw.trim();
    // A fence is a command or a log; either way it is not the answer. Skip to the closing fence.
    if (line.startsWith("```")) {
      inFence = !inFence;
      continue;
    }
    if (inFence || line === "" || /^[-=*|]+$/.test(line)) continue;
    const plain = line
      .replace(/^#{1,6}\s*/, "")
      .replace(/^[-*+>]\s*/, "")
      .replace(/\*\*|`|~~/g, "")
      .replace(/\s*\|\s*/g, " · ")
      .replace(/^[·\s]+|[·\s]+$/g, "")
      .trim();
    if (plain === "") continue;
    return plain.length <= limit ? plain : `${plain.slice(0, limit)}…`;
  }
  return "";
}
