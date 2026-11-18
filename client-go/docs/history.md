# 从 React 客户端到 Go + mygo：迁移与删除记录

这个仓库原来有两个桌面客户端：`client/`（MyGo 窗口 + React 19 + shadcn/ui Nova，逻辑全在 TypeScript）
和 `client-go/`（同一个后端，窗口由 mygo/ui 在 Metal 上直绘）。两套界面意味着每一处改动都要写两遍，
而 native 那套在键盘、通知、深链、多窗口、代码高亮这些地方能做到 webview 做不到的事。

**这一轮把 `client/` 删除了，仓库只剩一个桌面客户端。** 下面是删除前逐项核对过的覆盖表：每一行的
React 行为在 Go 侧都有落点，括号里是测试或证据的位置。

## 1. 主流程

| React 行为 | Go 侧 | 证据 |
| --- | --- | --- |
| 发消息、⌘↵ 发送、Enter 与 ⇧↵ 换行、IME 组词中不发送 | `internal/app/composer.go` + `OnTextKey` | `composer_test.go`、`input_test.go` 的 `TestIMECompositionDoesNotSend` |
| WebSocket 流式帧、断线重连、live 文档 | `internal/api/watch.go`、`internal/app/app.go` 的 `watch` | `api/client_test.go`、`TestLiveStreamingFrames`（`PITTA_E2E=1`，真后端） |
| 审批条：allow / allow session / deny，等待秒数 | `internal/app/composer.go` 的 `approvalBar` | `TestLiveApprovalDecision`（`PITTA_APPROVAL=1`，真后端拒绝后 pending 归零） |
| 改动、逐行批注、发送 review、提交、合并、推送、基线分支 | `internal/app/gitpanel.go` + `internal/side` | `sidepanel_test.go`、`m2_test.go` |
| diff 渲染（文件卡片、行号、折叠） | `internal/diff` | `diff_test.go`、`TestSnapshotStepExpanded` |
| 模型选择、思考档、收藏、设默认 | `internal/app/modelpicker.go` | `modelpicker_test.go` |
| 图片附件 | `internal/app/images.go`（路径对话框 + 剪贴板 `⌘V` + 拖放） | `images_test.go`、`native_test.go` |
| 多窗口 | `internal/app/windows.go`（一会话一窗口，审批与角标留在首窗） | `secondary_test.go` |

## 2. 这轮新增、React 客户端没有的

| 能力 | 位置 |
| --- | --- |
| 命令面板（⌘K：`>` 走命令，否则搜会话） | `internal/app/palette.go` |
| rail 键盘行走（⌥↑/⌥↓）、⌘⇧P 置顶、⌘⇧⌫ 归档 | `internal/app/railnav.go` |
| composer 输入历史（↑/↓ 召回） | `internal/app/history.go` |
| 文件预览按类型上色（Go/TS/Python/Rust…） | `internal/side/highlight.go` |
| 通知点击落到对应会话 | `internal/notify` + `main.go` 的 `OnNotificationClick` |
| 深链 `pilot://chat/123[?window=new]` | `internal/app/deeplink.go` + `mygo.config.ts` 的 `urlSchemes` |
| 自绘标题栏、Dock 角标、托盘（含待批条目）、菜单栏 | `main.go`、`internal/app/menu.go` |
| Liquid Glass 的浮层（菜单、错误胶囊、跳转条、toast） | `internal/app/glass.go` |
| 代码块语法高亮（React 客户端只有等宽 `<pre>`） | mygo `ui/markdown` |

## 3. 仍然存在的差异

差异逐条记在 [parity.md](parity.md) §4（例如 access 表头用等级过滤代替分组、memory 搜索在面板底部、
BotAvatar 是静态 SVG 而非逐帧动画、Toast 无滑入）。这些都不在主流程上，删 React 客户端后也不会有
「换回另一个客户端」这一选项，所以它们是这个客户端的现状，而不是待办。

## 4. 这次删除做了什么

- 删除 `client/`（Go 窗口壳 + `src/` 下的 React 应用 + `package.json`），删除只测它的
  `tests/turn-state.test.ts`；`evals/suites/contract.ts` 不再扫描该目录；根 `package.json`
  去掉 `mygo-cli`（那是前端构建工具），`package-lock.json` 同步。
- i18n 的对照基线从 `client/src/i18n.ts` 搬到 `client-go/internal/i18n/origin/i18n.ts`，
  `TestOriginDictionaryStillMatches` 继续保证「字典与基线同键」。
- README（中英）与 `assets/legacy-avatar/README.md` 改为只描述 Go 客户端；`parity.md` 与
  `ui-motion-todo.md` 保留，但标注基线已删除。
- 覆盖核对：本文 §1 的每一行在删除前都跑过一次（`go test ./internal/...` 全绿、
  `PITTA_LIVE/PITTA_E2E/PITTA_APPROVAL` 在真实后端通过）。
