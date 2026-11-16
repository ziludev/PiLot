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
| `order` / `pinned` | 拖拽排序与置顶（落盘） | `App.prefs.Order/Pinned` ← `railListState.Reorder` → `reorderChats`（拖到首行会置顶，同 `Rail.tsx:302-316`） | done |
| `tab` | SidePanel 的 changes/memory/access/usage | `App.sideTab` + `App.sideOpen` | done |
| `view`（transcript / file） | transcript 与文件视图互斥 | `App.view`/`file`/`filePath` → `side.FileView` | done |
| `filter` | 会话列表按标题过滤 | `App.query`（输入即过滤，`sortedSurfaces` 用 `strings.Contains`） | done |
| `review` | 行级评审批注（可发送给 agent） | `App.review/reviewDraft/reviewPick` + `side.ChangesView` | done |
| `focusEntry` | 搜索命中后把视图滚到那一条 | `App.focusEntry` → `transcript` 的窗口锚点 `focusedTurn()` | done |
| `showArchived` | 是否显示已归档会话 | `App.showArchived` → `railArchiveToggle` | done |
| `model` / `thinkingLevel` / `models` / `modelsLoadedAt` | 模型与思考档 | `App.model/thinking/models` | done |
| `favorites` / `recents` | 收藏与最近模型 | `App.modelsFav/modelsRecent`（落盘 prefs） | done |
| `usageDays` / `usage` | 用量日历与花费 | `ops.UsageCalendar` + `App.usage/usageDays` | done |
| `approvals` / `approvalWaits` | 待批工具调用、等待时间窗 | `App.approvals/waits` → `approvalBar` | done |
| `convQuery` / `convMatches` / `matchIndex` | 会话内查找：命中轮次、当前命中下标（⌘F） | `App.convQuery/convMatches/matchIndex` → `findBar` 的 `N/M` 与上下步进（`searchTurns/stepMatch/focusedTurn`） | done |
| `errors` | 错误面板（角标 + 列表 + 清空） | `App.errs` → `errorPanel` | done |
| `git` / `feishu` | 侧栏 Git 与飞书健康 | `App.git` → `side.CommitsView`；`App.feishu` → `railFeishu` 断连横幅（数据 + 渲染） | done |
| `composerDraft` | 「编辑重发」推入输入框 | `App.draft` ← 气泡上的 `messageActions` 编辑按钮 | done |
| `surfaceFilter` | all/client/feishu 过滤 | `App.prefs.SurfaceFilter` | done |

## 2. 组件（`client/src/components` 下 24 个 `.tsx`，另有 shadcn 的 `ui/` 目录）

