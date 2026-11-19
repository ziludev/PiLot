import assert from "node:assert/strict";
import { test } from "node:test";
import { lookupThread, recordThread, threadIndex, topicTitle } from "../src/threads.ts";

/**
 * The rail names a topic by the message that opened it. Before this, the state
 * endpoint guessed from the topic's latest ask — and every topic older than 40
 * entries of tool loop guessed wrong, so the rail read `话题 8f1a4d` for all of
 * them. These pin the name to the fork and to nothing later.
 */

type Doc = { threads: Record<string, { conversationId: number; parentConversationId?: number; surfaceId?: string; title?: string; at: number }> };

function fakeHarness(doc: Doc) {
  return {
    snapshot: async () => doc,
    commit: async (body: (tx: { doc: () => Doc }) => Promise<void>) => {
      await body({ doc: () => doc });
    },
  };
}

const at = (text: string): string => topicTitle(text);

test("a topic's name is one line, no mention, cut where it stops being a name", () => {
  assert.equal(at("@ 帮我查下用户2100081353的日志"), "帮我查下用户2100081353的日志");
  assert.equal(at("  两行\n  文字  "), "两行 文字");
  assert.equal(at("[图片]").length, 4, "a message with no text still gives the topic a name");
  const long = "把客户端重写成单窗口，rail 管会话和任务，transcript 只读";
  assert.equal(at(long).length, 32);
  assert.ok(at(long).startsWith("把客户端重写成单窗口"), "the cut keeps the front of the ask");
});

test("the opening message names the topic, and a later turn does not rename it", async () => {
  const doc: Doc = { threads: {} };
  const harness = fakeHarness(doc);
  await recordThread(harness as never, undefined as never, "omt_1", 42, 7, "oc_x", "先查日志");
  assert.equal(doc.threads["omt_1"]?.title, "先查日志");
  await recordThread(harness as never, undefined as never, "omt_1", 42, 7, "oc_x");
  assert.equal(doc.threads["omt_1"]?.title, "先查日志", "a message without a title keeps the old one");
  assert.equal((await lookupThread(harness as never, undefined as never, "omt_1"))?.title, "先查日志");
});

test("a fork made before topics had names is backfilled without losing its parent", async () => {
  const doc: Doc = { threads: {} };
  const harness = fakeHarness(doc);
  await recordThread(harness as never, undefined as never, "omt_2", 43, 7, "oc_x");
  assert.equal(doc.threads["omt_2"]?.title, undefined);
  await recordThread(harness as never, undefined as never, "omt_2", 43, undefined, "oc_x", "补一个名字");
  assert.equal(doc.threads["omt_2"]?.title, "补一个名字");
  assert.equal(doc.threads["omt_2"]?.parentConversationId, 7, "the parent survives a merged write");
});

test("the index the state endpoint reads carries that name", async () => {
  const doc: Doc = {
    threads: {
      omt_3: { conversationId: 44, parentConversationId: 7, surfaceId: "oc_x", title: "订阅日志", at: 1 },
      omt_4: { conversationId: 45, parentConversationId: 7, surfaceId: "oc_x", at: 1 },
    },
  };
  const index = await threadIndex(fakeHarness(doc) as never, undefined as never);
  assert.equal(index["44"]?.title, "订阅日志");
  assert.equal(index["45"]?.title, undefined, "an unnamed fork falls back, it does not invent");
  assert.equal(index["45"]?.parent, 7);
});
