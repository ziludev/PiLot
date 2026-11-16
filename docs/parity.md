# 功能对齐表（React 客户端 → Go/mygo）

基线：`/Users/alexwang/learn/pi-durable-agent/client/src`（React 19 + shadcn + zustand）。
本仓：`/Users/alexwang/work/pitta-mygo`（Go + mygo/ui v0.9.1，原生 GPU 渲染；版本选择与降级实验见 `docs/mygo-version.md`）。
状态用词：`TODO` 未开始 / `WIP` 进行中 / `done` 已对齐 / `diff` 已实现但与 React 有差异 / `n/a` 不复刻（并说明原因）。

## 1. store.ts 状态域（State 的 50 个字段归并成下面 29 行）

| 状态域 | React 行为 | Go 实现 | 状态 |
| --- | --- | --- | --- |
| `lang` | localStorage `agent.lang` + navigator.language，「Aa」切换 | `internal/i18n` + `App.prefs.Lang`（`~/.pitta/ui.json`），rail 头「Aa」 | done |
| `home` | 后端给的 agent home | `internal/bridge.Home()` | done |
| `connection` / `retryAttempts` / `reachable` / `lastOpenAt` | connecting/open/down + 30×1s 重试 | `App.conn/downReason/retry/reachable/lastOpen`，`api.Watch` 驱动 | done |
| `busy` | 提交中禁用输入 | `App.busy` | done |
| `surfaces` / `current` | 会话列表与当前会话 | `App.surfaces` / `App.current` | done |
| `projects` / `currentProject` | 项目作用域 | `App.projects` / `App.project` + `Rail.AppProjectBar` | done |
| `tasks` | 运行中任务（rail 底部，可点开） | `App.tasks` → `railTasks` → `openTask` → `ops.TaskDialog` | done |
| `entries` / `live` | transcript 数据源与流式文档 | `App.entries` / `App.live`（`Docs["pi.live"]`） | done |
| `memory` / `memoryQuery` / `memorySuggestions` / `curating` | 三类笔记 + 整理建议 | `ops.MemoryPanel` + `loadMemory/curateMemory/applyMemory/dropSuggestion` | done |
| `access` / `audit` | 授权表与审计尾 | `ops.AccessPanel` + `loadAccess` | done |
| `taskDetail` | 任务详情弹窗 | `App.taskOpen` → `ops.TaskDialog` | done |
| `changes` / `changesLoading` / `commitDiffs` | 未提交改动、提交补丁缓存 | `App.changes/changesLoading/commitDiffs` → `side.ChangesView/CommitsView` | done |
| `order` / `pinned` | localStorage 排序/置顶 | `App.prefs.Order/Pinned` | done |
| `tab` | SidePanel 的 changes/memory/access/usage | `App.sideTab` + `App.sideOpen` | done |
| `view`（transcript / file） | transcript 与文件视图互斥 | `App.view`/`file`/`filePath` → `side.FileView` | done |
| `filter` | 会话内搜索命中 | `App.filter/convQuery/convMatches/matchIndex` → `findBar` | diff |
| `review` | 行级评审批注（可发送给 agent） | `App.review/reviewDraft/reviewPick` + `side.ChangesView` | done |
| `focusEntry` | 搜索命中后把视图滚到那一条 | `App.focusEntry` → `transcript` 的窗口锚点 `focusedTurn()` | done |
| `convQuery` / `convMatchIndex` | 命中轮次列表与当前命中下标 | `App.convQuery/convMatches/matchIndex` → `findBar` 的 `N/M` 与上下步进 | done |
| `showArchived` | 是否显示已归档会话 | `App.showArchived` → `railArchiveToggle` | done |
| `model` / `thinkingLevel` / `models` / `modelsLoadedAt` | 模型与思考档 | `App.model/thinking/models` | done |
| `favorites` / `recents` | 收藏与最近模型 | `App.modelsFav/modelsRecent`（落盘 prefs） | done |
| `usageDays` / `usage` | 用量日历与花费 | `ops.UsageCalendar` + `App.usage/usageDays` | done |
| `approvals` / `approvalWaits` | 待批工具调用、等待时间窗 | `App.approvals/waits` → `approvalBar` | done |
| `convQuery` / `convMatches` / `matchIndex` | 会话内搜索（rail 列表） | `findBar` + `searchTurns/stepMatch/focusedTurn` | diff |
| `errors` | 错误面板（角标 + 列表 + 清空） | `App.errs` → `errorPanel` | done |
| `git` / `feishu` | 侧栏 Git 与飞书健康 | `App.git` → `side.CommitsView`；`App.feishu` 仅数据 | diff |
| `composerDraft` | 「编辑重发」推入输入框 | `App.draft` | done |
| `surfaceFilter` | all/client/feishu 过滤 | `App.prefs.SurfaceFilter` | done |

