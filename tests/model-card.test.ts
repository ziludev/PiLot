import assert from "node:assert/strict";
import { test } from "node:test";
import { formatTokens, parseModelCommand, turnMeta } from "../src/feishu.ts";

/**
 * `/model` in the Feishu chat, and the one line under every answer card that says what it cost.
 * Both are small, and both are the kind of thing that silently drifts, so the shapes are pinned.
 */

test("/model forms", () => {
  assert.deepEqual(parseModelCommand(""), { kind: "list" });
  assert.deepEqual(parseModelCommand("   "), { kind: "list" });
  assert.deepEqual(parseModelCommand("vendor/model-pro"), {
    kind: "set",
    modelId: "vendor/model-pro",
  });
  assert.deepEqual(parseModelCommand("default auto/glm-5.3"), { kind: "default", modelId: "auto/glm-5.3" });
  assert.deepEqual(parseModelCommand("thinking high"), { kind: "thinking", level: "high" });
  // A model id that merely starts with a keyword is still an id.
  assert.deepEqual(parseModelCommand("defaultish/x"), { kind: "set", modelId: "defaultish/x" });
});

test("tokens read as a person would write them", () => {
  assert.equal(formatTokens(0), "0");
  assert.equal(formatTokens(940), "940");
  assert.equal(formatTokens(12_345), "12.3k");
  assert.equal(formatTokens(1_000_000), "1000.0k");
});

test("the card line carries the tick and how many tools ran", () => {
  const line = turnMeta({
    tools: 14,
    tokens: 12_345,
    costUsd: 0.0312,
    model: "deepseek-flash",
    thinking: "high",
  });
  // Neither a duration (the "答完了" ping right below carries it) nor a ledger: tokens, money and the
  // model id are in the log line and in the cost_usd metric, not under the answer.
  assert.equal(line, "✅ · 14 个工具");
});

test("a group card repeats neither the tick nor the ledger", () => {
  // 同事要的是答案，不是账单：把 tokens 和 $0.29 摆在群里，看起来就像机器人自言自语。
  // 勾也不用留：绿色表头已经写了「✅ 答完了」，话题里下面那条 ping 还会再说一次。
  const line = turnMeta({
    tools: 14,
    tokens: 12_345,
    costUsd: 0.0312,
    model: "deepseek-flash",
    thinking: "high",
    quiet: true,
  });
  assert.equal(line, "");
});

test("a turn that ran no tools leaves no line at all", () => {
  const line = turnMeta({ tools: 0, tokens: 0, costUsd: 0, model: "", thinking: "" });
  assert.equal(line, "");
});
