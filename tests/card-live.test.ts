import assert from "node:assert/strict";
import { test } from "node:test";

test("a running pipeline's stage sits above the tool round", async () => {
  const { renderLive } = await import("../src/feishu.ts");
  const live = { text: "", tools: [], thinking: "", at: Date.now() };
  const trail = {
    rounds: [{ at: Date.now() - 3_000, label: "bash", ok: true, ms: 3_000, hint: "git log --oneline -3" }],
    since: Date.now(),
    key: "bash",
    label: "read",
    hint: "src/feishu.ts",
  };
  const out = renderLive(live as never, trail as never, ["▸ 按设计文档实现代码 · 2m 18s"]);
  const stage = out.indexOf("▸ 按设计文档实现代码");
  const round = out.indexOf("read");
  assert.ok(stage >= 0, `no stage line in:\n${out}`);
  assert.ok(stage < round, `stage should come before the tool round:\n${out}`);
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
  assert.match(out, /⌨️ bash/);
});

// The card used to stack up to five finished rounds above the answer, which on a phone meant the
// screen was history and no line said what was happening now. One round earns a line; the count of
// finished ones is in the subtitle (`第 3 步`).
test("only the round that is running earns a line on the live card", async () => {
  const { renderLive } = await import("../src/feishu.ts");
  const live = { text: "", tools: [], thinking: "", at: Date.now() };
  const trail = {
    rounds: [
      { label: "bash", ms: 2_000, hint: "git log --oneline -3" },
      { label: "bash", ms: 3_000, hint: "curl -s https://example.com" },
    ],
    since: Date.now(),
    key: "bash",
    label: "bash",
    hint: "cd client-go && go test ./...",
    tail: "ok pilot/internal/app",
  };
  const out = renderLive(live as never, trail as never);
  assert.match(out, /🧪 bash `cd client-go && go test \.\/\.\.\.`/);
  assert.doesNotMatch(out, /git log --oneline -3/, `a finished round is history, not the live card:\n${out}`);
  assert.doesNotMatch(out, /curl -s/, `a finished round is history, not the live card:\n${out}`);
});

test("a clipped thought starts on a sentence, not mid-markdown", async () => {
  const { renderLive } = await import("../src/feishu.ts");
  // What a reader saw: "…**, trigger 是 aichat 生成时带的…" — a slice that opened inside `**`.
  const long = `${"推敲一下分支名。".repeat(60)}**结论**：先删孤儿。`;
  const out = renderLive({ text: "", tools: [], thinking: long, busy: true } as never);
  const line = out.split("\n").find((entry) => entry.includes("💭")) ?? "";
  assert.ok(line.length > 0, `no thought on the card:\n${out}`);
  assert.ok(!line.includes("*"), `emphasis markers survived the clip: ${line}`);
  assert.match(line, /💭 …[\s\S]*结论：先删孤儿。_$/);
});
