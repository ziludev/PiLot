# 复核命令与结果

本文件是给复核者（人或审计 agent）用的：每条命令都能在这个仓库里直接重跑，输出贴的是本次运行的原始结果。时间 2026-11-16 19:50 CST，commit 见 `git log -1`。

## 1. 编译、静态检查、单测（不需要后端）

```
$ go build ./...
ok
$ go vet ./...
ok
$ go test ./internal/... -count=1
ok  	pitta/internal/api	1.626s
ok  	pitta/internal/app	1.104s
?   	pitta/internal/bridge	[no test files]
ok  	pitta/internal/diff	0.445s
ok  	pitta/internal/i18n	0.352s
ok  	pitta/internal/icons	0.369s
?   	pitta/internal/notify	[no test files]
ok  	pitta/internal/ops	1.022s
?   	pitta/internal/protocol	[no test files]
ok  	pitta/internal/side	0.926s
ok  	pitta/internal/theme	0.872s
ok  	pitta/internal/turns	0.784s
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
- 快照写到 `/tmp/pitta-app-{light,dark,side-*,modelpicker,approval}.png`。

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

## 5. 字面量 key 守卫

```
$ go test ./internal/i18n -run TestEveryLiteralKeyExists -v
--- PASS: TestEveryLiteralKeyExists (0.01s)      # 遍历仓库源码，108 个字面量 key 必须在 en/zh 两个字典里
```

## 6. 真窗口输入

见 `docs/manual-checks/results.txt` 与同目录 10 张截图（点击切换、打字、Shift+Enter、选区可见、选中替换、粘贴、IME 预编辑+候选窗、组词期间 Enter、候选提交、清空）。

## 7. 已知差异

`docs/parity.md` §4 逐条列出（⌘F 定位方式、附件靠路径、Toast 无动效、access 表分组、memory 逐条勾选、飞书健康横幅与 `/api/search` 未接等）；mygo 版本选择见 `docs/mygo-version.md`。
