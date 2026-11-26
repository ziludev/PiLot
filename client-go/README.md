# PiLot（Go + mygo）

pi-durable-agent 的桌面客户端，用 Go 和 [mygo](https://github.com/egoist/mygo) 的 `ui` 包原生渲染（GPU，Metal），不走 webview。渲染基于 `github.com/egoist/mygo` **v0.10.0**（`go.mod`；为什么不是 0.6.4 见 `docs/mygo-version.md`）。

窗口只有三块：左边 rail、中间 transcript、底下 composer。`94c8c28` 之后是这样——它之前的两版（React webview，以及后来那版带 diff / 提交 / 权限 / 运维面板的 Go 客户端）都已删除，代码和文档都没留；那些面板现在缺什么、接口在哪，看 [docs/gaps.md](docs/gaps.md)。右侧另有三个抽屉：改动（文件表 + patch）、记忆、时间线——同一个右边缘，一次只开一个。

## 视觉

配色、圆角和字号来自 [Beautiful UI](https://www.beautifului.dev)（MIT，React 组件，这里只取令牌）：三层底色（窗口 `Page` → rail `Canvas` → 会话与抽屉是一张 `Surface` 卡片，两者之间不放分隔线）、一张灰阶表（`Ink`/`Ink2`/`Ink3`、`Line`/`LineSoft`/`Field`/`Inset`/`Hover`）、一个品牌蓝和四个语义色。圆角只有四档：chip 6、控件 8、卡片 10、窗口 14；文字 14 / 13 / 12.5 / 11.5 / 11。全部在 `internal/theme` 里，组件只引用令牌，不写字面值——深浅两套主题因此是同一份布局。

## 构建与运行

```sh
go tool mygo build -skip-dmg     # → build/darwin-arm64/PiLot.app
open build/darwin-arm64/PiLot.app
```

装到用户目录（不覆盖 `/Applications` 里的任何构建）：

```sh
pkill -f "$HOME/Applications/PiLot.app/Contents/MacOS/PiLot"
rsync -a --delete build/darwin-arm64/PiLot.app "$HOME/Applications/"
open "$HOME/Applications/PiLot.app"
```

`go tool mygo` 由 go.mod 的 `tool` 指令提供（`go get -tool github.com/egoist/mygo/cmd/mygo`）。

## 窗口里有什么

- **rail**：会话按类型归档——进行中（一天内动过或有排队）、群聊、话题、私聊、其他，进行中默认展开，其余默认折叠，点分组头开合（选过的记在偏好里；正在看的那个会话所在的组永远展开）。每行右侧是时间，还给这个会话花掉的钱；`+` 开新会话，顶上能搜。任务页按进行中 / 等待中 / 休眠分组，显示下次运行时间；行右侧的 ⋯ 是任务的唯一动词——进行中的「停止任务」，休眠或等待中的「关闭任务」（同一个 abort，区别是要不要让它明天再回来）；点行本身开详情：先给它被要求的那句话和 watch 跑的命令，原始 JSON 折在「原始输入」后面；详情底部同样给停止 / 关闭，右下角的「收起」只关弹窗。drag 右边缘可拖宽，宽度记在偏好里。
- **transcript**：回答流式写入；一轮的工具调用折成一行 `⋯ n 步`，点开一步一行带耗时；失败的那轮在错误行上有「重试」；图片附件内联，自己的消息用首字母头像。
- **composer**：这个会话的排队消息列在输入框上方（插话单独标出，超出折叠成「还有 n 条」）；「停止」等于停掉这一轮并清空这个会话的队列。
- **审批**：需要批准时审批条替换掉输入框，允许 / 拒绝，多个排队时显示还剩几条。
- **底部**：语言（默认中文，`PILOT_LANG=en` 才英文；不读系统 locale，Mac 设成英文不代表主人要读英文）与深浅色切换（默认跟系统外观）。语言、主题、rail 宽度存在 `~/.pilot/window.json`——上一版 Rust 客户端的 `ui.json` 不再读。

## 连接后端

客户端连本地 agent 的 HTTP/WS 桥（默认 `http://127.0.0.1:8787`），配置来源按优先级：

1. 环境变量 `AGENT_HTTP_PORT` / `AGENT_HTTP_HOST` / `AGENT_HTTP_TOKEN`；
2. agent 检出目录下的 `.env`（与后端自己读的是同一个文件）。

检出目录由 `PI_DURABLE_HOME` 指定，默认 `$HOME/learn/pi-durable-agent`。底部栏显示当前值，菜单里的「重启 agent」调用该目录的 `scripts/agentctl`。

## 图标

`resources/icon.png`（1024×1024）与 `internal/app/avatar.png`（128）都从同一张母版切出来：`assets/brand/PiLot_fox.jpg`（2048，狐狸头 + `#273244` 底）。前者按 `#182235` 一脉的 squircle 出图，`go tool mygo build` 再把它打成 `AppIcon.icns`；后者是干净的头像裁切，回答旁边和 rail 都用它——Dock 里的图标和对话里的那张脸是同一张画。

重跑 `./scripts/render-icon.sh` 刷新，脚本只依赖 python3 + Pillow（系统自带的那份即可），不再需要 Chrome。

README 顶上那张「狐狸 + PiLot」的横排是 `../scripts/render-brand` 拼的：同一个 tile，加矢量包里的字标。

## 目录

```
main.go                 窗口与启动
mygo.config.ts          打包配置（标识符、版本、输出目录）
internal/bridge         后端地址、token、agentctl
internal/api            HTTP 客户端、WS 订阅（自愈重连）、/api/health
internal/protocol       wire 类型（与后端 JSON 一一对应）
internal/theme          zinc 配色 token、Geist 字体、每帧应用 ui.Theme
internal/i18n           中英文案（两份字典键必须一一对应）
internal/icons          lucide 图标
internal/app            App：状态、rail、transcript、composer、审批、偏好、快照
```

## 开发

```sh
gofmt -l internal/ && go vet ./... && go test ./... -count=1
go tool mygo build -skip-dmg
```

出快照（纯 CPU，`ui.NewTester`，不需要开窗口）：

```sh
go test ./internal/app -run TestSnapshot        # → /tmp/clientv2-light.png、/tmp/clientv2-dark.png
go test ./internal/app -run TestSnapshotRunning # → /tmp/clientv2-running.png
```

像素断言在 `internal/theme/theme_test.go`（底色跟系统外观走、文字对比度、边框取 token）。
