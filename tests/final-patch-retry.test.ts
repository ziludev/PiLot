import assert from "node:assert/strict";
import { test } from "node:test";

// A gateway being restarted under a running turn is the common case: `fetch failed`, then a few
// seconds of nothing, then it is back. The final patch is the only delivery with no follower, so
// waiting is what turns "the card is stuck on 处理中" into a slightly late answer.
test("a network failure is retried, with growing delays", async () => {
  const { patchRetryDelay } = await import("../src/feishu.ts");
  const error = new TypeError("fetch failed");
  assert.equal(patchRetryDelay(0, error), 500);
  assert.equal(patchRetryDelay(1, error), 1_500);
  assert.equal(patchRetryDelay(2, error), 4_000);
});

// Three tries and ~6s is enough for a restart and short enough that the answer is still news.
test("after the last delay there is no fourth attempt", async () => {
  const { patchRetryDelay } = await import("../src/feishu.ts");
  assert.equal(patchRetryDelay(3, new Error("fetch failed")), undefined);
  assert.equal(patchRetryDelay(9, new Error("socket hang up")), undefined);
});

// Feishu rejecting the card is the answer, not an accident: 230001 means the message is not a
// card at all, and retrying it just delays the fallback by six seconds.
test("a card Feishu refuses is not retried", async () => {
  const { patchRetryDelay } = await import("../src/feishu.ts");
  assert.equal(patchRetryDelay(0, new Error("patch failed: 230001 This message is NOT a card.")), undefined);
});
