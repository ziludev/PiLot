/**
 * Which Feishu scopes does this app still need?
 *
 * The only way to find out is to ask for something that needs the scope: a granted scope answers,
 * a missing one answers 400 with `need scope: …`. That message is the point of this script — the
 * console is the only place a scope can be added, and there it is a long list of checkboxes.
 *
 *   npm run check:scopes
 *
 * Exit code is the number of missing scopes, so it can gate anything that needs them.
 */
import * as Lark from "@larksuiteoapi/node-sdk";
import { config } from "../src/config.ts";

const owner = config.feishu.allowedSenderIds[0] ?? "";
const chatId = config.feishu.allowedChatIds[0] ?? "";

const client = new Lark.Client({ appId: config.feishu.appId, appSecret: config.feishu.appSecret });

type Probe = {
  scope: string;
  /** What breaks without it, in the owner's words. */
  why: string;
  run: () => Promise<unknown>;
};

const probes: Probe[] = [
  {
    scope: "im:message.group_msg",
    why: "读话题里的历史发言（现在每条话题消息都从零开始，看不到前文）",
    run: () =>
      client.im.message.list({
        params: {
          container_id_type: "thread",
          container_id: "omt_199f4242298e9ba8",
          sort_type: "ByCreateTimeAsc",
          page_size: 1,
        },
      }),
  },
  {
    scope: "contact:user.base:readonly",
    why: "把 sender open_id 换成人名（卡片上显示「张三」而不是 ou_xxx）",
    run: () => client.contact.user.get({ path: { user_id: owner }, params: { user_id_type: "open_id" } }),
  },
  {
    scope: "im:chat:readonly",
    why: "读群名和群成员（现在的会话名是 chat_id 或首条消息）",
    run: () => client.im.chat.get({ path: { chat_id: chatId }, params: { user_id_type: "open_id" } }),
  },
];

let missing = 0;
for (const probe of probes) {
  let verdict = "ok";
  try {
    await probe.run();
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    // The SDK throws on 400 instead of returning the body, so the scope name comes from the text.
    const need = /need scope: ([a-z:._]+)/.exec(message)?.[1];
    if (need !== undefined) {
      missing += 1;
      verdict = `MISSING (${need})`;
    } else {
      verdict = `unexpected: ${message.slice(0, 120)}`;
    }
  }
  console.log(`${verdict.padEnd(34)} ${probe.scope.padEnd(28)} ${probe.why}`);
}
if (missing > 0) {
  console.log(
    `\n${missing} scope(s) to add: https://open.feishu.cn/app/${config.feishu.appId}/auth ` +
      `— 开通后要「创建版本并发布」，权限才对线上生效。`,
  );
}
process.exit(missing);
