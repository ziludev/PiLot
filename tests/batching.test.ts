import assert from "node:assert/strict";
import { test } from "node:test";
import { READ_RUN, batchingHint, forgetReads, noteReadCall } from "../src/batching.ts";

/**
 * The hint existed since 11-21 and was ignored 696 times by 11-24 (476 of them in a single day), so
 * what it says matters as much as when it fires: it has to ask for the next request, not for a habit.
 */

test("a run of reads is counted and the hint fires once, on the sixth", () => {
  forgetReads(7);
  const fired = Array.from({ length: 12 }, (_, index) => noteReadCall(7, "read", 1_000 + index));
  assert.deepEqual(
    fired.map((count, index) => (count === READ_RUN ? index + 1 : 0)).filter((at) => at > 0),
    [6, 12],
  );
});

test("a progress tool ends the run, so a long turn is not warned about forever", () => {
  forgetReads(8);
  for (let index = 0; index < READ_RUN - 1; index += 1) noteReadCall(8, "read", 1_000 + index);
  assert.equal(noteReadCall(8, "edit", 1_020), 0);
  assert.equal(noteReadCall(8, "read", 1_021), 1);
});

test("the hint asks for the next request, not for a habit", () => {
  const hint = batchingHint();
  assert.match(hint, /现在/);
  assert.match(hint, /同一条消息里放多个工具调用/);
  assert.doesNotMatch(hint, /下次/);
});