## 2. 组件（`client/src/components` 下 24 个 `.tsx`，另有 shadcn 的 `ui/` 目录）

| 组件 | 职责 | Go 落点 | 状态 |
| --- | --- | --- | --- |
| `Rail.tsx` | 会话列表、过滤、置顶/归档、任务、底部控制 | `internal/app/rail.go` + `railview.go` | done |
| `ConvSearch.tsx` | rail 顶部会话搜索 | `railSearch`（rail.go） | done |
| `ProjectBar.tsx` | 项目切换 / 打开 / 移除 | `internal/app/projectbar.go` | done |
| `Transcript.tsx` | 回合列表、左右气泡、统计条、窗口化、通知 | `internal/app/transcript.go`（60 轮窗口 + earlierTurns） | diff |
| `TurnRail.tsx` | 每步的阶段色点竖轨 | `stepRow` 前置色点（无竖连线） | diff |
| `TurnDurations.tsx` | 思考/工具/等待耗时分段条 | `phaseBar`（180×3px 分段条，无图例） | diff |
| `ToolOutput.tsx` | 工具输出（diff/文本/失败色） | `stepOutput` + `diff.View` | done |
| `Composer.tsx` | 输入、steer、发送、模型入口 | `internal/app/composer.go`（Cmd/Ctrl+Enter 发送） | done |
| `ApprovalBar.tsx` | 审批三按钮 + 等待秒数 | `approvalBar`（composer.go） | done |
| `ModelPicker.tsx` | 搜索、收藏、最近、思考档 | `internal/app/modelpicker.go` | diff |
| `SidePanel.tsx` | changes/memory/access/usage 四标签 | `internal/app/sidepanel.go`（标签栏）+ `internal/side` + `internal/ops` | done |
| `FileViewer.tsx` | 只读文件 + 行号 + 行批注 | `internal/side/file.go` + `a.openFile` | done |
| `CommitDialog.tsx` | 提交信息弹窗 | `internal/side/commit.go` | done |
| `Commits.tsx` | 提交列表、补丁展开、push、compare | `internal/side/commits.go` | done |
| `MergeButton.tsx` | 合并基分支与结果文案 | `internal/side/merge.go` | done |
| `AccessPanel.tsx` | 授权级别、路由、审计 | `internal/ops/access.go` | diff |
| `TaskDialog.tsx` | 任务详情与取消 | `internal/ops/task.go` | done |
| `UsageCalendar.tsx` | 120 天花费热力格 | `internal/ops/usage.go` | done |
| `ChatMenu.tsx` | 重命名/归档/清理 worktree | `internal/app/search.go`（`chatMenuDialog`） | done |
| `SearchBox.tsx` | 会话内搜索框与上下步进 | `findBar`（search.go） | done |
| `ThemeToggle.tsx` | 深浅色切换 | `toggleTheme`（rail 头「◐」） | done |
| `ErrorPanel.tsx` | 错误角标 + 列表 + 清空 | `internal/app/errorpanel.go` | done |
| `BotAvatar.tsx` | 飞书会话头像 | 未画，rail 用 kind 图标区分 | diff |
| `Images.tsx` | 输入区图片附件（粘贴）、会话内缩略图与全屏预览 | `internal/app/images.go`（Paperclip→路径对话框、`ui.DecodeBitmap` 缩略图、预览弹层、上限 3 张） | diff |

`client/src/components/ui`（17 个 shadcn 原语）不复刻：mygo `ui` 包自带 Button/Select/Popover/Modal/Tabs/Segmented/Tooltip 等，外观由 `internal/theme` 的 shadcn token 决定。

## 3. lib 模块（`client/src/lib`，8 个文件）

| 模块 | 职责 | Go 落点 | 状态 |
| --- | --- | --- | --- |
| `api.ts` | HTTP 客户端与端点 | `internal/api`（client.go + watch.go） | done |
| `turns.ts` | 回合切分、统计、阶段 | `internal/turns`（turns.go + text.go） | done |
| `turn-state.ts` | 单回合 live 阶段 | `turns.State`/`Status` | done |
| `diff.tsx` | patch 解析与渲染 | `internal/diff`（diff.go 解析 + render.go 渲染） | done |
| `markdown.tsx` | Markdown 渲染 | `ui/markdown`（mygo 自带，View/Stream） | done |
| `i18n.ts` | 中英文案 | `internal/i18n`（183 键逐字抽取） | done |
| `motion.ts` | 入场/脉冲动画 | 未实现：mygo 帧循环无过渡 API | n/a |
| `avatar.ts` | 头像首字母与配色 | 未实现（同 BotAvatar） | diff |

