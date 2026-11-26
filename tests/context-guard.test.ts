import assert from "node:assert/strict";
import { test } from "node:test";
import type { Message } from "@earendil-works/pi-ai";
import {
  ELIDE_MIN_TOKENS,
  KEEP_RECENT,
  elideOldToolOutputs,
  forgetContextStats,
  lastRequestMessages,
  lastRequestTokens,
  noteSystemDigest,
  recordRequestStats,
  systemDigest,
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

test("the system prompt is found whether it is a field or a leading message", () => {
  const viaField = systemDigest({ system: "you are a bot" });
  const viaMessage = systemDigest({ messages: [{ role: "system", content: "you are a bot" }, { role: "user", content: "hi" }] });
  const viaInstructions = systemDigest({ instructions: "you are a bot" });
  assert.equal(viaField, viaMessage);
  assert.equal(viaField, viaInstructions);
  assert.notEqual(viaField, systemDigest({ system: "you are a different bot" }));
  assert.notEqual(systemDigest({ system: "a".repeat(9) }), systemDigest({ system: "a".repeat(10) }), "length is part of it");
});

test("request counts, tokens, and system prompt digests stay isolated per conversation", () => {
  forgetContextStats();
  recordRequestStats(101, 420, 35_000);
  recordRequestStats(202, 12, 800);

  assert.equal(lastRequestMessages(101), 420);
  assert.equal(lastRequestTokens(101), 35_000);
  assert.equal(lastRequestMessages(202), 12);
  assert.equal(lastRequestTokens(202), 800);
  // Zero-arg fallback keeps returning the latest conversation's values.
  assert.equal(lastRequestMessages(), 12);

  assert.equal(noteSystemDigest(101, "aaaa1111"), false, "first digest for conv 101");
  assert.equal(noteSystemDigest(202, "bbbb2222"), false, "first digest for conv 202 must not see 101's digest");
  assert.equal(noteSystemDigest(101, "aaaa1111"), false, "unchanged digest for conv 101");
  assert.equal(noteSystemDigest(101, "cccc3333"), true, "changed digest for conv 101");
});
