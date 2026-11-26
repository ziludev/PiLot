# 飞书接入层换成 Go SDK —— 技术方案

状态：待评审 · 2026-11-19 · 结论在第 1 节

## 1. 结论

**不做整包迁移，加一个 Go 侧车（sidecar）进程 `feishu-gw`，只让它负责「飞书连接」这一层**：
收长连接事件、发/改/删消息、下载附件。桥接层（`src/`，TypeScript）继续管会话、调度、卡片内容，
通过 `127.0.0.1` 的 HTTP 调它，卡片 JSON 原样透传。

- 收益：长连接与发消息的可靠性从 Node SDK 里拿出来，换成 `oapi-sdk-go/v3`（有官方 `larkws`，
  重连/心跳逻辑成熟）；顺带把现在「ack 在 turn 之后」导致的事件重复投递问题一并修掉。
- 代价：多一个进程、多一层 HTTP、多一处日志。**如果不为可靠性、只为「Go 的类型更整齐」，别做**——
  卡片内容本来就是纯 JSON（`card()` 产对象，`JSON.stringify` 后发），SDK 只碰传输，类型收益接近零。

## 2. 现状盘点（实测，非估算）

`@larksuiteoapi/node-sdk` 出现在 4 个文件，业务面只有两处：

| 位置 | 用途 |
|---|---|
| `src/feishu.ts` | 全部收发 |
| `src/feishu-selftest.ts` | 手工发卡片自检（`npm run card`） |
| `scripts/check-feishu-scopes.ts` | 读应用权限是否开全 |
| `src/cli/feishu-check.ts` | `durable doctor` 的一部分 |

`src/feishu.ts` 共 2910 行，真正调 SDK 的只有 13 个调用点，归结为 5 个操作：

| 操作 | 调用点 | 备注 |
|---|---|---|
| `im.message.create` | 981 `sendText`、989 `sendMarkdown`、1197 `sendNotice`、1634 未授权通知 | `receive_id_type=chat_id` |
| `im.message.reply` | 1018 卡片、1061 纯文本提醒、1109 `replyCard`、1129 纯文本回复 | 支持 `reply_in_thread`、`<at id=…>` |
| `im.message.patch` | 1036 `patchCard`（流式卡每步都在改） | 唯一有状态的写操作 |
| `im.message.get` | 1682 `quoteIsOurs`（判断被引用的是不是自己的消息） | 只读 `.data.items[0].sender` |
| `im.messageResource.get` | 1145 下载用户发来的图片/文件 | 二进制流 |

事件只有两个：`im.message.receive_v1`（消息）、`card.action.trigger`（按钮），后者是**同步回调**——
处理函数返回值（`{toast}` / 新卡片）由 SDK 直接回给飞书，在飞书侧更新卡片，不走独立 API。

关键约束（写方案时不能丢）：飞书对 `card.action.trigger` 的响应有超时（约 3 秒）；现在的
`im.message.receive_v1` handler 是先 `void handle(data)` 再返回，注释里写得很清楚：SDK 要等
handler resolve 才 ack，所以早先每条消息被重复投递。

## 3. 目标与非目标

目标
1. 收事件、发消息、改卡片、下载附件四件事由 Go 完成。
2. 事件 ack 与业务处理解耦：连接层立即 ack，业务落库后才算完成（消除重复投递）。
3. 卡片内容的生产逻辑一行不改：桥接层还是 `card()`，Go 侧不解析卡片结构。
4. 可灰度、可回滚：环境变量切回 Node 传输层，行为与今天一致。

非目标
- 不重写桥接层、会话存储、agent 循环。
- 不在 Go 侧做去重、限流、鉴权（allowlist 仍在桥接层，那里有上下文）。
- 不引入公网回调地址（继续用长连接）。保持单机单用户假设。

## 4. 架构

```
飞书 ──WS──> feishu-gw (Go)  ──HTTP──>  bridge (Node/TS)  ──> pi-durable runtime
              │  oapi-sdk-go/v3          │  src/feishu.ts 换成 http 客户端
              │  larkws: 重连/心跳        │  卡片 JSON、allowlist、会话、去重
              └─ 发/改/删消息、下载附件  └─ 全部业务判断
```

职责边界一句话：**`feishu-gw` 不认识「轮次」「会话」「卡片 2.0」，它只认识飞书的 API 形状和
事件信封。** 桥接层不认识 App Secret、不建长连接。