| 组件 | 职责 | Go 落点 | 状态 |
| --- | --- | --- | --- |
| `Rail.tsx` | 会话列表、拖拽排序、过滤、置顶/归档、任务、底部控制 | `internal/app/rail.go` + `railview.go`（`ListState.Reorder` → `reorderChats`，拖到首行即置顶） | done |
| `ConvSearch.tsx` | ⌘F 打开的会话内查找条（上一个/下一个命中） | `findBar`（Search.go） | done |
| `ProjectBar.tsx` | 项目切换 / 打开 / 移除 | `internal/app/projectbar.go` | done |
| `Transcript.tsx` | 回合列表、左右气泡、复制/编辑/重试、窗口化、新消息跳转、空态、通知 | `internal/app/transcript.go`（60 轮窗口 + earlierTurns + `jumpBar` + `noConversation` 空态；`followScroll` 打开即置底并在新条目到达时继续跟随，上翻后才计数） | diff |
| `TurnRail.tsx` | 每步的阶段色点竖轨 | `stepRow` 前置色点 + 步骤组左侧 1px 竖线（`transcript.go` 的 `BorderWidth(0,0,0,1)`） | diff |
| `TurnDurations.tsx` | 思考/工具/等待耗时分段条 | `phaseBar`（180×3px 分段条 + `phaseLegend` 逐段时长） | diff |
| `ToolOutput.tsx` | 工具输出（diff/文本/失败色） | `stepOutput` + `diff.View` | done |
| `Composer.tsx` | 输入、steer、发送、模型入口 | `internal/app/composer.go`（Cmd/Ctrl+Enter 发送） | done |
| `ApprovalBar.tsx` | 审批三按钮 + 等待秒数 | `approvalBar`（composer.go） | done |
| `ModelPicker.tsx` | 搜索、收藏、最近、思考档 | `internal/app/modelpicker.go` | diff |
| `SidePanel.tsx` | changes/memory/access/usage 四标签、可拖宽并记忆 | `internal/app/sidepanel.go` + `internal/side` + `internal/ops`；宽度由 `pane` 的 `ui.Split` 决定并写回 `prefs.SideWidth` | done |
| `FileViewer.tsx` | 只读文件 + 行号 + 行批注 | `internal/side/file.go` + `a.openFile` | done |
| `CommitDialog.tsx` | 提交信息弹窗 | `internal/side/commit.go` | done |
| `Commits.tsx` | 提交列表、补丁展开、push、compare、基分支选择 | `internal/side/commits.go`（`baseSelect` → `OnSetBase` → `api.SetGitBase`） | done |
| `MergeButton.tsx` | 合并确认对话框、脏工作区禁用、冲突交给 agent | `internal/side/merge.go`（`MergeButton` + `MergeDialog`）；`App.openMergeDialog/mergeBranch/askAgentToResolve` | done |
| `AccessPanel.tsx` | 授权级别、路由、审计 | `internal/ops/access.go` | diff |
| `TaskDialog.tsx` | 任务详情与取消 | `internal/ops/task.go` | done |
| `UsageCalendar.tsx` | 120 天花费热力格 | `internal/ops/usage.go` | done |
| `ChatMenu.tsx` | 重命名/归档/清理 worktree（未合并分支拒绝并提示） | `internal/app/search.go`（`chatMenuDialog` + `removeWorktree` 读 `status=="unmerged"` → `unmergedWork`） | done |
| `SearchBox.tsx` | rail 搜索框：输入过滤标题，提交走 `/api/search` 列命中 | `railSearch` + `searchHitsPanel`（rail.go）/ `api.Search` | done |
| `ThemeToggle.tsx` | 跟随系统 / 浅色 / 深色 三态循环 | `toggleTheme` + `nextTheme`（rail 头图标随状态换 Monitor/Sun/Moon） | done |
| `ErrorPanel.tsx` | 错误角标 + 列表 + 清空 | `internal/app/errorpanel.go` | done |
| `BotAvatar.tsx` | rail 里会随工作状态下表情的饼脸头像 | `internal/avatar`（`avatar.View`） | done |
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
| `avatar.ts` | 头像的 PIE 几何、mood 与配色 | `internal/avatar`（`Mood`/`Tones`） | done |

### 2026-11 视觉对齐

- 顶栏 48 px：标题 13/500、标识 10 px 等宽字体，右侧图标按钮 24 px 无底色、`PanelRight` 固定最右。
- 会话行 28 px 单行（图标 + 标题 + 花费），`#id` / 菜单 / 置顶在指针下出现，与 `SessionRow` 一致。
- 会话区头部一行：`SESSIONS` + 紧凑 Select（`All · 3`）+ 归档/新建图标；页脚一行：重启/备份/日志 + `~/…` 路径。
- 气泡 13 px、圆角 12、内边距 8/12；回合间距 8；消息操作只在悬停时出现。
- 左栏默认 21% 窗宽（16%–30%），右栏默认 26%（18%–34%），与 `react-resizable-panels` 的 defaultSize/minSize/maxSize 对应。

## 4. 已知差异

