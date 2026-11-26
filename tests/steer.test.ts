import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

// `/steer` is the only way a message skips the queue and bends a run that is already going, so the
// parse has to be exact: a false positive steers a run nobody meant to steer, a false negative
// quietly queues the message instead.
test("a steer command carries its text, in either spelling", async () => {
  const { parseSteer } = await import("../src/feishu.ts");
  assert.equal(parseSteer("/steer 别动 prod 库"), "别动 prod 库");
  assert.equal(parseSteer("/插话 先跑测试"), "先跑测试");
  assert.equal(parseSteer("/STEER  multi\nline "), "multi\nline");
});

// The command alone is not a steer with empty text: it gets a usage line, not a run.
test("the command without text is its own case", async () => {
  const { parseSteer } = await import("../src/feishu.ts");
  assert.equal(parseSteer("/steer"), "");
  assert.equal(parseSteer("/插话   "), "");
});

// Anything else is a message, including words that merely start with the letters.
test("ordinary messages are not steers", async () => {
  const { parseSteer } = await import("../src/feishu.ts");
  assert.equal(parseSteer("steer 一下"), undefined);
  assert.equal(parseSteer("/steering 要不要加按钮"), undefined);
  assert.equal(parseSteer("/steerx"), undefined);
  assert.equal(parseSteer("用 /steer 挺好"), undefined);
});

test("/steer handler runs independently of /cost in src/feishu.ts", () => {
  const source = readFileSync(new URL("../src/feishu.ts", import.meta.url), "utf8");
  const costIndex = source.indexOf("if (/^\\/(cost|花费|额度)$/i.test(command))");
  const steerIndex = source.indexOf("const steerText = parseSteer(command);");
  assert.ok(costIndex > 0, "expected /cost branch");
  assert.ok(steerIndex > costIndex, "expected /steer branch after /cost");
  const between = source.slice(costIndex, steerIndex);
  assert.match(between, /return;\s*\}/, "/cost branch must return and close before parseSteer");
});