进程：`feishu-gw` 作为第三个 launchd job（`com.pilot.agent.feishu`，`KeepAlive`），只绑
`127.0.0.1:8788`，与 bridge 的 `:8787` 并列。启动顺序不敏感——bridge 每次调它之前失败重试即可，
不搞启动依赖。

## 5. 接口契约

请求头统一 `X-Pilot-Token: $FEISHU_GW_TOKEN`（`.env` 里一对随机串，两侧共读）；只监听回环，
不设 TLS。出错一律 `{"error":{"code":"...","message":"..."}}` + 4xx/5xx，**并保留飞书的
`code`/`msg`**（现在 `patch failed: <code> <msg>` 的排查路径要留着）。

### 5.1 桥接层 → gw（飞书 API 的薄封装）

| 方法 | 路径 | 请求 | 响应 |
|---|---|---|---|
| POST | `/v1/text` | `{chatId, text, replyTo?, thread?}` | `{messageId}` |
| POST | `/v1/card` | `{chatId?, replyTo?, card, thread?, mention?}` | `{messageId?}` |
| PATCH | `/v1/card/{messageId}` | `{card}` | `{}` |
| POST | `/v1/message/{messageId}/sender` | `{}` | `{openId, isSelf}`（`isSelf` 由 gw 比对 bot open_id，省一次桥接层判断） |
| GET | `/v1/resource/{messageId}/{fileKey}?type=image\|file` | — | 二进制 + `Content-Type` |
| GET | `/healthz` | — | `{ready, since, flaps, lastError}` |

`card` 是任意 JSON：gw 收到后 `json.Marshal` 塞进 `content`，字段名一个都不看。所有
`msg_type=interactive` 的 create/reply/patch 走同一条编码路径。

### 5.2 gw → 桥接层（事件）

| 方法 | 路径 | 语义 |
|---|---|---|
| POST | `/feishu/event` | `{type:"im.message.receive_v1", eventId, payload, receivedAt}` |
| POST | `/feishu/card-action` | `{type:"card.action.trigger", eventId, payload}` → 响应体原样回给飞书 |

两条路径的超时与重试**故意不同**：

- **消息事件：先 ack，再投递。** gw 收到 WS 事件后立刻回成功给飞书，然后 POST 桥接层；
  桥接层只有在「消息已落到 durable 队列」后才回 200。gw 侧失败重试 3 次（1s/2s/4s，
  带 `eventId`），仍失败就写 `logs/feishu-gw.log` 一行 `dropped eventId=…` 并放弃——
  这条日志是唯一会丢消息的地方，也是唯一需要盯的地方。
- **按钮事件：同步转交，超时 2.5s。** 桥接层返回值原样作为飞书回调结果（`toast` 或新卡片）。
  2.5s 内没回来，gw 回 `{toast:{type:"warning",content:"处理超时，稍后看卡片"}}`，
  桥接层那次调用仍在后台继续（客户端已 ack，不存在重放）。

现在 `handled` 那个内存去重 Set 保留在桥接层，但语义从「防重复投递」降级为「防重放兜底」；
真正的幂等由 `eventId` 承担（`submissions` 表加一列 `event_id` 唯一索引即可）。

## 6. 卡片这条线为什么不碰

卡片 2.0 的 JSON 由 `card()` / `detailPanel()` / `approvalCard()` 生成，与传输层完全解耦。
换 Go 后这段代码、它的 248 个测试、`npm run card` 自检全部不动——`feishu-gw` 只是把同一个
JSON 对象发出去。这也是本方案能小步走的前提：**改动面只有「怎么发」，没有「发什么」。**

顺带记一条今天踩到的坑，写进 gw 的 README 免得后人再踩：
`im.message.get` 读回 2.0 消息时，飞书会转成旧版结构，每个元素列表都被替换成
「请升级至最新版本客户端，以查看内容」。**它不能用来验证卡片渲染**，只能用来读 sender。

## 7. 改造清单

Go 侧（新目录 `services/feishu-gw/`，独立 module `pilot/feishu-gw`）
- `main.go`：读 env（`FEISHU_APP_ID/SECRET`、`FEISHU_GW_TOKEN`、`BRIDGE_URL`、端口）
- `larkws` 客户端：`im.message.receive_v1`、`card.action.trigger` 两个 handler，连接状态上报 `/healthz`
- `sender.go`：5 个操作 → 5 个 handler，统一的错误封装与超时（写 10s / 读 15s）
- `dispatch.go`：事件投递、重试、丢弃日志
- 100% 覆盖 dispatch 的重试/超时分支（table-driven，无网络）

