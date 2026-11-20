import assert from "node:assert/strict";
import { test } from "node:test";

import { levelOf, type ChatLevel } from "../src/access.ts";
import { mentionsUs, rejectionReason, threadDigest, type Gate } from "../src/feishu.ts";

/**
 * One level per chat decides who gets an answer: `closed` answers nobody, `owner` answers only
 * the owner, `group` answers everyone with the owner's tools. Before, the same question was
 * answered by two allowlists and a derived rule, and the first version let an authorized group
 * card every member separately.
 */
const policy: Gate = { mentionsOnly: true };

function event(chat: string, sender: string, options: { chatType?: string; mentions?: unknown[] } = {}) {
  return {
    sender: { sender_id: { open_id: sender } },
    message: {
      message_id: "om_1",
      chat_id: chat,
      chat_type: options.chatType ?? "p2p",
      message_type: "text",
      content: JSON.stringify({ text: "在吗" }),
      mentions: options.mentions,
    },
  } as never;
}

const parsed = { text: "在吗", imageKeys: [], audioKey: undefined } as never;
const group = (chat: string, sender: string) =>
  event(chat, sender, { chatType: "group", mentions: [{ key: "@_user_1" }] });

test("a chat nobody opened answers nobody, even the owner", () => {
  assert.equal(rejectionReason(event("oc_new", "ou_owner"), parsed, "closed", true, policy), "chat-not-allowed");
  assert.equal(rejectionReason(event("oc_new", "ou_stranger"), parsed, "closed", false, policy), "chat-not-allowed");
});

test("a chat pinned to the owner answers only the owner", () => {
  assert.equal(rejectionReason(event("oc_owner", "ou_owner"), parsed, "owner", true, policy), undefined);
  assert.equal(rejectionReason(group("oc_owner", "ou_colleague"), parsed, "owner", false, policy), "owner-only");
});

test("an opened group answers every member, no per-person authorization", () => {
  assert.equal(rejectionReason(group("oc_group", "ou_colleague"), parsed, "group", false, policy), undefined);
  assert.equal(rejectionReason(group("oc_group", "ou_owner"), parsed, "group", true, policy), undefined);
  assert.equal(rejectionReason(event("oc_group", "ou_colleague"), parsed, "group", false, policy), undefined);
});

test("a shared group still requires the @", () => {
  const without = event("oc_group", "ou_colleague", { chatType: "group" });
  assert.equal(rejectionReason(without, parsed, "group", false, policy), "group-message-without-mention");
});

/** A group @'s its own members all day. That is not somebody talking to the bot. */
test("a mention of a colleague does not address us", () => {
  const ofColleague = event("oc_group", "ou_colleague", {
    chatType: "group",
    mentions: [{ key: "@_user_1", name: "郁斐凡", id: { open_id: "ou_yufan" } }],
  });
  assert.equal(rejectionReason(ofColleague, parsed, "group", false, policy, "ou_bot"), "group-message-without-mention");
  assert.equal(mentionsUs([{ id: { open_id: "ou_yufan" } }], "ou_bot"), false);

  const ofUs = event("oc_group", "ou_colleague", {
    chatType: "group",
    mentions: [{ key: "@_user_1", name: "知衡", id: { open_id: "ou_bot" } }],
  });
  assert.equal(rejectionReason(ofUs, parsed, "group", false, policy, "ou_bot"), undefined);
  assert.equal(mentionsUs([{ id: { open_id: "ou_bot" } }], "ou_bot"), true);
});

/** Without the bot's own id the old rule (any mention) stays: silence is the worse failure. */
test("an unknown bot identity falls back to any mention", () => {
  assert.equal(mentionsUs([{ id: { open_id: "ou_yufan" } }], undefined), true);
  assert.equal(mentionsUs([], "ou_bot"), false);
});

