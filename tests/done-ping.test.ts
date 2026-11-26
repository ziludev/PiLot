import assert from "node:assert/strict";
import { test } from "node:test";
import { shouldPingDone } from "../src/feishu.ts";

/**
 * A done-ping exists because patching a card raises no Feishu notification. The rule is one
 * function so the live path, the postponed-answer path and the after-restart recovery cannot drift
 * apart again — recovery used to compare `elapsed < seconds * 1000`, which with `0` pinged always.
 */

test("a short run leaves the card alone", () => {
  assert.equal(shouldPingDone(45, 44_999), false);
  assert.equal(shouldPingDone(45, 45_000), true);
});

test("zero or negative turns pings off, even for an hours-long gap", () => {
  assert.equal(shouldPingDone(0, 6 * 3_600_000), false);
  assert.equal(shouldPingDone(-1, 6 * 3_600_000), false);
});