TS 侧
- `src/feishu.ts`：`client.*` 13 个调用点 → `gw.*`（一个新文件 `src/feishu-gw-client.ts`，约 120 行）
- `startFeishu`：P2 期间是 `FEISHU_TRANSPORT=node|go` 分支（`node` 留着做回滚），P4 已删掉 node 分支——
  只剩 HTTP 入口和网关客户端
- `feishuStatus` 改为周期拉 `/healthz`（2s 间隔），`/health` 的输出形状不变
- `scripts/agentctl`：`install-gw` / 卸载时一起 bootout；`status` 里多一行 gw 状态
- 测试：`feishu.ts` 的单元测试把 SDK mock 换成 HTTP mock，现存 248 个测试应当一断言都不改

## 8. 实施阶段与验收

每个阶段结束都能跑、能回滚。

| 阶段 | 内容 | 验收 |
|---|---|---|
| P1 连接层 | `feishu-gw` 只收事件、转发现有的 `[feishu] msg` 日志，不发消息 | `FEISHU_TRANSPORT=go` 时你发一条消息，桥接层日志出现同样的 chat/sender，且**只出现一次**（对照现在重复投递的记录） |
| P2 发送层 | 5 个操作全接上；卡片 create/reply/patch 走 gw | `npm run card` 发出 4 张自检卡；群里跑一轮完整对话（流式卡 → 最终卡 → 按钮点击），肉眼确认；`/healthz` 的 `flaps` 一周内不增长 |
| P3 观察 | 观察 `dropped` / `patch failed` 是否增长 | 连续 7 天不增长 |
| P4 收尾 ✅ | 删 node 传输路径（WSClient + 重启 supervisor）与 `FEISHU_TRANSPORT` 开关；SDK client 只留 `im.message.list` 和直发告警 | node 长连接相关符号为 0；`npm test` 全绿；真机一轮对话 |


### 8.1 P1 实测记录（2026-11-19）

结论：**两条长连接可以并存，事件是广播的**——这个方案的前提成立了。细节：

- Node 侧 11:52:47 连上，Go 侧 12:01:56 连上，同一个 app；12:02–12:04 三条
  `im.message.receive_v1` 在 Go 侧各收到一次（`messageId` 无重复），同时 Node 侧照常起 turn、照常回复。
- 长连接模式不需要 `verification_token` / `encrypt_key`，两者传空串即可（实测能解出明文 payload，`schema: 2.0`）。
- **“connected” 不等于 “在收”**：12:01:56 连上的实例 27 秒后收到首条事件，而 11:57:47 连上的前一实例
  在两分钟内一条都没收到（期间 Node 收到两条）。原因未查清，可能是首连失败重连后的路由状态。
  因此 P2 不能把“连上了”当健康判据：要么等首条事件再宣布 ready，要么和桥接层的事件数对账。
- 收事件到日志的延迟在毫秒级，`/healthz` 的 `lastEvent` 可直接当活性探针。

### 8.2 P2 实测记录（2026-11-19）

`FEISHU_TRANSPORT=go` 已在真机上跑通一整轮：gw 收 `im.message.receive_v1` → 转发 `POST
/feishu/events` → 桥接层 `eventDispatcher.invoke` → 出卡 → gw 发回群。`/healthz` 在那一轮
报 `events=1 delivered=1 dropped=0`，桥接层日志走的是同一条 `turn start` → `final card patched` 路径。

几个落地的细节，比方案里写的更具体：

- gw 的 HTTP API 用 `x-pilot-token`（`FEISHU_GW_TOKEN`）鉴权，转发到桥接层时带
  `Authorization: Bearer $AGENT_HTTP_TOKEN`——桥接层的 API 本来就拒绝陌生请求，这一层不另开口子。
- `im.message.get` 对 app 发的消息返回 `sender.id = cli_...`（app_id），`open_id` 为空；
  对人是 `id = ou_...`。所以“这条引用是不是我们自己写的”只能按 `sender_type === "app"` 加 app_id 判；
  桥接层原先比的是 `sender.id === botOpenId`，实际恒为 false（历史 bug），已随本次改动修为
  `writtenByApp(sender, appId, botOpenId)`（见 `src/feishu.ts`，有单测）。
