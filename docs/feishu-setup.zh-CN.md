# 飞书应用配置

下面这些都在你自己租户的开发者后台：open.feishu.cn → 你的应用 → 左栏对应的菜单。
`./bin/durable setup` 装完会把同一份控制台读回来（`durable doctor` 随时可再跑一遍），
所以它打的每个 ✗ 都对着这里的一行。

| 菜单 | 设置 | 为什么 |
|---|---|---|
| 添加应用能力 | 机器人 | 没有它，应用发不了消息 |
| 权限管理 | `im:message`、`im:message:send_as_bot`、`im:message.p2p_msg:readonly`、`im:message.group_at_msg:readonly` | 收消息、回消息，单聊和群都要 |
| 权限管理 | `im:message.group_msg` | 只有你想在话题里不 @ 也回复时才需要 |
| 权限管理 | `contact:user.base:readonly` | 没它，卡片和引用里显示 `ou_…` 而不是人名 |
| 事件与回调 → 订阅方式 | **使用长连接接收事件** | 不需要公网 IP、域名、证书 |
| 事件与回调 → 回调 | `card.action.trigger` | 审批与「继续」按钮；没配就是点了没反应 |
| 事件与回调 → 事件 | `im.message.receive_v1` | 消息本体 |
| 版本管理与发布 | 创建版本 → 申请发布 | 不发布等于没配，长连接会一直失败 |

## 按这个顺序做

1. 创建企业自建应用，从「凭证与基础信息」抄下 **App ID** 和 **App Secret**。
2. 加能力、加权限，然后**发版**。发版之后补的权限还要再发一次版 —— 这一步最容易漏，
   症状是需要新权限的那个接口报 `99991672 Access denied`。
3. 跑 `./bin/durable setup`，填 3–6 步：应用凭据、你自己的 `open_id`，然后让它把控制台读回来。
4. 把机器人拉进会话（群 → 设置 → 群机器人 → 添加机器人）。单聊直接发消息就行；
   群里必须先拉进群，是否必须 @ 由 `FEISHU_MENTIONS_ONLY` 决定。
5. 给它发一条消息。**你发的第一条消息**决定了哪个会话是你的私聊 —— 审批卡片和早报都发到那里。

## 怎么拿到自己的 open_id

`setup` 要这个 id，但你不一定知道。两条路：

- 先给机器人发一条消息，然后看 `logs/agent.log` 里的发送者 id，或 `durable audit` 里的审计行。
- 找管理员在「开发者后台 → 用户与组织」查；开通 `contact:user.base:readonly` 后用应用凭据调
  `contact/v3/users` 也行。

## 长连接一直断怎么办

`/api/state` 里有 `feishu.ready`、`flaps`、`downMs`；`logs/agent.log` 里会看到
`connect ENETUNREACH …` 或 `receive events or callbacks through persistent connection only available
in self-build & Feishu app`。这是两种不同的故障：

- **ENETUNREACH / 连 `msg-frontier.feishu.cn` 超时**：这台机器的网络路径问题（VPN、代理、DNS）。
  supervisor 会重建客户端，但修不了路由。
- **「only available in self-build & Feishu app」**：订阅方式不是长连接，或版本没发出去。
  去控制台改，不是在代码里改。
