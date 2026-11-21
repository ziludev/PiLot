import assert from "node:assert/strict";
import { test } from "node:test";
import type { Harness } from "@earendil-works/pi-durable";
import { drain, retryDelay, type OutboxEntry } from "../src/outbox.ts";

function entry(text: string, chatId: string, at = 0): OutboxEntry {
  return { id: text, chatId, text, plain: false, createdAt: at, attempts: 0, nextAt: 0 };
}

/** The only part of the harness the outbox touches: one doc, read and written in a commit. */
function fakeHarness(entries: OutboxEntry[]) {
  const state = { entries };
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

test("nothing waiting is a no-op", async () => {
  const { harness } = fakeHarness([]);
  const report = await drain(harness, context, async () => {
    throw new Error("should not be called");
  });
  assert.deepEqual(report, { sent: 0, left: 0, gaveUp: [] });
});
