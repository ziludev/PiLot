import { test } from "node:test";
import assert from "node:assert/strict";

const { digestOf, enqueueTurn } = await import("../src/feishu.ts");

/** Two cards in one screenshot must not look identical, and a step that hangs must show its age. */
test("a card carries the user's line, clipped to one row", () => {
  assert.equal(digestOf("  两个一样的，这个不太对吧 "), "两个一样的，这个不太对吧");
  assert.equal(digestOf("第一行\n\n第二行"), "第一行 第二行");
  assert.equal(digestOf("   "), "");
  assert.equal(digestOf("啊".repeat(40)), `${"啊".repeat(28)}…`);
  // A hard cut landed mid-word ("…conversationstates 接…"); the title now ends on a break.
  assert.equal(digestOf("看看 list conversationstates 接口定义在哪"), "看看 list conversationstates…");
  assert.equal(digestOf("一".repeat(20) + "，" + "二".repeat(10)), `${"一".repeat(20)}，…`);
});

test("turns in one conversation run one at a time", async () => {
  const first = enqueueTurn(424242);
  assert.equal(first.queued, false);

  const second = enqueueTurn(424242);
  assert.equal(second.queued, true);

  let secondStarted = false;
  void second.wait.then(() => {
    secondStarted = true;
  });
  await new Promise((resolve) => setTimeout(resolve, 20));
  assert.equal(secondStarted, false, "the second turn must wait for the first");

  first.done();
  await new Promise((resolve) => setTimeout(resolve, 20));
  assert.equal(secondStarted, true);

  const third = enqueueTurn(424242);
  assert.equal(third.queued, true, "still behind the unfinished second turn");
  second.done();
  third.done();
  await new Promise((resolve) => setTimeout(resolve, 20));

  const fourth = enqueueTurn(424242);
  assert.equal(fourth.queued, false, "the queue forgets a conversation once it drains");
  fourth.done();
});

test("different conversations do not queue behind each other", () => {
  const a = enqueueTurn(5150);
  const b = enqueueTurn(5151);
  assert.equal(a.queued, false);
  assert.equal(b.queued, false);
  a.done();
  b.done();
});
