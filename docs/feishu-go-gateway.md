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

进程：`feishu-gw` 作为第三个 launchd job（`com.pitta.agent.feishu`，`KeepAlive`），只绑
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

Go 侧（新目录 `services/feishu-gw/`，独立 module `pitta/feishu-gw`）
- `main.go`：读 env（`FEISHU_APP_ID/SECRET`、`FEISHU_GW_TOKEN`、`BRIDGE_URL`、端口）
- `larkws` 客户端：`im.message.receive_v1`、`card.action.trigger` 两个 handler，连接状态上报 `/healthz`
- `sender.go`：5 个操作 → 5 个 handler，统一的错误封装与超时（写 10s / 读 15s）
- `dispatch.go`：事件投递、重试、丢弃日志
- 100% 覆盖 dispatch 的重试/超时分支（table-driven，无网络）

TS 侧
- `src/feishu.ts`：`client.*` 13 个调用点 → `gw.*`（一个新文件 `src/feishu-gw-client.ts`，约 120 行）
- `startFeishu`：`FEISHU_TRANSPORT=node|go` 分支——`node` 走老路（保留一个版本做回滚），`go` 只起 HTTP server 提供 `/feishu/event`、`/feishu/card-action`
- `feishuStatus` 改为周期拉 `/healthz`（2s 间隔），`/health` 的输出形状不变
- `scripts/agentctl`：`install-gw` / 卸载时一起 bootout；`status` 里多一行 gw 状态
- 测试：`feishu.ts` 的单元测试把 SDK mock 换成 HTTP mock，现存 248 个测试应当一断言都不改

## 8. 实施阶段与验收

每个阶段结束都能跑、能回滚。

| 阶段 | 内容 | 验收 |
|---|---|---|
| P1 连接层 | `feishu-gw` 只收事件、转发现有的 `[feishu] msg` 日志，不发消息 | `FEISHU_TRANSPORT=go` 时你发一条消息，桥接层日志出现同样的 chat/sender，且**只出现一次**（对照现在重复投递的记录） |
| P2 发送层 | 5 个操作全接上；卡片 create/reply/patch 走 gw | `npm run card` 发出 4 张自检卡；群里跑一轮完整对话（流式卡 → 最终卡 → 按钮点击），肉眼确认；`/healthz` 的 `flaps` 一周内不增长 |
| P3 切默认 | `FEISHU_TRANSPORT` 默认 `go`，node 路径标 deprecated | 连续 7 天无 `dropped eventId`、无 `patch failed` |
| P4 收尾 | 删 node 传输路径与 node-sdk 依赖（自检/doctor 脚本单独保留） | `grep -r node-sdk src/ scripts/` 只剩 doctor 相关；`npm test` 全绿 |

## 9. 风险

| 风险 | 影响 | 对策 |
|---|---|---|
| 多一个进程，起不来时飞书静默失联 | 高（收不到消息） | `/healthz` + `agentctl status` 聚合；bridge 每 5 分钟探一次，连续 3 次失败发飞书告警（gw 挂了只能靠文本消息以外的手段——用 macOS 通知 + 日志） |
| 按钮回调 2.5s 超时截断 | 中（点按钮看到超时提示，卡片没更新） | 现网 `set-model`/`open-chat` 都只碰 SQLite，实测 < 200ms；把慢操作（起 turn）继续放在「已 ack 后异步」这条线里，不在回调里做 |
| 事件 HTTP 投递丢 | 中（少一条消息） | 重试 + `eventId` 幂等 + `dropped` 日志；P1 阶段先只观察不动 |
| 两套传输层并存期间逻辑漂移 | 中 | node 路径冻结（只修 bug），P4 直接删 |
| 附件下载改成二进制透传 | 低 | 现有实现对 >20MB 有截断逻辑，保留在桥接层，gw 只负责流 |

## 10. 工作量

- Go 侧：约 450–550 行（含测试），1 人日
- TS 侧改动（`feishu-gw-client.ts` + `feishu.ts` 调用点替换 + 测试 mock 改造）：约 0.5–1 人日
- launchd / agentctl / doctor / 文档：0.5 人日
- **合计 2–2.5 人日**，其中 P1 单独可在半天内验完。

对照：整包 Go 化桥接层（`src/` 全部重写，2910 行 feishu.ts + 调度/SQLite/agent 循环）估 2–3 周，
且要重跑全部 248 个测试与线上灰度——除非目标就是「整个 runtime 统一 Go」，否则不做。

## 11. 决策点

评审时只需要回答一个问题：**换 Go 的动机是「可靠性」还是「技术栈统一」？**

- 可靠性 → 按本方案做 P1/P2 即可，P3 观察一周再定。
- 技术栈统一 → 跳过本方案，另提全量重写方案（工作量大 10 倍）。
- 两者都不是（只是嫌 Node SDK 的类型） → 不做，把 `card()` 的 JSON 类型写严谨即可。
