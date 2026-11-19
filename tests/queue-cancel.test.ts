import { test } from "node:test";
import assert from "node:assert/strict";

const { cancelQueuedTurns, enqueueTurn } = await import("../src/feishu.ts");

const settle = () => new Promise((resolve) => setTimeout(resolve, 20));

/** A stop pressed while a message waits must drop that message, not the turn in front of it. */
test("cancelling the line leaves the running turn alone", async () => {
  const running = enqueueTurn(710001);
  const waiting = enqueueTurn(710001);
  assert.equal(running.queued, false);
  assert.equal(waiting.queued, true);

  assert.equal(cancelQueuedTurns(710001), 1);
  assert.equal(running.cancelled(), false, "the turn already working keeps its work");
  assert.equal(waiting.cancelled(), true);

  let released = false;
  void waiting.wait.then(() => {
    released = true;
  });
  running.done();
  await settle();
  assert.equal(released, true, "a cancelled turn still releases the line behind it");
  waiting.done();
});

test("a drained queue has nothing to take out", async () => {
  assert.equal(cancelQueuedTurns(710002), 0);
  const only = enqueueTurn(710002);
  assert.equal(cancelQueuedTurns(710002), 0, "the running turn is never a queue entry");
  assert.equal(only.cancelled(), false);
  only.done();
  await settle();
  assert.equal(cancelQueuedTurns(710002), 0);
});

test("cancelling twice counts once per message", () => {
  const running = enqueueTurn(710003);
  const second = enqueueTurn(710003);
  const third = enqueueTurn(710003);
  assert.equal(cancelQueuedTurns(710003), 2);
  assert.equal(cancelQueuedTurns(710003), 0, "already cancelled is not counted again");
  assert.equal(second.cancelled(), true);
  assert.equal(third.cancelled(), true);
  running.done();
  second.done();
  third.done();
});

/** The next message after a cancelled one must still start, or a stop wedges the chat forever. */
test("a cancelled turn does not wedge the turns behind it", async () => {
  const running = enqueueTurn(710004);
  const dropped = enqueueTurn(710004);
  const later = enqueueTurn(710004);
  cancelQueuedTurns(710004);

  running.done();
  await settle();
  dropped.done();
  await settle();

  let laterStarted = false;
  void later.wait.then(() => {
    laterStarted = true;
  });
  await settle();
  assert.equal(laterStarted, true, "the queued-but-not-cancelled message is unreachable here");
  later.done();
});

test("a conversation that drained starts fresh after a cancel", async () => {
  const running = enqueueTurn(710005);
  const waiting = enqueueTurn(710005);
  cancelQueuedTurns(710005);
  running.done();
  await settle();
  waiting.done();
  await settle();

  const fresh = enqueueTurn(710005);
  assert.equal(fresh.queued, false);
  assert.equal(fresh.cancelled(), false);
  fresh.done();
});