test("empty content and unsupported types are named separately", () => {
  const empty = { text: "", imageKeys: [] } as never;
  assert.equal(rejectionReason(event("oc_owner", "ou_owner"), empty, "owner", true, policy), "empty-content");
  const voice = event("oc_owner", "ou_owner");
  (voice as { message: { message_type: string } }).message.message_type = "sticker";
  assert.match(rejectionReason(voice, parsed, "owner", true, policy) ?? "", /unsupported-message-type/);
});

test("levelOf: a stored level wins, `.env` is the fallback, everything else is closed", () => {
  const access: Record<string, ChatLevel> = { oc_group: "group", oc_pinned: "owner" };
  assert.equal(levelOf("oc_group", access, ["oc_env"]), "group");
  assert.equal(levelOf("oc_pinned", access, ["oc_env"]), "owner");
  assert.equal(levelOf("oc_env", access, ["oc_env"]), "owner");
  assert.equal(levelOf("oc_unknown", access, ["oc_env"]), "closed");
  // `.env` must not win over an explicit level: a group opened with /allow stays a group.
  assert.equal(levelOf("oc_env", { ...access, oc_env: "group" }, ["oc_env"]), "group");
});

// A group has one instruction text: alex asking in his own group and a colleague asking in the
// same group are told the same thing. What differs is the memory boundary (policy doc) and the
// write rule, and the write rule is enforced in the guard, not promised in the prompt.
test("a shared group is told it can read everything and write nothing, and that the old rule is dead", async () => {
  const { groupInstructions } = await import("../src/feishu.ts");
  const { config } = await import("../src/config.ts");
  const text = groupInstructions();

  assert.match(text, /改文件/);
  assert.match(text, new RegExp(`不要透露 ${config.feishu.ownerName}`), "the memory rule stays");
  assert.match(text, /旧规则已作废/, "the earlier read-only answer has to be superseded explicitly");
  assert.match(text, /群里是只读的/, "one text for everyone, and it says reads are open");
  assert.doesNotMatch(text, /弹审批卡/, "members are refused outright, not queued for a card");
  assert.match(groupInstructions("只回答本群主题"), /只回答本群主题/, "/open keeps its topic rule");
  assert.match(text, /方括号是网关标注的说话人/, "the bracket convention is explained");
});

// 写权限按人分，口径也得按人分：alex 自己在群里发指令时，再告诉他「这一步得 alex 来做」
// 就是在让主人去找主人。群还是那个群，隐私规则照旧，只是写操作的措辞换一套。
test("the owner speaking in a group is not told to go find himself", async () => {
  const { groupInstructions } = await import("../src/feishu.ts");
  const { config } = await import("../src/config.ts");
  const owner = groupInstructions(undefined, "owner");
  const member = groupInstructions(undefined, "member");

  assert.equal(groupInstructions(), member, "a shared group still defaults to the member wording");
  assert.match(member, /群里是只读的/);
  assert.doesNotMatch(owner, /群里是只读的/, "the owner keeps the write tools");
  assert.doesNotMatch(owner, /这一步得/, "and is never sent to ask himself");
  assert.match(owner, /写操作照做/);
  assert.match(owner, /不要透露/, "the privacy rule does not depend on who is asking");
  assert.match(owner, /不要把密钥/, "a group is public even when the owner talks");
  assert.match(groupInstructions("只回答本群主题", "owner"), /只回答本群主题/);
  assert.match(owner, /群里所有人都看得见/, "and is told the room is still public");
  assert.match(member, /给同事看的/);
});

// The model does not trust an identity claim in the instructions; it reads the speaker on the
// message — which now only feeds the audit trail, since the tools no longer depend on it.
test("each group message carries who is speaking", async () => {
  const { speakerTag } = await import("../src/feishu.ts");
  const owner = "ou_0123456789abcdef0123456789abcdef";
  const { config } = await import("../src/config.ts");
  assert.match(speakerTag(true, owner), new RegExp(`^\\[${config.feishu.ownerName} 本人\\]$`));
  // A name, when Feishu gives one, replaces the id fragment; without one the id still identifies.
  assert.equal(speakerTag(false, "ou_9abc1a2b3c4d", "张三"), "[群成员 张三]");
  assert.match(speakerTag(false, "ou_9abc1a2b3c4d"), /^\[群成员 ou_9abc1\]/);
  assert.match(speakerTag(false, "ou_9abc1a2b3c4d", ""), /^\[群成员 ou_9abc1\]/);
  assert.doesNotMatch(speakerTag(false, "ou_9abc1a2b3c4d", "张三"), /本人|ou_9abc1/);
});

