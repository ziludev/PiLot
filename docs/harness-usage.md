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
| 图片输入（飞书图片直接进模型，最多 3 张） | `src/feishu.ts:3223` |

## 没用上的，按值不值得做排序

1. **持久子 agent（pattern 22）**：能 spawn、steer 中途改方向、stop、list，答完主动回报父会话。
   我们现在只有「同步等结果」的 `delegate` 和「跑完发条消息」的流水线，中间插不上话，也看不见它
   在干什么。这是从「派活」到「带团队」的那一步。
2. **`onReport` 报警**：现在只写日志（`src/harness.ts:266`）。钩子抛错、任务 faulted、存储失败、
   扩展没装上导致任务一直 blocked，全都不出声——这类故障要靠 alex 觉得「怎么没动静」才发现。
   做法：错误分类，storage/task faulted 立刻推一条飞书消息。
3. **子任务并行 + `waiting` + `policy: failFast`**：`build_feature` 现在是顺序 await，designer 与
   reviewer、多份测试本可以并行；用子任务模型还能拿到「失败自动 abort 其他分支」和重启续跑。
4. **`watchTaskGraph` / `inspect()`**：任务面板靠轮询库表画，「哪些任务等谁」「哪个被卡住」得自己算；
   库里直接给一张实时图。
5. **`CompactionTask.beforeCompact`**：自动摘要用的是通用提示词；我们的记忆规则（alex 的偏好、
   分支红线）比通用摘要更该被保留，可以让它按我们的模板写摘要。
6. **`control: { terminate: true }`**：工具直接结束这一轮，省一次模型请求。`report_done`、后台
   `build_feature` 这种「说完就没别的事」的场景正合适。
7. **`afterTools` / `onYield`**：现在是「撞上限 → 结算 → 我们提交 continue」，库里可以在钩子里就
   接着跑，少一次状态往返、也少一个 `turn_limit` 的中间态。

## 看了但决定不用

- `whenIdle: "reject"`：飞书去重已经靠 `requestId` 幂等（`src/feishu.ts:2698`）。
- `submit(tx, draft)`：除非要把「标记消息已处理」和「入队」写进同一个 commit，否则不值当。
- `structuredOutput`：工具结果主要给人看，`bash` 的 exitCode 已经够脚本判断。
- `callers` / `modelTools` 再细分：`tool_search` 已经把长尾挡住了。
- `ConversationTranscript`：分页在 bridge 的 HTTP 层做，客户端拿的是渲染好的块。
- `createNodePhotonImages`：飞书图片走 provider 原生图片输入，没用到不需要的格式转换。
- 各存储后端：SQLite 一开到底，JSONL / IndexedDB 没场景。
