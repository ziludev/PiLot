import assert from "node:assert/strict";
import { test } from "node:test";
import type { Harness } from "@earendil-works/pi-durable";
import { dead as deadLetters, discard, drain, redrive, releaseHeld, retryDelay, type DeadEntry, type OutboxEntry } from "../src/outbox.ts";

function entry(text: string, chatId: string, at = 0): OutboxEntry {
  return { id: text, chatId, text, plain: false, createdAt: at, attempts: 0, nextAt: 0 };
}

/** The only part of the harness the outbox touches: one doc, read and written in a commit. */
function fakeHarness(entries: OutboxEntry[]) {
  const state: { entries: OutboxEntry[]; dead?: DeadEntry[] } = { entries, dead: [] };
  const harness = {
    commit: async (fn: (tx: unknown) => Promise<unknown>) => fn({ doc: async () => state }),
  } as unknown as Harness;
  return { harness, state };
}

const context = {} as never;

test("waiting grows with each failure and stops growing", () => {
  assert.deepEqual([0, 1, 2, 3, 4, 9].map(retryDelay), [15_000, 60_000, 300_000, 900_000, 3_600_000, 3_600_000]);
});

test("a chat keeps its order: a failed message blocks the ones behind it, not other chats", async () => {
  const { harness, state } = fakeHarness([entry("a", "c1"), entry("b", "c1"), entry("c", "c2")]);
  const sent: string[] = [];
  const report = await drain(
    harness,
    context,
    async (message) => {
      if (message.text === "a") throw new Error("gateway down");
      sent.push(message.text);
    },
    1_000,
  );

  assert.deepEqual(sent, ["c"], "b must not overtake the failed a");
  assert.equal(report.sent, 1);
  assert.equal(report.left, 2);
  const a = state.entries.find((item) => item.text === "a")!;
  assert.equal(a.attempts, 1);
  assert.equal(a.nextAt, 1_000 + 15_000, "the retry is scheduled, not immediate");
  assert.match(a.lastError ?? "", /gateway down/);
});

test("a cooled-down message holds its chat without being sent", async () => {
  const retrying = { ...entry("a", "c1"), attempts: 1, nextAt: 60_000 };
  const { harness } = fakeHarness([retrying, entry("b", "c1")]);
  const sent: string[] = [];
  await drain(harness, context, async (message) => {
    sent.push(message.text);
  }, 1_000);
  assert.deepEqual(sent, [], "neither goes: a is not due and b is behind it");
});

test("a message that keeps failing is handed back instead of retried forever", async () => {
  const hopeless = { ...entry("a", "c1"), attempts: 7 };
  const { harness, state } = fakeHarness([hopeless]);
  const report = await drain(harness, context, async () => {
    throw new Error("401");
  }, 1_000);
  assert.equal(state.entries.length, 0);
  assert.equal(report.gaveUp.length, 1);
  assert.equal(report.gaveUp[0]!.attempts, 8);
});

// A lost answer used to end at one log line; these four pin the record that replaces it.
test("giving up keeps the message as a dead letter, with why", async () => {
  const hopeless = { ...entry("a", "c1"), attempts: 7 };
  const { harness, state } = fakeHarness([hopeless]);
  await drain(harness, context, async () => {
    throw new Error("401");
  }, 1_000);

  const dead = state.dead ?? [];
  assert.equal(dead.length, 1);
  assert.equal(dead[0]!.id, "a");
  assert.equal(dead[0]!.attempts, 8);
  assert.equal(dead[0]!.reason, "attempts");
  assert.equal(dead[0]!.diedAt, 1_000);
  assert.match(dead[0]!.lastError ?? "", /401/);
  assert.equal(dead[0]!.text, "a", "the payload is kept, so a redrive needs nothing else");
});

test("a message older than a day dies on age even with attempts left", async () => {
  const stale = { ...entry("old", "c1", 0), attempts: 1 };
  const { harness, state } = fakeHarness([stale]);
  await drain(harness, context, async () => {
    throw new Error("timeout");
  }, 25 * 3_600_000);
  assert.equal(state.dead?.[0]?.reason, "age");
});

test("dead letters can be listed", async () => {
  const { harness, state } = fakeHarness([]);
  state.dead = [{ ...entry("a", "c1", 0), attempts: 8, diedAt: 5, reason: "attempts" }];
  const listed = await deadLetters(harness, context);
  assert.equal(listed.length, 1);
  assert.equal(listed[0]!.id, "a");
  listed[0]!.text = "mutated";
  assert.equal(state.dead[0]!.text, "a", "listing hands back copies");
});

test("redriving puts a dead letter back in line with a fresh clock", async () => {
  const { harness, state } = fakeHarness([]);
  state.dead = [{ ...entry("a", "c1", 0), attempts: 8, lastError: "401", diedAt: 5, reason: "attempts" }];

  assert.equal(await redrive(harness, context, "a"), 1);
  assert.equal(state.dead?.length, 0, "it is not in the dead letters any more");
  const back = state.entries[0]!;
  assert.equal(back.text, "a", "the payload survives the round trip");
  assert.equal(back.attempts, 0);
  assert.equal(back.nextAt, 0);
  assert.ok(back.createdAt > 0, "the age window restarts, so it gets a full day again");
  assert.equal((back as DeadEntry).diedAt, undefined);
});

test("redrive and discard leave other dead letters alone", async () => {
  const { harness, state } = fakeHarness([]);
  state.dead = ["a", "b", "c"].map((text) => ({
    ...entry(text, "c1", 0),
    attempts: 8,
    diedAt: 5,
    reason: "attempts" as const,
  }));

  assert.equal(await redrive(harness, context, "a"), 1);
  assert.equal(await discard(harness, context, "b"), 1);
  assert.deepEqual(state.dead?.map((item) => item.id), ["c"]);
  assert.equal(await discard(harness, context, "nope"), 0, "an unknown id is a no-op, not an error");
});

test("nothing waiting is a no-op", async () => {
  const { harness } = fakeHarness([]);
  const report = await drain(harness, context, async () => {
    throw new Error("should not be called");
  });
  assert.deepEqual(report, { sent: 0, left: 0, gaveUp: [] });
});

test("a paused message waits without blocking the chat and survives the drain", async () => {
  const held = { ...entry("held", "c1"), paused: true };
  const { harness, state } = fakeHarness([held, entry("b", "c1")]);
  const sent: string[] = [];
  await drain(harness, context, async (message) => {
    sent.push(message.text);
  }, 1_000);
  assert.deepEqual(sent, ["b"], "observe holds its own message, not the queue");
  assert.deepEqual(state.entries.map((item) => item.text), ["held"], "a paused message is not dropped");
});

test("flushing releases what observe held, and only that", async () => {
  const { harness, state } = fakeHarness([
    { ...entry("held", "c1"), paused: true },
    { ...entry("b", "c1"), attempts: 2, nextAt: 999_999 },
  ]);
  assert.equal(await releaseHeld(harness, context), 1);
  assert.equal(state.entries[0]!.paused, undefined);
  assert.equal(state.entries[0]!.nextAt, 0);
  assert.equal(state.entries[1]!.nextAt, 999_999, "a retry keeps its own timing");
});
