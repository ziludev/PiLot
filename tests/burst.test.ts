import assert from "node:assert/strict";
import { test } from "node:test";
import { collectBurst, mergeUserContent } from "../src/feishu.ts";

const item = (text: string, id = text) => ({
  message: { message_id: id, chat_id: "oc_test", chat_type: "p2p" } as never,
  parsed: { text, imageKeys: [] },
  senderId: "ou_owner",
});

test("messages typed within the window run as one batch", async () => {
  const first = collectBurst(901, item("看看 X"), 25);
  assert.equal(await collectBurst(901, item("顺便查 Y"), 25), undefined, "the second message joins, it does not start a turn");
  const batch = await first;
  assert.deepEqual(batch?.map((entry) => entry.parsed.text), ["看看 X", "顺便查 Y"]);
});

test("a message after the window starts its own batch", async () => {
  const first = await collectBurst(902, item("第一条"), 10);
  assert.equal(first?.length, 1);
  const next = await collectBurst(902, item("隔了一会儿"), 10);
  assert.deepEqual(next?.map((entry) => entry.parsed.text), ["隔了一会儿"]);
});

test("two chats never share a batch", async () => {
  const a = collectBurst(903, item("A"), 20);
  const b = collectBurst(904, item("B"), 20);
  assert.deepEqual((await b)?.map((entry) => entry.parsed.text), ["B"]);
  assert.deepEqual((await a)?.map((entry) => entry.parsed.text), ["A"]);
});

test("a merged batch keeps every message recognisable", () => {
  assert.equal(mergeUserContent(["只有一条"]), "只有一条", "one message is passed through untouched");
  const merged = mergeUserContent(["看看 X", [{ type: "text" as const, text: "图片说明" }]]);
  assert.deepEqual(merged, [
    { type: "text", text: "【第 1 条】" },
    { type: "text", text: "看看 X" },
    { type: "text", text: "【第 2 条】" },
    { type: "text", text: "图片说明" },
  ]);
});
