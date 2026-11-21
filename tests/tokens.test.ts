import assert from "node:assert/strict";
import { test } from "node:test";
import type { Message } from "@earendil-works/pi-ai";
import { COMPACTION_TRIGGER_CAP, DEFAULT_BACKGROUND_TOKENS, compactionPolicy } from "../src/compaction.ts";
import { messageTokens, textTokens } from "../src/tokens.ts";

/**
 * Chinese is counted at one token per character.
 *
 * The upstream estimate is characters / 3.5, which is right for English and undercounts Chinese
 * about 3.5x. Every threshold we own is in tokens, so the error made them unreachable rather than
 * merely imprecise: the 24k elide threshold needed ~84k characters to fire.
 */

test("english stays at the upstream ratio", () => {
  assert.equal(textTokens("a".repeat(350)), 100);
  assert.equal(textTokens(""), 0);
});

test("chinese counts one token per character", () => {
  assert.equal(textTokens("中".repeat(100)), 100);
  assert.equal(textTokens("中文混合"), 4);
});

test("an emoji counts its two UTF-16 units, not one", () => {
  assert.equal(textTokens("🙂"), 2);
});

test("mixed text adds both halves", () => {
  // 4 Chinese characters (4 tokens) + 7 ASCII characters (ceil(7 / 3.5) = 2)
  assert.equal(textTokens("中文中文abcdef"), 6);
});

function toolResult(text: string): Message {
  return { role: "toolResult", toolCallId: "1", toolName: "bash", content: text } as unknown as Message;
}

test("a chinese tool result is no longer estimated at a third of its cost", () => {
  const chinese = messageTokens(toolResult("中".repeat(7_000)));
  const english = messageTokens(toolResult("a".repeat(7_000)));
  assert.ok(chinese > english * 3, `chinese ${chinese} should dwarf english ${english}`);
  assert.ok(chinese >= 7_000, `chinese ${chinese} should be at least one token per character`);
});

test("the corrected count reaches the elide threshold at the stated size", () => {
  // 24k tokens of Chinese is ~24k characters, not the ~84k the upstream estimate demanded.
  assert.ok(messageTokens(toolResult("中".repeat(24_000))) >= 24_000);
});

test("structured content is counted block by block", () => {
  const message = {
    role: "assistant",
    content: [
      { type: "text", text: "中文".repeat(20) },
      { type: "toolCall", id: "1", name: "bash", arguments: { command: "中文".repeat(20) } },
    ],
  } as unknown as Message;
  assert.ok(messageTokens(message) >= 80, String(messageTokens(message)));
});

test("system sections are counted, not just content", () => {
  const message = {
    role: "system",
    content: "",
    sections: { identity: "中文".repeat(50) },
  } as unknown as Message;
  assert.ok(messageTokens(message) >= 100, String(messageTokens(message)));
});

/**
 * Compaction has to fire before the window is nearly full.
 *
 * With a 1M-token model, `contextWindow - reserveTokens` is 984k and measured requests already
 * carried 185k tokens: the limit was never approached, so the context grew unbounded and every
 * round paid for it.
 */

test("a large window compacts at the cap, not near the window", () => {
  const policy = compactionPolicy(1_000_000);
  const trigger = 1_000_000 - policy.reserveTokens - policy.backgroundTokens;
  assert.equal(trigger, COMPACTION_TRIGGER_CAP);
  // The hard limit and the summary budget must not move with the cap.
  assert.equal(policy.reserveTokens, 16_384);
});

test("a small window keeps the library floor", () => {
  const policy = compactionPolicy(64_000);
  assert.equal(policy.backgroundTokens, DEFAULT_BACKGROUND_TOKENS);
});

test("an unknown window does not produce a negative threshold", () => {
  const policy = compactionPolicy(0);
  assert.equal(policy.backgroundTokens, DEFAULT_BACKGROUND_TOKENS);
  assert.ok(Number.isSafeInteger(policy.backgroundTokens));
});
