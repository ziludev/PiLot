# 复核命令与结果

本文件是给复核者（人或审计 agent）用的：每条命令都能在这个仓库里直接重跑，输出贴的是本次运行的原始结果。时间 2026-11-16 20:21 CST，commit 见 `git log -1`。

## 1. 编译、静态检查、单测（不需要后端）

```
$ go build ./...
ok
$ go vet ./...
ok
$ go test ./internal/... -count=1
ok  	pitta/internal/api	1.192s
ok  	pitta/internal/app	0.531s
ok  	pitta/internal/avatar	0.341s
?   	pitta/internal/bridge	[no test files]
ok  	pitta/internal/diff	0.148s
ok  	pitta/internal/i18n	0.085s
ok  	pitta/internal/icons	0.109s
?   	pitta/internal/notify	[no test files]
ok  	pitta/internal/ops	0.424s
?   	pitta/internal/protocol	[no test files]
ok  	pitta/internal/side	0.386s
ok  	pitta/internal/theme	0.164s
ok  	pitta/internal/turns	0.126s
```

（`bridge`/`notify`/`protocol` 没有测试文件：前者是 8 行环境变量读取，后者是纯类型声明，`notify` 的用例在 `internal/app/notify_test.go` 里以行为断言覆盖。）

## 2. 主题像素断言（无头快照）

```
$ go test ./internal/theme -run 'TestThemeColorsMatchTokens|TestSelectionIsVisibleComposited' -v
$ go test ./internal/app -run 'Snapshot|TestSelectAllHighlightsText' -v
```

- 背景像素 == `Tokens.Background`，正文至少一处与背景不同色，主按钮像素 == `Tokens.Primary`，分隔线像素 == `Tokens.Border`（深色下按 alpha 合成后 ±2）。
- 选区高亮必须半透明且合成后与背景至少差 8 级；把映射改回 `--accent` 时 `TestSelectAllHighlightsText` 会报 `0 pixels changed to the selection color`。
- 渲染出来的文本只要"本身是字典 key"就判失败（`TestChatMenuDrawsLabels`、`TestImageDialogDrawsLabels` + `i18n.IsKey`）；把 key 写回 `renameChat` 时这两个用例会红。
- 快照默认写到系统临时目录（`/tmp/pitta-app-{light,dark,side-*,modelpicker,approval}.png`），`go test ./...` 不会改动受版本控制的文件；需要刷新仓库里的留存副本时跑 `PITTA_SNAPSHOT_DIR=../../docs/snapshots go test ./internal/app -run 'TestSnapshotChatMenu|TestSnapshotRail'`。

新增交互断言（同一命令可跑）：

