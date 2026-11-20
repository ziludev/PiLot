import assert from "node:assert/strict";
import { test } from "node:test";
import { DRY_STEPS, forgetStreak, noteCall, stallWarning } from "../src/stall.ts";

const CALLS = 1;

test("a long read-through is left alone until it stops being a read-through", () => {
  forgetStreak(CALLS);
  let warned: number[] = [];
  for (let call = 1; call <= DRY_STEPS * 2; call += 1) {
    const out = noteCall(CALLS, "read", 1_000 + call);
    if (out.warned) warned.push(out.dry);
  }
  assert.deepEqual(warned, [DRY_STEPS, DRY_STEPS + 20, DRY_STEPS + 40], "says it, then says it again every 20");
});

test("writing something puts the count back to zero", () => {
  forgetStreak(CALLS);
  for (let call = 0; call < DRY_STEPS; call += 1) noteCall(CALLS, "bash", 1_000 + call);
  assert.equal(noteCall(CALLS, "edit", 2_000).dry, 0);
  // The warning does not fire on the first dry call after a write, however late the run is.
  assert.equal(noteCall(CALLS, "read", 2_001).warned, false);
});

test("a pause between calls is a new run, not the same streak", () => {
  forgetStreak(CALLS);
  for (let call = 0; call < DRY_STEPS - 1; call += 1) noteCall(CALLS, "read", 0);
  const later = noteCall(CALLS, "read", 11 * 60_000);
  assert.equal(later.dry, 1, "an hour-old habit says nothing about this turn");
  assert.equal(later.warned, false);
});

test("what the run is told names the number and asks for a wrap-up, not a new tool", () => {
  const line = stallWarning(DRY_STEPS);
  assert.match(line, new RegExp(`连续 ${DRY_STEPS} 步只读没写`));
  assert.match(line, /停下来说清卡在哪/);
  assert.doesNotMatch(line, /bash/, "it must not read as if alex asked");
});

test("one conversation's streak does not count another's", () => {
  forgetStreak(7);
  forgetStreak(8);
  for (let call = 0; call < DRY_STEPS - 1; call += 1) noteCall(7, "read", 1_000);
  assert.equal(noteCall(8, "read", 1_001).dry, 1);
});
