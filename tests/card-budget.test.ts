import assert from "node:assert/strict";
import { test } from "node:test";
import {
  card,
  CARD_BUDGET_BYTES,
  CARD_PANEL_STEPS,
  chunkMarkdown,
  inQuietHours,
  MAX_MARKDOWN_CHARS,
  noticeHeader,
  splitAnswerForCards,
} from "../src/feishu.ts";
import type { TurnDetails } from "../src/feishu.ts";

const bytes = (value: unknown): number => Buffer.byteLength(JSON.stringify(value), "utf8");

function steps(count: number): TurnDetails["steps"] {
  return Array.from({ length: count }, (_, index) => ({
    name: "bash",
    hint: `git status --short --branch 第 ${index} 次`,
    ok: index % 3 !== 0,
    preview: "输出行 ".repeat(120),
    seconds: 1,
  }));
}

function details(count: number): Omit<TurnDetails, "at"> {
  return { steps: steps(count), seconds: 42, costUsd: 0.01, decisions: [] };
}

test("a paragraph longer than one markdown element is cut, not truncated by the client", () => {
  const paragraph = Array.from({ length: 400 }, (_, index) => `第 ${index} 行，说一件小事。`).join("\n");
  const pieces = chunkMarkdown(paragraph);
  assert.ok(pieces.length > 1);
  for (const piece of pieces) assert.ok(piece.length <= MAX_MARKDOWN_CHARS, `piece ${piece.length}`);
  // Nothing is lost and no line is broken in half: the pieces join back into the original.
  assert.equal(pieces.join("\n"), paragraph);
});

test("a cut inside a code fence closes it and reopens it, so the rest stays code", () => {
  const body = Array.from({ length: 300 }, (_, index) => `const line${index} = ${index};`);
  const fence = ["```ts", ...body, "```"].join("\n");
  const pieces = chunkMarkdown(fence);
  assert.ok(pieces.length > 1);
  // Every piece but the last is closed, and every piece but the first is reopened.
  for (const piece of pieces.slice(0, -1)) assert.match(piece, /\n```$/);
  for (const piece of pieces.slice(1)) assert.match(piece, /^```ts\n/);
  const kept = pieces.join("\n").split("\n").filter((line) => line !== "```ts" && line !== "```");
  assert.deepEqual(kept, body);
});

test("one line too long to cut anywhere is cut mid-line", () => {
  const line = "x".repeat(7_000);
  const pieces = chunkMarkdown(line);
  assert.ok(pieces.length >= 3);
  for (const piece of pieces) assert.ok(piece.length <= MAX_MARKDOWN_CHARS);
  assert.equal(pieces.join(""), line);
});

test("a short answer stays one element", () => {
  assert.deepEqual(chunkMarkdown("三行\n就够了\n真的"), ["三行\n就够了\n真的"]);
});

test("a card with a huge step panel still fits under the limit and keeps the answer", () => {
  const answer = "结论在这里。".repeat(300);
  const built = card(answer, "1m12s · 0.01 美元", undefined, undefined, details(200));
  assert.ok(bytes(built) <= CARD_BUDGET_BYTES, `card was ${bytes(built)} bytes`);
  const flat = JSON.stringify(built);
  assert.ok(flat.includes(answer.slice(0, 60)), "the answer survives when the panel is what gets cut");
});

test("a card too big even without its panel arrives shorter instead of not at all", () => {
  // 9000 CJK characters are 27KB of JSON before a single element wrapper is added.
  const answer = "很长的回答。".repeat(2_000);
  assert.ok(Buffer.byteLength(answer, "utf8") > CARD_BUDGET_BYTES);
  const built = card(answer);
  assert.ok(bytes(built) <= CARD_BUDGET_BYTES, `card was ${bytes(built)} bytes`);
  assert.match(JSON.stringify(built), /截掉了/);
});

test("a shortened panel keeps the newest steps and drops the output", () => {
  // A long answer plus a 200-step panel is past the budget, so the panel is what gives way.
  const shortened = card("回答。".repeat(1_700), undefined, undefined, undefined, details(200));
  assert.ok(bytes(shortened) <= CARD_BUDGET_BYTES);
  const built = JSON.stringify(shortened);
  assert.ok(built.includes(`第 ${200 - 1} 次`), "the step that just ran is the one that stays");
  assert.ok(!built.includes("第 0 次"), "the oldest steps are what went");
  assert.equal((built.match(/输出行/g) ?? []).length, 0, "a trimmed panel carries no output");
  assert.ok(CARD_PANEL_STEPS > 0);
});

test("the chat list shows the header, or the first line of the answer", () => {
  const withHeader = card("正文", undefined, undefined, { template: "green", title: "✅ 目标达成" });
  assert.deepEqual((withHeader.config as Record<string, unknown>).summary, { content: "✅ 目标达成" });
  const bare = card("## 先说结论\n\n然后是细节。");
  assert.deepEqual((bare.config as Record<string, unknown>).summary, { content: "先说结论" });
});

test("splitAnswerForCards splits oversized CJK answers into continuation cards without losing text", () => {
  const short = "普通回答。".repeat(20);
  assert.deepEqual(splitAnswerForCards(short), [short]);

  const long = Array.from({ length: 400 }, (_, i) => `第 ${i} 行：详细分析结果。`).join("\n");
  const chunks = splitAnswerForCards(long, 6_000);
  assert.ok(chunks.length >= 2);
  assert.match(chunks[0]!, /1\/\d+，下文续/);
  assert.match(chunks[1]!, /续 2\/\d+/);
  for (const piece of chunks) {
    assert.ok(bytes(card(piece)) <= CARD_BUDGET_BYTES);
  }
});

test("inQuietHours handles overnight and same-day windows in Asia/Shanghai", () => {
  const lateNight = new Date("2026-11-25T23:30:00+08:00");
  const morning = new Date("2026-11-25T09:00:00+08:00");
  const early = new Date("2026-11-25T07:59:00+08:00");

  assert.equal(inQuietHours("22:00-08:00", lateNight), true);
  assert.equal(inQuietHours("22:00-08:00", early), true);
  assert.equal(inQuietHours("22:00-08:00", morning), false);
  assert.equal(inQuietHours("off", lateNight), false);
  assert.equal(inQuietHours("12:00-14:00", new Date("2026-11-25T12:30:00+08:00")), true);
  assert.equal(inQuietHours("12:00-14:00", morning), false);
});

test("noticeHeader maps severity icons to card templates", () => {
  assert.equal(noticeHeader("🔴 storage_failed").template, "red");
  assert.equal(noticeHeader("⚠️ 飞书网关异常").template, "orange");
  assert.equal(noticeHeader("✅ 恢复正常").template, "green");
  assert.equal(noticeHeader("普通进度更新").template, "blue");
});