1. **无 localStorage**：偏好存 `~/.pitta/ui.json`（原子写），字段与 localStorage 键一一对应。
2. **无 Tailwind/Radix**：外观由 `internal/theme` 的 shadcn zinc 换算值驱动，每帧写入 `ui.Theme`；`Radius=10`（0.625rem）。
3. **无 motion**：mygo 帧循环没有过渡/动画原语，入场与脉冲省略。rail 顶部的 BotAvatar 会按状态换表情（思考/完成微笑 2.5 秒）与配色（委派任务转蓝、完成转青），但省掉了 React 版的眨眼、注视与跳跃缓动。
4. **会话内搜索是「回合跳转」**：⌘F 打开查找条（`internal/app/search.go`，与 React `ConvSearch.tsx:16` 同一手势；mygo 的 `OnWindowShortcut` 修饰键按完全匹配，故注册 `ui.Cmd`），但定位方式是给命中回合加主色外框并把窗口移到该回合，不滚动、不逐 entry 高亮。
5. **TurnRail/TurnDurations 是简化版**：步骤左侧有竖连线与色点，但没有 React 那种跨步骤连成一体的圆角轨；耗时分段条带逐段图例（`think/tool/wait/error` 各自时长），但没有 hover 提示。
6. **ModelPicker 是命令面板，但不是浮层**：列表画在 composer 上方的同一栏里（mygo 的 `ui.Popover` 会把 Enter 当成自己的关闭键，而 Enter 正是选中的键，所以改为内联面板），搜索/收藏/最近/思考档齐备，↑↓ 移动高亮、Enter 选中；没有 React 的 provider 分组折叠与滑块式思考档。
7. **Memory 建议的勾选语义一致、排版不同**：`SidePanel.tsx:231` 的 `checked={selected[index] !== false}` 对应 `ops.MemoryProps.Selected` + `OnToggle`，Apply 只提交勾选项；差别只剩搜索框与「全部应用」的位置更紧凑。
8. **AccessPanel** 用 All/Group/Owner/Closed 过滤代替 React 的分组表头，审计固定 40 条（与 React `limit=40` 一致）。
9. **图片靠路径而非粘贴**：mygo 的剪贴板 API 只有文本（`windowHost.ReadText`），`OnPaste` 也只给字符串，所以输入区的 Paperclip 打开路径对话框（支持 `~`，png/jpg/gif/webp/bmp，最多 3 张）——React 支持把图片直接粘进输入区，mygo 的剪贴板只给文本，这条差异用户可见。缩略图用 `ui.DecodeBitmap` 解码后 `ui.Image` 渲染，点击全屏预览；发送时随 `images` 一起提交，会话里用户消息上同样显示缩略图。
10. **通知**：React 在窗口隐藏时调 `Agent.notify`；这里用 `osascript display notification`，只在 `win.IsFocused()` 为假时触发，`internal/notify`。
11. **会话列表长度**：`/api/conversation?limit=200` 与 React 相同，服务端上限 200，不改。
12. **飞书健康横幅**：已按 `Rail.tsx:426-435` 绘制（`ready=false && downMs>60000` 时画红条 + `PlugZap` + "飞书连接断开 Nm"），差别只在 React 的 hover 提示（`lastError`）没有 tooltip 承载，文案直接显示在条内。
13. **新消息跳转是一条窄条而不是浮层**：`Transcript.tsx:331` 把「↓ N 条新消息」浮在列表右下角，mygo 没有绝对定位，所以画在 transcript 与 composer 之间的一条右对齐窄条（滚动离开末尾才出现，点击 `ScrollTo(0, MaxFloat32)`）；空会话用 `noConversation` 文案代替空白。
14. **右栏拖宽用 `ui.Split`**：React 是 `PanelGroup + PanelResizeHandle`（比例伸缩），这里是 `Split` 定宽第一格（main），面板宽度取余量并写回 `prefs.SideWidth`；`SplitReset(760)` 是双击分隔条复位到的宽度。
15. **真窗口记录**：`docs/manual-checks/62-jump-bar.png`（停在历史位置时出现「5 条新消息」跳转条）与 `63-follow-newest.png`（修好跟随之后，长会话 #35773 打开即在最新一轮，跳转条不再出现）。
16. **日志入口**：React rail 底部有 open-logs 按钮，这里同样接了「日志」（`bridge.OpenLogs()` 用 Finder 打开 `<home>/logs`），行为一致。
17. **毛玻璃用 mygo 的 `plugins/glass`**：React 用 `.glass`（`backdrop-filter: blur(22px) saturate(180%)`），这里用 Liquid Glass 材质（`internal/app/glass.go` 的 `panelGlass()`/`pillGlass()`），已挂在会话菜单、Toast、错误胶囊、跳转条上；像素级验证 `TestGlassSamplesWhatIsUnderIt`（背景透出、提亮、细边高光）。系统 `glass.Native`（`NSGlassEffectView`）采样不到同帧元素，故用着色器版。

