import assert from "node:assert/strict";
import { test } from "node:test";

type Live = Record<string, unknown>;

/**
 * A conversation whose only job is to hand the watcher frames we choose: the real one would
 * need a runtime and a storage. `emit` is the commit that follows a token batch.
 */
function fakeConversation(initial: Live) {
  let listener: ((call: unknown) => Promise<void>) | undefined;
  return {
    conversation: {
      watch: async () => ({
        value: { docs: { "pi.live": initial } },
        start: (fn: (call: unknown) => Promise<void>) => {
          listener = fn;
        },
        stop: async () => {},
      }),
    },
    async emit(next: Live) {
      await listener?.({ kind: "value", value: { docs: { "pi.live": next } }, ops: [], retired: false });
    },
  };
}

const liveDoc = (text: string) => ({
  generation: { message: { content: [{ type: "text", text }] } },
  run: { turns: 1 },
});

// The card used to be redrawn on a fixed 1500 ms beat, whatever had changed and however much of
// it. The point of measuring is that a word arriving now may be drawn now, and a frame of forty
// kilobytes may wait — the reader loses nothing either way.
test("a frame's quiet time is paid by its size, floored and capped", async () => {
  const { liveQuietMs } = await import("../src/live.ts");
  assert.equal(liveQuietMs(0), 300, "a tiny frame still respects the card's own 300 ms gate");
  assert.equal(liveQuietMs(16 * 1024), 500, "16 KiB at 32 KiB/s is half a second");
  assert.equal(liveQuietMs(10 * 1024 * 1024), 1500, "a huge frame is capped, not starved");
  assert.ok(liveQuietMs(64 * 1024) > liveQuietMs(8 * 1024), "larger is never quicker");
});

test("a snapshot that did not change is not pushed again", async () => {
  const { watchLive } = await import("../src/live.ts");
  const fake = fakeConversation(liveDoc("done"));
  const seen: string[] = [];
  const watcher = await watchLive(fake.conversation as never, (live) => seen.push(live.text), {
    minIntervalMs: 10,
  });
  assert.deepEqual(seen, ["done"]);
  await fake.emit(liveDoc("done"));
  assert.deepEqual(seen, ["done"], "the same words must not redraw the card");
  await watcher.stop();
});

// A delta that lands a moment after the last push is exactly the case a plain throttle drops:
// it is too early to send and nobody comes back for it. The timer is what comes back.
test("a delta inside the floor is carried by the timer, not dropped", async () => {
  const { watchLive } = await import("../src/live.ts");
  const fake = fakeConversation(liveDoc(""));
  const seen: string[] = [];
  const watcher = await watchLive(fake.conversation as never, (live) => seen.push(live.text), {
    minIntervalMs: 60,
    maxIntervalMs: 400,
  });
  assert.deepEqual(seen, [""]);
  await fake.emit(liveDoc("a"));
  assert.deepEqual(seen, [""], "too soon after the first push");
  await new Promise((resolve) => setTimeout(resolve, 120));
  assert.deepEqual(seen, ["", "a"], "the early delta arrives on the timer");
  await watcher.stop();
});

test("stopping drops a scheduled push", async () => {
  const { watchLive } = await import("../src/live.ts");
  const fake = fakeConversation(liveDoc(""));
  const seen: string[] = [];
  const watcher = await watchLive(fake.conversation as never, (live) => seen.push(live.text), {
    minIntervalMs: 60,
    maxIntervalMs: 400,
  });
  await fake.emit(liveDoc("late"));
  await watcher.stop();
  await new Promise((resolve) => setTimeout(resolve, 120));
  assert.deepEqual(seen, [""], "a stopped watcher must not touch the card");
});
