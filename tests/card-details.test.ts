import assert from "node:assert/strict";
import { test } from "node:test";
import { detailPanel } from "../src/feishu.ts";

/**
 * The detail block is the only place the owner can see why a turn paused or how it ended, so the
 * trail has to survive the trip into card elements — the card's markdown subset drops more than it
 * shows.
 */

type Element = { tag: string; content?: string } & Record<string, unknown>;

const panel = (details: Parameters<typeof detailPanel>[0]): Element[] => detailPanel(details);
const text = (elements: Element[]): string => elements.map((element) => element.content ?? "").join("\n");

/** The heading is the line the rule sits under: "本轮执行详情 · 1 步 · 171s · …". */
const heading = (elements: Element[]): string =>
  elements.find((element) => (element.content ?? "").includes("本轮执行详情"))?.content ?? "";

test("the trail is written above the steps, and the header counts it", () => {
  const card = panel({
    steps: [{ name: "bash", ok: true, preview: "npm test → 126 pass" }],
    seconds: 171,
    costUsd: 0.0281,
    decisions: [{ at: new Date(2026, 0, 2, 9, 5).getTime(), kind: "approval", text: "你允许了 write · 等了 2 分钟" }],
  });
  assert.match(heading(card), /1 步 · 171s · \$0\.0281 · 1 次决策/);
  const body = text(card);
  assert.ok(body.indexOf("决策") < body.indexOf("bash"), "the reason comes before the work");
  assert.match(body, /09:05 你允许了 write · 等了 2 分钟/);
});

test("no decisions means no heading and no count", () => {
  const card = panel({ steps: [{ name: "read", ok: false, preview: "" }], seconds: 3, costUsd: 0 });
  assert.doesNotMatch(heading(card), /决策/);
  assert.doesNotMatch(text(card), /决策/);
  assert.match(text(card), /（无输出）/, "an empty preview still says something");
});