rail 的外观（头像、状态行、搜索命中）见 `docs/snapshots/rail-{light,dark}.png`，由 `TestSnapshotRail` 生成；默认写到临时目录，刷新留存副本用 `PITTA_SNAPSHOT_DIR=../../docs/snapshots go test ./internal/app -run TestSnapshotRail`。

消息级操作（复制 / 编辑重发 / 重试，`Transcript.tsx:20-46`）实现在 `internal/app/transcript.go` 的 `messageActions`：复制走 `c.Services().WriteClipboard` + `c.Toast("已复制")`，编辑把原文写回 `App.draft`，重试重发该回合的用户输入。React 是 hover 才显形（`group-hover:opacity-100`），mygo 没有分组 hover，这里常显为淡灰小图标。

拖拽排序用 mygo 的 `ui.List` 原生行拖拽（`ListState.Reorder(func(rows []int, to int))`），落盘到 `prefs.Order`；`sortedSurfaces` 按「置顶 → order 下标 → 会话号倒序」排序，与 `Rail.tsx:227-239` 一致。差别只有 React 的 dnd-kit 有拖拽预览浮层，这里用列表自带的拖影。

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
| 中文排版与字体回退 | 真实回答「命令被系统拦下了，我没有执行。拦截原因写的是「未获批准（提权执行）」」在回答气泡里按 Geist + 系统 CJK 回退渲染，无豆腐块、无截断（`/tmp/pitta-manual/05-back.png`，仓库外临时文件） |
| Markdown 渲染 | 同一窗口内多段中文回答、行内代码与列表正常（同上截图） |
| 流式 | 同一会话由真实模型逐段产出，耗时条显示 `1 tool · $0.00 · 3.0s`（`internal/app/live_test.go` 也断言过 `streamed=true`） |
| 审批拒绝在客户端可见 | 拒绝响应后，回答里出现「未获批准（提权执行）」，后端日志 `[approval] resolved {approved:false, by:"client"}` + `[guard] bash denied`（`internal/app/approval_live_test.go`） |
| 点击会话行切换 | 点击 rail 行后 header 变为「话题 b6d61b #22603」（`docs/manual-checks/58-click-switches-chat.png`） |
| 打字进 composer | 点击 composer 后输入 `abc` 上屏（`docs/manual-checks/59-typing.png`） |
| Shift+Enter 换行 | 再按 Shift+Enter 并输入，得到空行 + `line-two`，未发送（`docs/manual-checks/60-shift-enter.png`） |
| 选区可见 | `Cmd+A` 后文本被蓝色高亮覆盖（`docs/manual-checks/56-selection.png`） |
| 选中后输入替换 | 保持全选再输入，原文本被整体替换（`docs/manual-checks/57-replace.png`） |
| `Cmd+V` 粘贴 | `pbcopy` 一段中英混排后按 `Cmd+V`，草稿出现该文本（`docs/manual-checks/51-paste.png`） |
| 中文 IME 预编辑 | 微信拼音键入 `nihao`，composer 内显示带下划线的 `ni'ha'o`，候选窗贴光标下方，预编辑不进草稿（`docs/manual-checks/52-ime-preedit.png`） |
| 组词期间按 Enter | 不发送、不插换行；微信输入法把原始拼音落成字面 `nihao`（输入法行为，见 `docs/manual-checks/results.txt`） |
| 空格提交候选 | 草稿上屏 `你哈儿`，中文渲染无豆腐块（`docs/manual-checks/54-ime-commit.png`） |