## 4. 已知差异

1. **无 localStorage**：偏好存 `~/.pitta/ui.json`（原子写），字段与 localStorage 键一一对应。
2. **无 Tailwind/Radix**：外观由 `internal/theme` 的 shadcn zinc 换算值驱动，每帧写入 `ui.Theme`；`Radius=10`（0.625rem）。
3. **无 motion**：mygo 帧循环没有过渡/动画原语，入场与脉冲省略。
4. **会话内搜索是「回合跳转」**：⌘F 打开查找条（`internal/app/search.go`，与 React `ConvSearch.tsx:16` 同一手势；mygo 的 `OnWindowShortcut` 修饰键按完全匹配，故注册 `ui.Cmd`），但定位方式是给命中回合加主色外框并把窗口移到该回合，不滚动、不逐 entry 高亮。
5. **TurnRail/TurnDurations 是简化版**：色点没有竖连线，耗时分段条没有逐段图例。
6. **ModelPicker 是弹层而非命令面板**：留在 composer 上方弹出，搜索/收藏/最近/思考档齐备，没有键盘方向键导航。
7. **AccessPanel** 用 All/Group/Owner/Closed 过滤代替 React 的分组表头，审计固定 40 条（与 React `limit=40` 一致）。
8. **图片靠路径而非粘贴**：mygo 的剪贴板 API 只有文本（`windowHost.ReadText`），`OnPaste` 也只给字符串，所以输入区的 Paperclip 打开路径对话框（支持 `~`，png/jpg/gif/webp/bmp，最多 3 张），缩略图用 `ui.DecodeBitmap` 解码后 `ui.Image` 渲染，点击全屏预览；发送时随 `images` 一起提交，会话里用户消息上同样显示缩略图。
9. **通知**：React 在窗口隐藏时调 `Agent.notify`；这里用 `osascript display notification`，只在 `win.IsFocused()` 为假时触发，`internal/notify`。
10. **会话列表长度**：`/api/conversation?limit=200` 与 React 相同，服务端上限 200，不改。
11. **飞书健康横幅未绘制**：`feishu` 数据（`State.Feishu`）已随 `/api/state` 取回并保存，但 rail 上没有 React 那条连接状态横幅；用户可见差异，尚未实现。
12. **日志入口**：React rail 底部有 open-logs 按钮，这里同样接了「日志」（`bridge.OpenLogs()` 用 Finder 打开 `<home>/logs`），行为一致。

## 5. 写类操作的真实后端验证

`PITTA_LIVE=1 go test ./internal/app -run TestLiveWritePaths`，2026-11-16 对日常 agent（8787）执行：

| 操作 | 端点 | 结果 |
| --- | --- | --- |
| 切模型（并还原） | `POST /api/model` | ok：`magpie/sitin-deepseek/deepseek-flash` → `amazon-bedrock/amazon.nova-2-lite-v1:0`（thinking `off`），再还原 |
| 重命名（写回原值，幂等） | `POST /api/surface-meta` | ok，标题保持 `用一句话介绍你自己，并说出你当前在哪个工作目录` |
| push 试运行 | `POST /api/git/push`（dryRun） | ok：`status="no-remote"`（该 worktree 无 remote，未推送） |
| 提交 | `POST /api/commit` | 只在假桥测试（`internal/app/sidepanel_test.go`）：会改用户 worktree，不在真实仓库执行 |
| 合并 | `POST /api/merge` | 同上：会改 worktree |
| 审批决策（拒绝） | `GET` + `POST /api/approvals` | ok：新建会话要求 agent 执行 `sudo -n true`（guard 的 sudo 规则会挂起），客户端读到 `callId=call_00_Bv3PY4…`、`tool=bash`、`reason="提权执行：sudo -n true"`，再经 `api.DecideApproval{Approved:false}` 拒绝，`{ok:true}`，随后审批列表归零。复现：`PITTA_APPROVAL=1 go test ./internal/app -run TestLiveApprovalDecision -v`（拒绝，命令不会执行） |

`POST /api/surface-meta` 必须带 `surfaceId` 或可解析的 `conversationId`：老会话的 surface 可能已不存在，此时带 `conversationId` 会得到 400 `surfaceId or conversationId required`，故 `App.apiSurfaceMeta` 会顺带填 `surfaceId`。

