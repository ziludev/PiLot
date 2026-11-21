import assert from "node:assert/strict";
import { test } from "node:test";
import type { Message } from "@earendil-works/pi-ai";
import {
  ELIDE_MIN_TOKENS,
  KEEP_RECENT,
  elideOldToolOutputs,
  trimMiddle,
} from "../src/context-guard.ts";

/**
 * A long diagnosis keeps every tool result in the request until compaction. The guard trims the
 * older *reference* results to their head and tail — never the recent ones, never an edit's output,
 * and only once a request is already large, so an ordinary turn is not rewritten.
 */

const toolResult = (id: string, chars: number, toolName = "bash"): Message =>
  ({
    role: "toolResult",
    toolCallId: id,
    toolName,
    content: [{ type: "text", text: `start-${id}-` + "x".repeat(Math.max(0, chars - 20)) + `-end-${id}` }],
  }) as unknown as Message;

const user = { role: "user", content: "hi" } as unknown as Message;

const textOf = (message: Message): string =>
  JSON.stringify((message as { content?: unknown }).content ?? "");

test("a small request is left alone", () => {
  const messages = [user, ...Array.from({ length: 20 }, (_, index) => toolResult(`c${index}`, 1_000))];
  const { messages: next, elided } = elideOldToolOutputs(messages);
  assert.equal(elided, 0);
  assert.equal(next.length, messages.length);
  assert.ok(ELIDE_MIN_TOKENS > 0);
});

test("older reference results are trimmed to head and tail; the recent ones are untouched", () => {
  const total = KEEP_RECENT + 6;
  const messages = [user, ...Array.from({ length: total }, (_, i) => toolResult(`c${i}`, 8_000))];
  const { messages: next, elided } = elideOldToolOutputs(messages);
  assert.equal(elided, 6, "only the six beyond the kept window");
  const first = textOf(next[1] as Message);
  assert.match(first, /此处省略约/);
  assert.match(first, /start-c0-/);
  assert.match(first, /-end-c0/);
  const last = next[next.length - 1] as Message;
  assert.equal(textOf(last).includes("此处省略"), false, "the newest result is untouched");
});

test("an old result that was already short is kept as it is", () => {
  const messages = [user, ...Array.from({ length: KEEP_RECENT + 1 }, (_, i) => toolResult(`c${i}`, 4_000))];
  messages[1] = toolResult("small", 50);
  const { elided } = elideOldToolOutputs(messages);
  assert.equal(elided, 0, "nothing worth trimming");
});

test("a non-reference tool's output is never trimmed", () => {
  const total = KEEP_RECENT + 6;
  const messages = [user, ...Array.from({ length: total }, (_, i) => toolResult(`e${i}`, 8_000, "edit"))];
  const { elided } = elideOldToolOutputs(messages);
  assert.equal(elided, 0);
});

test("trimMiddle keeps both ends and never leaves half a surrogate pair", () => {
  const long = "😀".repeat(3_000);
  const trimmed = trimMiddle(long);
  assert.ok(trimmed.length < long.length);
  assert.ok(trimmed.startsWith("😀"));
  assert.ok(trimmed.endsWith("😀"));
  assert.equal(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/.test(trimmed), false);
  assert.equal(trimMiddle("short"), "short");
});
