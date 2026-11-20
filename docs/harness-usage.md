# pi-durable 1.5.2：我们用了什么，没用什么

对着 `node_modules/@earendil-works/pi-durable/README.md` 逐节核对了一遍。结论：**底座用得算彻底
（会话、任务、文档、续跑、fork、compaction、codemode、tool-search 都在跑），缺的集中在两头——
"多 agent 之间怎么对话"和"出错时谁告诉你"**。

## 已经在用

| 能力 | 落点 |
| --- | --- |
| 会话 / 提交（input、steer、continue、reset+handoff） | `src/feishu.ts:2510`、`1898`、`2357`、`2934` |
| 任务（`defineTask`、休眠、子任务、`abortTask`） | `src/routines.ts`、`src/reminders.ts`、`src/subagents.ts` |
| `afterRun` + `runEndTask`（跑完自动起一个任务） | `src/title.ts:121` 起标题 |
| 钩子 `beforeTool` / `afterTool` / `beforeRequest` | `src/approval.ts:273`、`src/stall.ts:89`、`src/context-guard.ts:93` |
| 每会话 `configure`（模型、指令、工具集、`maxTurns`、cwd） | `src/subagents.ts:41`、`src/roles.ts`、`src/harness.ts` |
| 每会话独立 worktree + cwd | `src/worktree.ts`、`src/harness.ts:246` |
| fork（按分支血缘切会话） | `src/threads.ts:134` |
| compaction 设置、`keepRecentTokens` 调到 10k | `src/harness.ts:254` |
| codemode（脚本里跑工具） | `src/codemode.ts` |
| tool_search + `modelTools` 延迟加载 | `src/deferred.ts` |
| usage / 花费 | `src/budget.ts:47`、`src/server.ts:252` |
| 实时视图 `conversation.watch()`（卡片靠它画） | `src/live.ts:105` |
| 长期在岗的具名 agent（ownerless 会话 + 后台投递任务） | `src/agents.ts` |
| `onReport` → 飞书报警 | `src/alerts.ts` |
| 图片输入（飞书图片直接进模型，最多 3 张） | `src/feishu.ts:3223` |

## 已经补上的

- **持久子 agent**：`spawn_agent` / `tell_agent` / `list_agents` / `stop_agent`（`src/agents.ts`）。
  会话 ownerless，所以没有回合能 abort 掉它；每条消息由一个 `app.agent-turn` 后台任务投递并
  等答复，父会话说完话就闲下来，重启也能接着等。
- **静默故障**：`onReport` 里 `storage_failed` / `task_faulted` / `extension_error` 这类
  「不修就一直坏」的报告直接发飞书，同一故障 10 分钟只说一次。
- **评审阶段**：`build_feature` 末尾加 reviewer，直接读 diff（也看 tester 的结论）。

## 看完决定不做

- **`beforeCompact` 自定义摘要**：这个钩子只能「自己给一段摘要」或「拒绝压缩」，改不了压缩提示词；
  再跑一遍模型写摘要等于把压缩的成本翻倍。
- **`control: { terminate: true }`**：唯一想用的地方是 `report_done`，但终止会连带吞掉它后面的那句
  人话，而那句话正是 alex 在聊天里看到的结论。
- **`onYield`**：它是在模型给出答案的那一刻插话继续跑，跟我们「别反复催模型」的取向冲突；步数上限
  后的续跑已经能用了。

## 还没接的，按值不值得做排序

1. **`watchTaskGraph` / `inspect()`**：任务面板靠轮询库表画，「哪些任务等谁」「哪个被卡住」得自己算；
   库里直接给一张实时图。
2. **`afterTools`**：一轮结束后一次，适合放「这一轮要不要提醒他」这类聚合判断；现在分散在
   `afterTool` 里，每条工具调用都算一遍。

## 看了但决定不用

- `whenIdle: "reject"`：飞书去重已经靠 `requestId` 幂等（`src/feishu.ts:2698`）。
- `submit(tx, draft)`：除非要把「标记消息已处理」和「入队」写进同一个 commit，否则不值当。
- `structuredOutput`：工具结果主要给人看，`bash` 的 exitCode 已经够脚本判断。
- `callers` / `modelTools` 再细分：`tool_search` 已经把长尾挡住了。
- `ConversationTranscript`：分页在 bridge 的 HTTP 层做，客户端拿的是渲染好的块。
- `createNodePhotonImages`：飞书图片走 provider 原生图片输入，没用到不需要的格式转换。
- 各存储后端：SQLite 一开到底，JSONL / IndexedDB 没场景。
