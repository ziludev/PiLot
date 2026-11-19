import assert from "node:assert/strict";
import { test } from "node:test";

const T0 = 1_000_000;

test("a brief hiccup says nothing", async () => {
  const { gatewayWatchStep } = await import("../src/feishu.ts");
  let state = { misses: 0, alertedAt: 0 };
  for (let i = 0; i < 9; i += 1) {
    const out = gatewayWatchStep(state, false, T0 + i * 30_000);
    assert.equal(out.act, "none");
    state = out.state;
  }
  // ...and one healthy answer puts it back to the start.
  assert.deepEqual(gatewayWatchStep(state, true, T0 + 300_000).state, { misses: 0, alertedAt: 0 });
});

test("five minutes down alerts once, then holds fire for half an hour", async () => {
  const { gatewayWatchStep } = await import("../src/feishu.ts");
  let state = { misses: 0, alertedAt: 0 };
  let alerts = 0;
  for (let i = 0; i < 200; i += 1) {
    const out = gatewayWatchStep(state, false, T0 + i * 30_000);
    state = out.state;
    if (out.act === "alert") alerts += 1;
  }
  // 200 polls of 30s is 100 minutes: one alert at the 10th miss, then a reminder after 30 minutes.
  assert.equal(alerts, 4);
});

test("recovery is announced exactly once", async () => {
  const { gatewayWatchStep } = await import("../src/feishu.ts");
  let state = { misses: 0, alertedAt: 0 };
  for (let i = 0; i < 10; i += 1) state = gatewayWatchStep(state, false, T0 + i * 30_000).state;
  assert.equal(gatewayWatchStep(state, true, T0 + 300_000).act, "recovered");
  const after = gatewayWatchStep(state, true, T0 + 330_000).state;
  assert.equal(gatewayWatchStep(after, true, T0 + 360_000).act, "none");
});

test("nothing ever was down: no recovery message", async () => {
  const { gatewayWatchStep } = await import("../src/feishu.ts");
  assert.equal(gatewayWatchStep({ misses: 3, alertedAt: 0 }, true, T0).act, "none");
});
