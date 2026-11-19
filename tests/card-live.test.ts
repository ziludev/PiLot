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