```
$ go test ./internal/app -run 'TestModelPickerWalks|TestRailHeader|TestSearchHits|TestAvatar|TestMessageActions|TestRailReorder' -v
--- PASS: TestModelPickerWalksWithArrowKeys      Tab 聚焦搜索框 → ↑↓ 移动高亮 → Enter 选中
--- PASS: TestRailStatusFollowsTheBridge         rail 头部状态行跟着连接/忙碌变化
--- PASS: TestAvatarMoodsFollowTheRun            运行=思考、刚完成=微笑 2.5s、有后台任务=委派配色
--- PASS: TestRailHeaderDrawsAvatarAndStatus     头部同时画出头像与状态文案
--- PASS: TestSearchHitsPanelShowsHits           提交搜索后列出 `#会话 · 类型` 与摘要，点击跳转
--- PASS: TestMessageActionsCopyEditRetry        气泡上的复制/编辑重发/重试三个按钮真的生效
--- PASS: TestRailReorderSavesTheOrder           拖拽后写回 prefs.Order，拖到首行会置顶
--- PASS: TestThemeToggleCyclesThreeStates        跟随系统 → 浅色 → 深色 → 跟随系统
--- PASS: TestMergeDialogAsksFirstAndHandsConflictsOver  脏工作区禁用合并；冲突时把文件交给 agent
--- PASS: TestCommitsBaseSelectCallsSetGitBase    基分支下拉真的 POST /api/git/base
--- PASS: TestRemoveWorktreeReportsUnmergedBranch 未合并分支拒绝清理并提示
--- PASS: TestSidePanelWidthFollowsTheDivider     拖分隔条改右栏宽度并写回 prefs
--- PASS: TestTranscriptEmptyStateAndJumpBar      空会话提示 + 「N 条新消息」跳转（含点击后清零）
--- PASS: TestTranscriptFollowsNewestTurn         打开即在末尾、流式继续跟随、上翻后才计数、跳转后重新粘底
```

跨会话搜索走的是后端 `/api/search?q=`（`api.Search` → `protocol.SearchHit`），命中列表在 rail 的搜索框下方；点击命中会切换会话并把窗口移到含该 entry 的回合（`turnOfEntry`）。

## 3. 真实后端（`127.0.0.1:8787`，凭据来自 `~/learn/pi-durable-agent/.env`）

```
$ PITTA_LIVE=1 go test ./internal/app -count=1 -run 'TestLive' -v
--- PASS: TestLiveBridgeWindow (0.47s)      73 个 surface、默认会话、rail+header+composer 渲染
--- PASS: TestLiveSidePanels (0.54s)        changes/memory/access/usage 四个侧栏标签
--- PASS: TestLiveLongConversation (1.63s)  200 条消息的会话，31 轮、31 个回答
--- PASS: TestLivePatchRender (1.18s)       284 行 patch 的 diff 渲染
--- PASS: TestLiveMarkdownRender (1.06s)    31 个回答逐个过 markdown.View
--- PASS: TestLiveWritePaths (0.06s)        切模型、重命名、push dry-run，跑完还原
ok  	pitta/internal/app	5.260s
```

另有两条按需开启（会真实触发模型/写入，故默认跳过）：

```
$ PITTA_E2E=1 go test ./internal/app -run 'TestLiveTurnAgainstTheBridge|TestLiveStreamingFrames' -v
  新建会话 → 发一句 → 等到回答；断言 streamed=true（实测 conversation=34832, entries=3, text="ok"）

$ PITTA_APPROVAL=1 go test ./internal/app -run 'TestLiveApprovalDecision' -v
  让模型跑 `sudo -n true` → 客户端拒绝 → POST /api/approvals → 列表清空
  后端日志：[approval] resolved {approved:false, by:"client"} / [guard] bash denied
