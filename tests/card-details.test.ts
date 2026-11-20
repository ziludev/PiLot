import assert from "node:assert/strict";
import { test } from "node:test";
import { detailPanel } from "../src/feishu.ts";

/**
 * The folded panel is the only place the owner can see why a turn paused or how it ended, so the
 * trail has to survive the trip into card elements — the card's markdown subset drops more than it
 * shows.
 */

type Panel = {
  header: { title: { content: string } };
  elements: Array<{ tag: string; content?: string } & Record<string, unknown>>;
};

const panel = (details: Parameters<typeof detailPanel>[0]): Panel => detailPanel(details) as unknown as Panel;
const text = (value: Panel): string => value.elements.map((element) => element.content ?? "").join("\n");

test("the trail is written above the steps, and the header counts it", () => {
  const card = panel({
    steps: [{ name: "bash", ok: true, preview: "npm test → 126 pass" }],
    seconds: 171,
    costUsd: 0.0281,
    decisions: [{ at: new Date(2026, 0, 2, 9, 5).getTime(), kind: "approval", text: "你允许了 write · 等了 2 分钟" }],
  });
  assert.match(card.header.title.content, /1 步 · 171s · \$0\.0281 · 1 次决策/);
  const body = text(card);
  assert.ok(body.indexOf("决策") < body.indexOf("bash"), "the reason comes before the work");
  assert.match(body, /09:05 你允许了 write · 等了 2 分钟/);
});

test("no decisions means no heading and no count", () => {
  const card = panel({ steps: [{ name: "read", ok: false, preview: "" }], seconds: 3, costUsd: 0 });
  assert.doesNotMatch(card.header.title.content, /决策/);
  assert.doesNotMatch(text(card), /决策/);
  assert.match(text(card), /（无输出）/, "an empty preview still says something");
});

/** A turn of `count` steps, each with `size` characters of output. */
const long = (count: number, size: number) => ({
  steps: Array.from({ length: count }, (_, index) => ({
    name: index % 5 === 4 ? "read" : "bash",
    ok: true,
    preview: `${index}`.padEnd(size, "·"),
  })),
  seconds: 600,
  costUsd: 0.2,
});

test("a 60-step turn lists the last 40 and counts the rest by tool", () => {
  const body = text(panel(long(60, 20)));
  assert.match(body, /^更早的 20 步：bash ×16、read ×4/);
  // Numbering keeps counting from the first step of the turn, so "跳到了 43 步" still means something.
  assert.match(body, /21\. ✅/);
  assert.doesNotMatch(body, /^1\. /m, "the dropped steps are not renumbered from one");
});

test("a short turn is listed whole, with no count line", () => {
  const body = text(panel(long(5, 20)));
  assert.doesNotMatch(body, /更早的/);
  assert.match(body, /1\. ✅/);
  assert.match(body, /5\. ✅/);
});

test("previews are spent on the newest steps; older ones keep only their headline", () => {
  const body = text(panel(long(40, 200)));
  assert.ok(body.includes("（输出略）"), "the oldest steps give up their output");
  assert.ok(body.includes("39".padEnd(200, "·")), "the newest step keeps its output");
  // The panel is bounded: 40 × 200 would be 8000 characters of output on its own.
  assert.ok(body.length < 6000 + 40 * 120, `panel body was ${body.length} chars`);
});

test("an earlier step under budget still says it printed nothing", () => {
  const body = text(panel({ steps: [{ name: "read", ok: true, preview: "" }], seconds: 2, costUsd: 0 }));
  assert.match(body, /（无输出）/);
});