// Threading rule, which has flip-flopped: an @ in a group is one person's question, so
// the answer goes into a topic instead of pushing the timeline aside for everyone.
test("an @ in a group answers in a topic, a plain message answers inline", async () => {
  const { wantsThread } = await import("../src/feishu.ts");
  const messageOf = (value: unknown) => (value as { message: never }).message;

  const mentioned = event("oc_owner", "ou_owner", { chatType: "group", mentions: [{ key: "@_user_1" }] });
  assert.equal(wantsThread(messageOf(mentioned)), true, "mention should thread");

  const plain = event("oc_owner", "ou_owner", { chatType: "group" });
  assert.equal(wantsThread(messageOf(plain)), false, "no mention, no thread");

  const inThread = event("oc_owner", "ou_owner", { chatType: "group" });
  (inThread as { message: { thread_id: string } }).message.thread_id = "omt_1";
  assert.equal(wantsThread(messageOf(inThread)), true, "already in a topic");

  const direct = event("oc_owner", "ou_owner", { chatType: "p2p", mentions: [{ key: "@_user_1" }] });
  assert.equal(wantsThread(messageOf(direct)), false, "p2p never threads");
});

/** A topic writes no events for messages we are not mentioned in, and has no per-message quote. */
test("a topic's own messages reach the model as one block", () => {
  const items = [
    { message_id: "om_a", msg_type: "text", body: { content: JSON.stringify({ text: "这个接口是哪个服务的" }) }, sender: { sender_type: "user", sender_name: "王福发" } },
    { message_id: "om_b", msg_type: "text", body: { content: JSON.stringify({ text: "语音代回任务那块" }) }, sender: { sender_type: "user", sender_name: "熊菲" } },
    // our own answer is already in the conversation, in full
    { message_id: "om_c", msg_type: "text", body: { content: JSON.stringify({ text: "在 dora-service" }) }, sender: { sender_type: "app", sender_name: "知衡" } },
    // the message being answered right now
    { message_id: "om_d", msg_type: "text", body: { content: JSON.stringify({ text: "@知衡 你再看下" }) }, sender: { sender_type: "user", sender_name: "王福发" } },
  ];
  assert.equal(
    threadDigest(items, { current: "om_d" }),
    "[话题里最近的 2 条发言]\n王福发: 这个接口是哪个服务的\n熊菲: 语音代回任务那块\n[话题结束]\n\n",
  );
});

/** Feishu puts the mention as a token; left alone the digest reads "@_user_1 你再看下". */
test("a mention in the topic keeps the name, not the token", () => {
  const items = [
    {
      message_id: "om_a",
      msg_type: "text",
      body: { content: JSON.stringify({ text: "@_user_1 这个文档也看下" }) },
      sender: { sender_type: "user", sender_name: "王福发" },
      mentions: [{ key: "@_user_1", name: "知衡（PiLot）", id: "ou_bot" }],
    },
  ];
  assert.equal(
    threadDigest(items, { current: "om_b" }),
    "[话题里最近的 1 条发言]\n王福发: @知衡（PiLot） 这个文档也看下\n[话题结束]\n\n",
  );
});

test("a topic with nothing but our own answers adds nothing", () => {
  const items = [{ message_id: "om_a", msg_type: "text", body: { content: JSON.stringify({ text: "好了" }) }, sender: { sender_type: "app", sender_name: "知衡" } }];
  assert.equal(threadDigest(items, { current: "om_b" }), "");
});
