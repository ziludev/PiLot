# Pitta（Go + mygo）

pi-durable-agent 的桌面客户端，用 Go 和 [mygo](https://github.com/egoist/mygo) 的 `ui` 包原生渲染（GPU，Metal），不走 webview。渲染基于 `github.com/egoist/mygo` **v0.9.1**（`go.mod:5`；为什么不是 0.6.4 见 `docs/mygo-version.md`）。
功能对齐基线是同一后端的 React 客户端（`pi-durable-agent/client/src`），逐条对照见 [docs/parity.md](docs/parity.md)。

## 构建与运行

```sh
go tool mygo build -skip-dmg     # → build/darwin-arm64/Pitta.app
open build/darwin-arm64/Pitta.app
```

装到用户目录（与 `/Applications` 里同名的 mygo webview 版 Pitta 分开，别覆盖它）：

```sh
pkill -f "$HOME/Applications/Pitta.app/Contents/MacOS/Pitta"
rsync -a --delete build/darwin-arm64/Pitta.app "$HOME/Applications/"
open "$HOME/Applications/Pitta.app"
```

`go tool mygo` 由 go.mod 的 `tool` 指令提供（`go get -tool github.com/egoist/mygo/cmd/mygo`）。

## 连接后端

客户端连本地 agent 的 HTTP/WS 桥（默认 `http://127.0.0.1:8787`），配置来源按优先级：

1. 环境变量 `AGENT_HTTP_PORT` / `AGENT_HTTP_HOST` / `AGENT_HTTP_TOKEN`；
2. agent 检出目录下的 `.env`（与后端自己读的是同一个文件）。

检出目录由 `PI_DURABLE_HOME` 指定，默认 `$HOME/learn/pi-durable-agent`。底部栏显示当前值，Restart / Backup 会调用该目录的 `scripts/agentctl`。

界面偏好（语言、深浅色、置顶、会话宽度、模型收藏）存在 `~/.pitta/ui.json`。

## 图标

`resources/icon.png`（1024×1024）由 `resources/pitta-logo-dark.svg` 渲染而来——就是 pitta-gpui 用的那套 logo（深色底 + 白色鸟形）。
重跑 `./scripts/render-icon.sh` 刷新，`go tool mygo build` 会把它打成 `AppIcon.icns`。

## 目录

```
main.go                 窗口与启动
mygo.config.ts          打包配置（标识符、版本、输出目录）
internal/bridge         后端地址、token、agentctl
internal/api            HTTP 客户端与 WS 订阅（自愈重连）
internal/protocol       wire 类型（与后端 JSON 一一对应）
internal/turns          回合切分、统计、live 阶段
internal/diff           patch 解析与渲染
internal/theme          shadcn zinc 配色、Geist 字体、每帧应用 ui.Theme
internal/i18n           中英文案（183 键 + 3 个 attach*）
internal/icons          lucide 图标
internal/notify         回答完成通知（osascript）
internal/ops            运维面板：memory / access / usage / task
internal/side           侧栏面板：changes / commits / commit / file / merge
internal/app            App：状态、rail、transcript、composer、面板接线
```

## 开发

```sh
go build ./... && go vet ./... && go test ./internal/... -count=1
PITTA_LIVE=1 go test ./internal/app -run TestLiveBridgeWindow   # 连真实后端出快照 /tmp/pitta-live.png
PITTA_LIVE=1 go test ./internal/app -run TestLiveSidePanels     # 四个侧栏面板的真实数据快照
PITTA_E2E=1  go test ./internal/app -run TestLiveTurnAgainstTheBridge   # 新建会话、发一条、等回答（花一次模型调用）
PITTA_LIVE=1 go test ./internal/app -run TestLiveWritePaths     # 写类操作（切模型、重命名、push 试运行）后还原
```

UI 测试用 `ui.NewTester`，纯 CPU 出帧，无需窗口；像素断言在 `internal/theme/theme_test.go`。
