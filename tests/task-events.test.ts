import assert from "node:assert/strict";
import test from "node:test";
import { CARD_TEXT_MAX, CardTextDoc, pruneCards, rememberCardText, sentCardText, type SentCard } from "../src/card-text.ts";
import { stepsFromTranscript } from "../src/activity.ts";
import { CARD_PLACEHOLDER, quoteText } from "../src/feishu.ts";

/** Just enough harness for the two card-text helpers: one doc, one commit at a time. */
function fakeHarness(): { harness: never; state: { cards: Record<string, SentCard>; commits: number } } {
  const state = { cards: {} as Record<string, SentCard>, commits: 0 };
  const harness = {
    commit: async (body: (tx: { doc: () => Promise<{ cards: Record<string, SentCard> }> }) => Promise<void>) => {
      state.commits += 1;
      await body({ doc: async () => ({ cards: state.cards }) });
    },
    snapshot: async () => ({ cards: state.cards }),
  };
  return { harness: harness as never, state };
}

test("a card's text is written down and read back", async () => {
  const { harness, state } = fakeHarness();
  await rememberCardText(harness, undefined as never, "om_1", "  答案在这里\n");
  assert.equal(state.cards.om_1?.text, "答案在这里");
  assert.equal(state.commits, 1);
  assert.equal(await sentCardText(harness, undefined as never, "om_1"), "答案在这里");
  // Someone else's message is not in the book, and that is how the caller tells ours from theirs.
  assert.equal(await sentCardText(harness, undefined as never, "om_other"), undefined);
});

test("nothing to say, nothing written", async () => {
  const { harness, state } = fakeHarness();
  await rememberCardText(harness, undefined as never, "om_2", "   ");
  await rememberCardText(harness, undefined as never, undefined, "text");
  assert.deepEqual(state.cards, {});
  assert.equal(state.commits, 0);
});

test("the oldest cards go first", () => {
  const cards: Record<string, SentCard> = {};
  for (let i = 0; i < CARD_TEXT_MAX + 5; i += 1) cards[`om_${i}`] = { text: `t${i}`, at: i };
  pruneCards(cards);
  assert.equal(Object.keys(cards).length, CARD_TEXT_MAX);
  assert.equal(cards.om_0, undefined);
  assert.equal(cards[`om_${CARD_TEXT_MAX + 4}`]?.text, `t${CARD_TEXT_MAX + 4}`);
  assert.equal(CardTextDoc.definition.kind, "app.card-text");
});

test("the 2.0 placeholder is not mistaken for what the card said", () => {
  const stub = JSON.stringify({ title: "✅ 目标达成", elements: [[{ tag: "text", text: CARD_PLACEHOLDER }]] });
  assert.equal(quoteText("interactive", stub, []), "✅ 目标达成");
  // A card read back with nothing but the stub in it still must not quote the stub.
  const nothing = JSON.stringify({ elements: [[{ tag: "text", text: `${CARD_PLACEHOLDER}，以查看内容` }]] });
  assert.equal(quoteText("interactive", nothing, []), "（一张卡片，里面没有可读的文字）");
  const own = JSON.stringify({ elements: [[{ tag: "markdown", content: "**结论**：改好了" }]] });
  assert.equal(quoteText("interactive", own, []), "**结论**：改好了");
});

test("an event we wrote down reads like a step", () => {
  const steps = stepsFromTranscript([
    { id: 7, kind: "app.approval", at: 1_000, content: [{ type: "text", text: "bash：rm -rf /tmp/x → 已批准" }], callId: null, isError: false },
    { id: 9, kind: "app.done", at: 2_000, content: [{ type: "text", text: "目标未达成：测试还是红的" }], callId: null, isError: false },
  ]);
  assert.deepEqual(steps, [
    { id: 7, kind: "say", at: 1_000, state: "ok", text: "bash：rm -rf /tmp/x → 已批准" },
    { id: 9, kind: "say", at: 2_000, state: "ok", text: "目标未达成：测试还是红的" },
  ]);
});
