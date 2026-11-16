# The Feishu app

Everything below happens in the developer console of your own tenant: open.feishu.cn → your app →
the menu named in the left column. `./bin/durable setup` reads the same console back afterwards
(`durable doctor` repeats it any time), so every ✗ it prints points at one row here.

| Menu | Set | Why |
|---|---|---|
| 添加应用能力 | 机器人 | without it the app cannot send a message |
| 权限管理 | `im:message`, `im:message:send_as_bot`, `im:message.p2p_msg:readonly`, `im:message.group_at_msg:readonly` | receive and reply, direct and in groups |
| 权限管理 | `im:message.group_msg` | only if you want replies in a topic without an @mention |
| 权限管理 | `contact:user.base:readonly` | without it cards and quotes show `ou_…` instead of a name |
| 事件与回调 → 订阅方式 | **使用长连接接收事件** | no public IP, no domain, no certificate |
| 事件与回调 → 回调 | `card.action.trigger` | the approval and Continue buttons; without it a tap does nothing |
| 事件与回调 → 事件 | `im.message.receive_v1` | the message itself |
| 版本管理与发布 | 创建版本 → 申请发布 | an unpublished change is not a change; the long connection keeps failing until it is published |

## Order that works

1. 创建企业自建应用, then copy **App ID** and **App Secret** from 凭证与基础信息.
2. Add the capability and the scopes, then publish a version. Scopes granted after a publish need
   another publish — this is the step people skip, and the symptom is a 99991672 "Access denied"
   on the API that needs the new scope.
3. Run `./bin/durable setup` and fill 3–6: the app credentials, your own `open_id`, then let it
   read the console back.
4. Add the bot to a chat (群里 → 设置 → 群机器人 → 添加机器人). A p2p chat works as soon as you
   message the bot; a group needs the bot added first, and `FEISHU_MENTIONS_ONLY` decides whether a
   bare message counts.
5. Message it. The first message from you is what teaches it which chat is your DM — approvals and
   the morning digest go there.

## Getting your open_id

`setup` accepts the id, but you may not know it yet. Two ways:

- Send the bot a message and read `logs/agent.log` for `access … owner chat` / the sender id in the
  audit line.
- Ask an admin for it in 开发者后台 → 用户与组织, or call `contact/v3/users` with the app's own
  credentials once `contact:user.base:readonly` is granted.

## When the long connection keeps dropping

`/api/state` reports `feishu.ready`, `flaps` and `downMs`; `logs/agent.log` shows
`connect ENETUNREACH …` or `receive events or callbacks through persistent connection only available
in self-build & Feishu app`. Those are two different failures:

- **ENETUNREACH / timeouts to `msg-frontier.feishu.cn`**: a path problem on this machine (VPN, proxy,
  DNS). The supervisor rebuilds the client, but it cannot fix a route.
- **"only available in self-build & Feishu app"**: 订阅方式 is not 长连接, or the version was not
  published. Fix it in the console, not in the code.