- 桥接层保留一个 SDK client：`im.message.list`（线程历史/摘要）不在 gw 的五个操作里，仍走 SDK。
- 守护：`launchd/feishu-gw.plist.tmpl` + `agentctl install-gateway`，`KeepAlive` 常驻；
  P2 期间 `agentctl install` 只在 `FEISHU_TRANSPORT=go` 时才装它（两条连接并存会让每条消息被回答
  两次）；P4 删掉 node 分支后它变成必装项。
- **告警判据是 `connected`，不是 `ready`**（实测发现）：`ready = connected && 收到过事件`，所以每次
  重启网关之后它都会一直是 `false`，直到下一条消息到来。拿它当健康判据，安静的夜里会每半小时
  报一次假警、还顺手 `kickstart` 一次网关。所以 watchdog 只看 `connected`（连不上飞书才算坏），
  「连着但没流量」只写进 `/health` 的 `lastError`，不打扰人。
- watchdog：bridge 每 30s 探一次 `/healthz`，连续 10 次（5 分钟）不健康 → 一条告警 + `kickstart -k`
  重启网关（标签从 `~/Library/LaunchAgents/*feishu-gw.plist` 反查，不写死），恢复后再发一条
  「已恢复」。告警本身走 **SDK 直发**（`im.message.create` 到 owner 会话）：网关正是坏掉的那一环，
  不能再指望它送这条消息。`kickstart -k gui/501/com.pilot.agent.feishu-gw` 已实测能重启并让
  `/healthz` 重新 `connected:true`。判据抽成 `gatewayWatchStep()`（纯函数，4 个单测）：掉线报一次、
  半小时内不重复、恢复只报一次。

### 8.3 P4 实测记录（2026-11-19）

删掉 node 传输路径，桥接层只剩「网关客户端 + 事件 HTTP 入口」：

- `sdkTransport`（把 SDK client 包成同形状的 5 个操作）整个删除。`im` 现在只有
  `gatewayClient(gatewayEndpoint())` 一个来源。
- `Lark.WSClient` + `eventDispatcher` 的重启 supervisor（120s 无 ready 就重启、每三次重建 client
  以重解析 DNS）一起删除——这段逻辑挪到了网关进程和 bridge 的 watchdog 里，留着就是第二套没人
  维护的重连策略。`FEISHU_LOG_LEVEL`（只给 ws 打帧日志用）随之作废。
- `FEISHU_TRANSPORT` 开关消失；`.env` 里那行已删，`agentctl` 的 `gateway_wanted()` 也删了——
  网关从「可选」变成「没有它就是聋子哑巴」。`stop()` 现在只负责清掉 30s 健康探测的定时器。
- 保留的 SDK client 只剩两处**非传输**用途：`im.message.list`（线程历史/摘要）和告警直发
  （`im.message.create`，网关坏的时候唯一的出口）。所以「删干净 node-sdk」这条验收目标是做不到的，
  也不该做——要做的是让 SDK 不再承担**收发路径**。
- 文件从 3143 行降到 3047 行，`npx tsc --noEmit` 干净，`npm test` 267/267。验收方式是真机跑一轮：
  这条消息本身就是走网关进来的，回答的卡片也是网关发出去的。

### 8.4 网关之后补的两件小事（2026-11-19）

日志翻出来的两个漏洞，都不是架构问题，但都会让「网关了」变成「用户看不到答案」：

- **终帧 patch 现在会重试**。流式帧丢了下一帧会盖回来，终帧没有下一帧——它失败就是卡片永远停在
  「处理中」。原来 `deliverAnswer` 一次失败就退到「另发一张卡」，而那张卡也走网关，网关正是坏的
  那一半，于是三层 fallback 全落空（`12:14:56` 的三条 `stream patch failed fetch failed` 就是这个）。
  现在按 `patchRetryDelay(attempt, error)` 退避重试 500ms/1.5s/4s，只重试「值得重试」的失败：
  网络类（`fetch failed`）等它回来，飞书明确拒绝的（`patch failed: 230001`）不浪费这 6 秒。
  三层 fallback 之外再加一层 `sendTextDirect`——绕开网关、用 SDK client 直发，是网关挂了时唯一
  还通的出口（和 watchdog 的告警同一条路）。
