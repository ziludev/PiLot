import assert from "node:assert/strict";
import { test } from "node:test";
import { Routine } from "../src/routines.ts";

/**
 * The round-level behaviour, driven through the real phase handler with a fake runtime: the only
 * thing that matters here is whether a round touches the chat at all. An unchanged watch has to
 * sleep without starting a turn — that is what keeps a routine from spending tokens to say nothing.
 */

type Sleep = { until: number; checkpoint: { phase: string; seen?: string } };

function fakeRuntime(): { runtime: { sleepUntil: (until: number, checkpoint: Sleep["checkpoint"]) => Promise<void> }; sleeps: Sleep[] } {
  const sleeps: Sleep[] = [];
  return {
    sleeps,
    runtime: {
      sleepUntil: async (until, checkpoint) => {
        sleeps.push({ until, checkpoint });
      },
    },
  };
}

const baseTask = (watch: unknown, seen: string | undefined) => ({
  id: 7,
  conversationId: 0,
  input: {
    schedule: { kind: "every", everyMs: 1_800_000 },
    text: "看看 MR 有没有新评论",
    // No surface: the turn branch still runs, but it has nowhere to deliver, so the test never
    // touches a chat. The assertion is about whether that branch was reached at all.
    surfaceId: "",
    anchor: Date.now(),
    ...(watch === undefined ? {} : { watch }),
  },
  state: { status: "running", checkpoint: { phase: "fire", at: Date.now(), ...(seen === undefined ? {} : { seen }) } },
});

test("an unchanged watch sleeps without starting a turn", async () => {
  const { runtime, sleeps } = fakeRuntime();
  const task = baseTask({ cmd: "printf 'no comments'", baseline: "ok\nno comments" }, "ok\nno comments");
  await Routine.definition.phases.fire(task as never, runtime as never, {} as never);
  assert.equal(sleeps.length, 1);
  assert.equal(sleeps[0]!.checkpoint.phase, "wait");
  assert.equal(sleeps[0]!.checkpoint.seen, "ok\nno comments");
  assert.ok(sleeps[0]!.until > Date.now(), "the next round must be in the future");
});

test("a changed watch runs the round and carries the new reading forward", async () => {
  const { runtime, sleeps } = fakeRuntime();
  const task = baseTask({ cmd: "printf '1 new comment'", baseline: "ok\nno comments" }, "ok\nno comments");
  await Routine.definition.phases.fire(task as never, runtime as never, {} as never);
  assert.equal(sleeps.length, 1);
  assert.equal(sleeps[0]!.checkpoint.phase, "wait");
  // The reading stored for next time is the one this round saw, not the one it woke up with.
  assert.equal(sleeps[0]!.checkpoint.seen, "ok\n1 new comment");
});

test("a routine without a watch still runs every round", async () => {
  const { runtime, sleeps } = fakeRuntime();
  const task = baseTask(undefined, undefined);
  await Routine.definition.phases.fire(task as never, runtime as never, {} as never);
  assert.equal(sleeps.length, 1);
  assert.equal(sleeps[0]!.checkpoint.seen, undefined);
});
