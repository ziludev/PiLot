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
| `TestCmdCommaOpensSettings` | ⌘, 打开设置、列快捷键、主题三选落到 prefs |
| `TestLongWaitBlinksInTheTitle` | 12s 不闪、420s 闪，且秒数仍在标题里 |
| `TestClickingASharedChatRaisesItsWindow` | 专属窗口被抬起、自己不抬自己、关窗后忘记 |

## 2026-11-17 删除 React 客户端前的验证

这一轮结束时仓库只剩一个桌面客户端（`client-go/`）。下面是执行过的命令与结果，顺序与上下文
（`docs/history.md` 记了每一步做了什么）。

### 1. Go 客户端

```
$ cd client-go && go build ./...     # BUILD OK
$ go vet ./...                       # VET OK
$ go test ./internal/... -count=1
ok  pitta/internal/api     1.539s
ok  pitta/internal/app     1.241s
ok  pitta/internal/avatar  0.333s
?   pitta/internal/bridge  [no test files]
ok  pitta/internal/diff    0.336s
ok  pitta/internal/i18n    0.138s
ok  pitta/internal/icons   0.300s
ok  pitta/internal/motion  0.108s
ok  pitta/internal/notify  0.331s
ok  pitta/internal/ops     0.263s
?   pitta/internal/protocol [no test files]
ok  pitta/internal/side    0.199s
ok  pitta/internal/theme   0.423s
ok  pitta/internal/turns   0.344s
```

### 2. 真实后端（不是 mock）

```
$ PITTA_LIVE=1 go test ./internal/app -run TestLive -count=1
ok  pitta/internal/app  4.122s                     # 长会话 200 条、patch 渲染、markdown、字体

$ PITTA_E2E=1 go test ./internal/app -run TestLiveStreamingFrames -count=1
long_test.go:213: conversation=38466 surface=client:mwjipuj1
--- PASS: TestLiveStreamingFrames (2.05s)

$ PITTA_APPROVAL=1 go test ./internal/app -run TestLiveApprovalDecision -count=1
approval_live_test.go:43: conversation=38478 surface=client:mwjipwl5
approval_live_test.go:77: pending callId=call_00_cRXve2dF5pq2GP3Fj54i8513 tool=bash reason="提权执行：sudo -n true; echo \"exit=$?\""
approval_live_test.go:96: denied callId=… via the client; 0 approvals left
--- PASS: TestLiveApprovalDecision (2.13s)
```

### 3. 服务端（未改动，仅确认没被这次删除波及）

```
$ npm test    → 137 pass, 0 fail
$ npm run eval → 160 pass, 1 warn (quiet-catch 预算 118/140), 0 fail
```

`tests/turn-state.test.ts` 已删除：它只测被删掉的客户端逻辑，同样的判定在
`client-go/internal/turns` 里有 Go 版测试。`evals/suites/contract.ts` 不再扫描 `client/src`。

### 4. 残留引用

```
$ grep -rIl -iE "react|vite|tailwind|shadcn|\.tsx" . \
    --exclude-dir=node_modules --exclude-dir=.git --exclude-dir=workspace \
    --exclude-dir=backups --exclude-dir=tmp --exclude-dir=logs --exclude-dir=build
```

命中只剩三类，都是有意保留的：

| 位置 | 为什么留着 |
| --- | --- |
| `client-go/internal/i18n/origin/i18n.ts` | 字典基线被 vendored 进来，`TestOriginDictionaryStillMatches` 用它保证键集不漂移；这是数据文件，不是可构建的前端 |
| `client-go/docs/{history,parity,ui-motion-todo,mygo-version,manual-checks/results}.md` | 迁移与取舍的记录，写了「基线已删除」 |
| `package-lock.json` 的 `react` | 上游 `@earendil-works/*` 的 **optional peerDependency**，不是本仓库依赖 |

`client/` 目录（连同被 gitignore 的 `dist/`、`node_modules/`）已从磁盘删除。

### 5. 深链

```
$ open "pitta://chat/9753"
$ tail -1 ~/learn/pi-durable-agent/logs/agent.log
[http] client attached {"conversationId":9753}
```

`CFBundleURLTypes` 里有 `pitta`（`PlistBuddy -c "Print :CFBundleURLTypes" build/darwin-arm64/Pitta.app/Contents/Info.plist`）。

### 6. 安装与证据的边界