```

## 4. 打包

```
$ go tool mygo build -skip-dmg
$ ls -d build/darwin-arm64/Pitta.app     # 12.8MB
$ rsync -a --delete build/darwin-arm64/Pitta.app ~/Applications/
$ open ~/Applications/Pitta.app
```

装到 `~/Applications` 而不是 `/Applications`：后者已有一个旧的 mygo webview 版 Pitta（bundleID 同为 `com.example.agent`），不能覆盖。启动后 `logs/agent.log` 出现 `[http] client attached {"conversationId":…}`。

- `TestRailSearchIsCentred`：搜索框占位符必须落在框体垂直中线上（±1.5 px），把高度从内容高度硬压到 24 px 会让它偏低 2.5 px——这正是肉眼看到的"文字没上下居中"。

## 5. 字面量 key 守卫

```
$ go test ./internal/i18n -run TestEveryLiteralKeyExists -v
--- PASS: TestEveryLiteralKeyExists (0.01s)      # 遍历仓库源码，108 个字面量 key 必须在 en/zh 两个字典里
```

## 6. 真窗口输入

见 `docs/manual-checks/results.txt` 与同目录 10 张截图（点击切换、打字、Shift+Enter、选区可见、选中替换、粘贴、IME 预编辑+候选窗、组词期间 Enter、候选提交、清空）。

## 7. 已知差异

`docs/parity.md` §4 逐条列出（⌘F 的定位方式、附件靠路径而非粘贴、Toast 无动效、access 表头分组、memory 勾选排版、BotAvatar 无景深/眨眼、日志入口是图标按钮等）；跨会话 `/api/search` 与飞书横幅已接，见 §2 表与 `railFeishu`/`api.Search`。mygo 版本选择见 `docs/mygo-version.md`。

## 原生能力

| 用例 | 覆盖 |
| --- | --- |
| `TestCmdKOpensTheSwitcher` | ⌘K 打开切换器、列出全部会话、查询过滤 |
| `TestDropImageAttachesIt` | 拖入 PNG 变成附件 |
| `TestPasteImageFromClipboard` | 剪贴板图片 → 附件（字节一致） |
| `TestRightClickCopiesAMessage` | 右键菜单不误改剪贴板 |
| `TestAttachUsesTheSystemPanel` | 系统面板返回的图片变成附件（过滤器为 `image/*`） |
| `TestProjectPanelAsksForADirectory` | 打开项目走目录面板并 POST `/api/projects` |
| `TestSecondaryWindowKeepsTheOwnedState` | 第二窗口打开指定会话、共享客户端、不写 ui.json、不设角标 |
| `TestRailOffersASecondWindow` | 会话菜单的开窗项把会话号交给 main 的窗口工厂 |
| `TestExportWritesTheChatAsMarkdown` | 导出文件含标题/问题/回答/成本行 |
| `TestExportNamesTheFileSafely` | 文件名不含路径分隔符，空标题回退 `chat #id.md` |
| `TestSearchHitMarksTheEntry` | 搜索命中的条目被标记，跳转清空后不再标记 |
| `TestCopyCarriesMarkdown` | 剪贴板同时携带 `text/markdown` 与纯文本 |
| `TestRunningChatsListsBusyOnes` | 托盘运行列表按会话去重、只列忙碌的 |
| `TestRunningHookSkipsSecondWindows` | 第二窗口不发布运行列表 |
| `TestTaskFinishDotsTheChat` | 后台会话完成留下未读点，当前会话不留 |
| `TestUnreadClearsWhenOpened` | 打开/清空未读，且未读点能写入并从 prefs 恢复 |
| `TestAnswerWhileHiddenDots` | 隐藏时收到回答算新消息，同一帧不重复计数 |
| `TestCmdSlashListsTheKeys` | `⌘/` 打开快捷键面板并列出 ⌘K/⌘⇧N/⌘⇧Space |
| `TestStartupReopensTheLastChat` | 启动回到上次会话；已归档/不存在的会话不恢复（真机：重启后 `logs/agent.log` 仍 attach `conversationId=37035`） |
| `TestTrayApprovalsNameTheirChat` | 托盘审批行携带所属会话；未列出的 surface 返回 0 |
| `TestCmdArrowsPageTheTranscript` | ⌘↑ 翻离最新一轮、⌘↓ 翻回来（真机跟随末尾时 Y=MaxFloat32 的处理） |
| `TestModelPaletteAnimatesIn` | 模型面板 280ms 滑入（同一行文字在动画中途位置更低） |
| `TestSidePanelTabRises` | 右栏切标签时内容上浮进入 |
| `TestApprovalBarSlidesIn` | 审批条滑入而非两帧之间出现 |
| `TestStreamingCaretBlinks` | 流式回答末尾的光标明灭（只统计光标窄带的主色像素） |
| `TestQuickKeysSwitchTheFirstChats` | ⌘2 切到 rail 第二个会话；不足九个时 ⌘9 无动作 |
| `TestPreviewZoomStepsAndClamps` | 图片查看器 +/1:1 与 0.5×–4× 夹取 |
| `TestStepsHangOffARoundedRail` | 步骤时间轴是 2px 圆角轨（1px 边框会红） |
| `TestAttentionSoundsOncePerWait` | 审批提醒只响一次，结束时通知取消 |
真窗口这一轮拍不到（显示器休眠，`CGWindowList` 里窗口 `onscreen=false`），时间轴与图片缩放的证据改用仓库内的快照：`docs/snapshots/turn-{light,dark}.png`（`TestSnapshotTurn`，`PITTA_SNAPSHOT_DIR=../../docs/snapshots`）。
| `TestSurfacesReachTheOtherWindows` | 一个窗口载入的会话列表同步到另一个窗口，且不覆盖自己 |
| `TestTitleCarriesTheApprovalWait` | 标题的等待秒数与未读计数 |
| `TestPrefsCarryTheWindowFrame` | 窗口位置/尺寸随 `ui.json` 往返 |
| `TestQuitAsksWhileATurnRuns` | 有回合时 ⌘Q 先问，确认后退出一次 |
| `TestQuitLeavesWhenIdle` | 空闲 ⌘Q 直接退出、不弹窗 |
| `TestApprovalsReachTheOtherWindows` | 审批列表同步到另一个窗口 |
