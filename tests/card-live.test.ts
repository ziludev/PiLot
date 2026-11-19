import assert from "node:assert/strict";
import { test } from "node:test";

test("a running pipeline's stage sits above the tool rounds", async () => {
  const { renderLive } = await import("../src/feishu.ts");
  const live = { text: "", tools: [], rounds: [], thinking: "", at: Date.now() };
  const trail = {
    rounds: [{ at: Date.now() - 3_000, label: "bash", ok: true, ms: 3_000 }],
    steps: 3,
  };
  const out = renderLive(live as never, undefined, trail as never, ["▸ 按设计文档实现代码 · 2m 18s"]);
  const stage = out.indexOf("▸ 按设计文档实现代码");
  const round = out.indexOf("bash");
  assert.ok(stage >= 0, `no stage line in:\n${out}`);
  assert.ok(stage < round, `stage should come before the tool rounds:\n${out}`);
});

test("the model's thinking rides above its answer, clipped to a line", async () => {
  const { renderLive } = await import("../src/feishu.ts");
  const long = `${"推敲一下分支名。".repeat(60)}结论：先删孤儿。`;
  const out = renderLive({ text: "好了", tools: ["bash"], thinking: long, busy: true } as never);
  const thought = out.indexOf("💭");
  assert.ok(thought >= 0, `no thinking on the card:\n${out}`);
  assert.ok(thought < out.indexOf("好了"), `thinking should sit above the answer:\n${out}`);
  assert.ok(out.includes("结论：先删孤儿。"), `the newest end is what survives the clip:\n${out}`);
  assert.equal(out.split("\n").filter((line) => line.includes("💭")).length, 1, "one line, not a wall");
});

test("a card with no thinking says so instead of showing an empty thought", async () => {
  const { renderLive } = await import("../src/feishu.ts");
  // Providers that withhold reasoning send redacted thinking: no words, and no claim either.
  const out = renderLive({ text: "", tools: ["bash"], busy: true } as never);
  assert.equal(out.includes("💭"), false);
  assert.match(out, /⚙️ bash/);
});