- **`/api/health` 现在带 `feishu` 字段**（`{ready, since, flaps, lastError, started, downMs}`），
  `agentctl status` 头部打一行 `== feishu: ok / DOWN for N min — 原因`。此前这份状态只在
  `/api/state`（桌面客户端读的那个）里，命令行上判断「飞书还通不通」只能去 curl 网关的 `/healthz`。
  网关的 `/healthz` 说的是「网关自己」，桥接层这份说的是「从桥接层看过去的收发能力」，两者同时
  为真才是真的通。

## 9. 风险

| 风险 | 影响 | 对策 |
|---|---|---|
| 多一个进程，起不来时飞书静默失联 | 高（收不到消息） | 已实现：bridge 每 30s 探 `/healthz`，连续 10 次连不上 → SDK 直发告警 + `kickstart -k` 重启网关；恢复后再报一条。`agentctl status` 一并显示 |
| 按钮回调 2.5s 超时截断 | 中（点按钮看到超时提示，卡片没更新） | 现网 `set-model`/`open-chat` 都只碰 SQLite，实测 < 200ms；把慢操作（起 turn）继续放在「已 ack 后异步」这条线里，不在回调里做 |
| 事件 HTTP 投递丢 | 中（少一条消息） | 重试 + `eventId` 幂等 + `dropped` 日志；P1 阶段先只观察不动 |
| 两套传输层并存期间逻辑漂移 | 中 | 已消除：P4 删掉 node 传输路径，只剩网关一条 |
| 附件下载改成二进制透传 | 低 | 现有实现对 >20MB 有截断逻辑，保留在桥接层，gw 只负责流 |

## 10. 工作量

- Go 侧：约 450–550 行（含测试），1 人日
- TS 侧改动（`feishu-gw-client.ts` + `feishu.ts` 调用点替换 + 测试 mock 改造）：约 0.5–1 人日
- launchd / agentctl / doctor / 文档：0.5 人日
- **合计 2–2.5 人日**，其中 P1 单独可在半天内验完。

对照：整包 Go 化桥接层（`src/` 全部重写，2910 行 feishu.ts + 调度/SQLite/agent 循环）估 2–3 周，
且要重跑全部 248 个测试与线上灰度——除非目标就是「整个 runtime 统一 Go」，否则不做。

### 8.5 死 token 自愈（2026-11-24）
线上复盘：13:53 代理抖动把 SDK 的 token 刷新打断，之后网关一直拿一个**飞书已作废、本地还没到
过期时间**的 tenant token 发请求，14:20 起每次都是 `99991663 Invalid access token`。症状是最难
查的一种：进程活着、`/healthz` 还报 `connected:true`、长连接照收事件，只是发不出去——桥接层那侧
表现为「turn start 之后一条日志都没有」（卡在 `card created`），用户看到的是「无响应」。

根因在 SDK 的行为：tenant token 在内存里缓存到它自己记的过期时刻，飞书回 `99991663/99991668`
它**不重取也不重试**。所以修法不是加长重试，而是换一个没有旧缓存的东西：
- `send.go` 新增 `clientSet`，持有 `build func() *lark.Client`；`guard()` 包住五个操作和
  `bot/v3/info`，只在 `isStaleToken(err)`（= `99991663`/`99991668`）时 `reset()` 重建 client 并
  **重试一次**。其它错误（超时、限流、消息已撤回）一次都不重试——超时重试会发两遍。
- `reset()` 只在「手里这个 client 仍是失败的那个」时才重建，一串并发失败只重建一次。
- 重建就是全部修复：新 client 的 token cache 是空的，下一次调用自然重新取 token。
- `newServer` 因此收 `build func() *lark.Client` 而不是 client（凭据只有 `main` 知道），
  测试传 `nil` 仍然可用。日志多一条 `{"t":"stale token","what":...,"err":...}`。

实测：`99991663` 之后自动恢复，无需 `kickstart`；`go test ./...` 全绿（`token_test.go` 5 个用例
钉住「只重试死 token、只重试一次、并发只重建一次」）。

## 11. 决策点

评审时只需要回答一个问题：**换 Go 的动机是「可靠性」还是「技术栈统一」？**

- 可靠性 → 按本方案做 P1/P2 即可，P3 观察一周再定。
- 技术栈统一 → 跳过本方案，另提全量重写方案（工作量大 10 倍）。
- 两者都不是（只是嫌 Node SDK 的类型） → 不做，把 `card()` 的 JSON 类型写严谨即可。