## 6. 真实后端压测与全链路实测（2026-11-16）

`PITTA_LIVE=1` / `PITTA_E2E=1 go test ./internal/app -count=1`，对日常 agent（127.0.0.1:8787，74 个 surface）：

| 项 | 命令 | 实测 |
| --- | --- | --- |
| 最长会话 | `-run TestLiveLongConversation` | conversation 2：**200 条 entry / 31 轮 / 31 个回答**，首帧 5ms、后续帧 4ms（窗口 60 轮），落 `/tmp/pitta-long.png` |
| Markdown 无 panic | `-run TestLiveMarkdownRender` | 31 个真实回答逐个过 `markdown.View` 一帧，无 panic |
| patch 解析 | `-run TestLivePatchRender` | conversation 3201：**5 个文件 / 284 行 patch**，`diff.Parse` 解析后渲染一帧，落 `/tmp/pitta-patch.png` |
| 流式全链路 | `-run TestLiveStreamingFrames`（PITTA_E2E=1，花一次模型调用） | 新建 conversation 34832（surface `client:mwj40xno`），3 次 entry 变化，回答 `ok` |
| 审批 UI | `-run Snapshot` | 假桥待批项（45s 前创建）渲染 approval bar，落 `/tmp/pitta-app-approval.png` |

注意：`/api/conversation?limit=200` 给 200 条，而 WS watch 的首帧快照可能更少（这次是 50 条）；React 客户端同样用 `value.entries` 覆盖，行为一致，故不改。压测里因此直接用 HTTP 结果填 `entries`，避免被 watch 快照削短。

## 7. 输入法、剪贴板与选区

`go test ./internal/app -run TestPaste\|TestSelectAll\|TestIME -v`（`internal/app/input_test.go`）：

- 粘贴：`tt.SetClipboard` + `Cmd+V` 后草稿变为剪贴板文本。
- 全选 + Backspace 清空；`Shift+←` 选中后输入替换选中内容（`fresX`）。
- 中文 IME：`tt.Compose("nihao", 5)` 预编辑不进草稿、预编辑期间 Enter 既不发送也不换行；提交后草稿为 `你好`；`Cmd+Enter` 发出 `你好`。
- 字体：英文 Geist + 中文系统回退字体混排正常。`/tmp/pitta-fonts.png` 是 `internal/theme/fonts_test.go` 用 `ui.NewTester` **离屏渲染**的截图，只能证明字体注册与回退，**不是**真窗口输入法记录；真窗口的中文排版见 §7.1。

### 7.1 打包应用的真窗口记录

`~/Applications/Pitta.app`（`go tool mygo build -skip-dmg`，12.8MB）启动后连真实后端，截图确认：

| 项目 | 证据 |
| --- | --- |
| 中文排版与字体回退 | 真实回答「命令被系统拦下了，我没有执行。拦截原因写的是「未获批准（提权执行）」」在回答气泡里按 Geist + 系统 CJK 回退渲染，无豆腐块、无截断（`/tmp/pitta-manual/05-back.png`） |
| Markdown 渲染 | 同一窗口内多段中文回答、行内代码与列表正常（同上截图） |
| 流式 | 同一会话由真实模型逐段产出，耗时条显示 `1 tool · $0.00 · 3.0s`（`internal/app/live_test.go` 也断言过 `streamed=true`） |
| 审批拒绝在客户端可见 | 拒绝响应后，回答里出现「未获批准（提权执行）」，后端日志 `[approval] resolved {approved:false, by:"client"}` + `[guard] bash denied`（`internal/app/approval_live_test.go`） |

**尚未取得手工记录、仅有 tester 级证据的三项**（合成事件到不了该窗口：`AXIsProcessTrusted`/`CGPreflightPostEventAccess` 均为 true、鼠标移动事件生效，但点击 rail 行或 Search 图标、`keyboardSetUnicodeString` 打字都不引起 UI 变化；`internal/app/input_test.go` 覆盖同样的逻辑）：

- Shift+Enter 在真窗口里换行（测试 `TestEnterKeepsNewline`）
- Shift+← 选区后输入替换（测试 `TestSelectAllAndBackspaceClears`）
- Cmd+V 粘贴系统剪贴板（测试 `TestPasteInsertsClipboardText`）

这三项与中文 IME 需要人工在窗口里点一次确认。为此提供 `scripts/manual-check.sh`：它逐步提示操作、每步截一张窗口图并写 `docs/manual-checks/results.txt`，跑完即为手工验证记录。