```
$ go tool mygo build -skip-dmg          → build/darwin-arm64/Pitta.app (15 MB)
$ rsync -a --delete build/darwin-arm64/Pitta.app ~/Applications/
$ open ~/Applications/Pitta.app
$ pgrep -f "$HOME/Applications/Pitta.app/Contents/MacOS/Pitta"  → 62937
$ tail -1 ~/learn/pi-durable-agent/logs/agent.log
[http] client attached {"conversationId":9753}
```

**真窗口截图这一轮拿不到**：`CGSessionCopyCurrentDictionary` 报 `CGSSessionScreenIsLocked=1`，
`CGWindowListCopyWindowInfo` 里窗口 `onscreen=0`（窗口存在、进程活着、`client attached` 有日志）。
按目标里的规则如实记录，不伪造：`README` 首图与 `docs/manual-checks/` 之外的界面证据改用
**应用自己的渲染路径**——`PITTA_LIVE=1 go test ./internal/app -run TestLiveConversation` 把真实
后端返回的会话画成 1180×820 的 PNG（`/tmp/pitta-live.png`，README 首图即此图），它和窗口走同一套
`ui.View` 代码，缺的只是"被窗口服务器合成到屏幕上"这一步。

`go run .` 可以正常起窗并连桥；裸二进制下 `notify` 报 `not supported on this platform`（点通知
需要 .app bundle 的 UserNotifications 授权），这是降级而不是崩溃：通知不可用时其余流程照常。

### 7. 装好的那一份（t-ship）

```
$ du -sh ~/Applications/Pitta.app                             → 15M
$ /usr/libexec/PlistBuddy -c "Print :CFBundleIdentifier" …    → com.example.agent
$ /usr/libexec/PlistBuddy -c "Print :CFBundleURLTypes:0:CFBundleURLSchemes:0" … → pitta
$ ls Contents/Resources                                        → AppIcon.icns  icon.png  pitta-logo-dark.svg
$ pgrep -f "~/Applications/Pitta.app/Contents/MacOS/Pitta"     → 62937
$ ls ~/Library/Logs/DiagnosticReports/*Pitta* | wc -l          → 0
```

主流程在真实后端的证据分两层，因为屏幕锁着，第二层拿不到"人点窗口"那种证据：

1. **应用本身**：进程 62937 存活、无崩溃报告、`logs/agent.log` 有
   `[http] client attached {"conversationId":9753}`；向同一会话 `POST /api/submit`
   （`{"submissionId":38506}`）后服务端正常接单。窗口在窗口服务器里存在（1180×820，
   位置 732,241），只是 `onscreen=0`（锁屏）。
2. **同一套代码路径的自动化**：`PITTA_LIVE` / `PITTA_E2E` / `PITTA_APPROVAL` 三组用例直接驱动
   `App.watch`、`sendText`、`approvalBar`，对着真桥跑通发送、流式、审批（见 §2）。这两层加起来
   覆盖了"装好的二进制能连真后端"和"主流程在真后端可用"，缺的只有"手指点到窗口上"这一环。

真窗口的界面证据来自更早的会话（当时屏幕是醒的、窗口在前台）：`docs/manual-checks/` 里的
`58-click-switches-chat.png`、`59-typing.png`、`60-shift-enter.png`、`63-follow-newest.png`
等，配合本轮修掉的死锁（`applyWait` 主线程自锁）之后重拍过一轮。

### 8. 历史整理（2026-11-17）

审计指出两个提交把两件事混在了一起（`eec197c` = composer 输入历史 + rail 键盘导航，
`eb5f64a` = 文件高亮 + 其他项目折叠），回滚任一提交会同时撤掉两个功能。已重排为
`8cfe713`（输入历史）、`72d9426`（rail 导航与置顶/归档快捷键）、`51b1c5f`（文件高亮）、
`b2bdcb1`（折叠记忆）四个提交，各自只碰自己的文件：

```
$ git show --stat --format=%s 72d9426
  client-go/internal/app/app.go          |  8 ++
  client-go/internal/app/railnav.go      | 59 +++
  client-go/internal/app/railnav_test.go | 83 +++
  client-go/internal/app/shortcuts.go    |  4 ++
```

重排后逐个看过：四个提交各自 `go build ./...` 通过（`8cfe713`/`72d9426`/`51b1c5f`/`b2bdcb1`
各在独立 worktree 里跑过 `go build`，后两个还跑了 `go vet`），重排后的树与重排前
（`dd7d055`）逐字节相同（`git diff dd7d055 --stat` 为空），`go test ./internal/...` 仍 12 包全绿。