这张表里"合成点击/键盘"在 0.9.1 的窗口上是可用的；早先记录过"点击到不了窗口"，那是另一个 bug 的假象：

**点一下就冻死**。rail 行的 `OnClick` 调 `selectConversation` → `applyWait`，而 `applyWait` 当时是"把写入投递到主线程 + 等 channel"，回调本身就在主线程上，于是主线程等主线程，窗口永久冻结（macOS 显示"应用程序无响应"）。`kill -QUIT` 的栈直接指出来：

```
goroutine 1 ... [chan receive, locked to thread]:
pitta/internal/app.(*App).applyWait(...)   app.go:277
pitta/internal/app.(*App).selectConversation(...)  app.go:487
pitta/internal/app.(*App).railRow.func1()  rail.go:213
```

修法：`applyWait` 改用 `mygo.RunOnMain`（已在主线程就直接跑，否则投递并等待），并在 `race_test.go` 里钉住"点击处理路径不得阻塞"。**这条修复之前的窗口交互一律不可信**，包括当时判断"合成事件到不了这个窗口"的结论。

### 7.2 选区高亮曾不可见

主题把 `ui.Theme.Selection` 映射成了 shadcn 的 `--accent`（浅色 `#f5f5f5`、深色 `#262626`）——那是 hover 底色，用作文本选区就是白底上的近白、黑底上的近黑，选中了看不出来（用户实测提出："确实可以替换，但是没有选中的效果"）。现在 `theme.Tokens.Selection` 单独给出半透明蓝（浅 `RGBA(37,99,235,0.25)`、深 `RGBA(59,130,246,0.40)`），两条断言：

- `internal/theme` `TestSelectionIsVisibleComposited`：选区色必须半透明，且合成到背景后与背景至少差 8 级。
- `internal/app` `TestSelectAllHighlightsText`：composer 聚焦后 `Cmd+A`，逐像素比对新帧与"旧像素叠上主题选区色"，命中数 < 400 即失败（把映射改回 `--accent` 时该用例报 `0 pixels changed to the selection color`）。

### 7.3 会话菜单曾画出原始 i18n key

会话菜单把 key 写成了 `renameChat` / `archiveChat`，而 React 的字典里叫 `rename` / `archive`（归档后是 `unarchive`），于是按钮上直接显示 `renameChat`。现在改用真实 key，并加了两道防线：

- `internal/i18n` `TestEveryLiteralKeyExists`：遍历仓库源码里所有 `i18n.T("…")` / `TF("…")` 字面量，要求 en 与 zh 两个字典都有该 key（把 key 改回 `renameChat` 时该用例报 `key "renameChat" has no zh string`）。
- `internal/app` `TestChatMenuDrawsLabels` / `TestChatMenuOffersUnarchive` / `TestImageDialogDrawsLabels`：渲染对话框后，任何一段文本只要"本身是字典的 key"就判失败（`i18n.IsKey`）。
- `TestSnapshotChatMenu` 把菜单渲染成 `docs/snapshots/chat-menu-{light,dark}.png`（浅/深各一张，重跑该用例即重新生成），截图里按钮读作 `Save` / `Archive` / `Clean worktree` / `Cancel`，归档会话的版本读作 `Unarchive`。

### 7.4 记录的落点

真窗口那一轮的操作、截图与逐项结论写在 `docs/manual-checks/results.txt`（截图同目录 `51-paste.png` … `55-cleared.png`）。要重做一次，跑 `scripts/manual-check.sh`，它按同样的步骤提示操作并逐步截图。
