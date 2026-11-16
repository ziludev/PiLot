# mygo 版本说明：为什么钉在 v0.9.1，而不是 v0.6.4

> 结论已由用户确认（2026-11-16）：**保持最新版 v0.9.1**，不降级。

目标里写的是「钉死 mygo v0.6.4（0.5→0.6 ui 包有破坏性变化，升级需单独说明）」。实际仓库
`go.mod:5` 是 `github.com/egoist/mygo v0.9.1`。本文就是那份额外的说明：先按 0.6.4 真的降过一次，
记录降级代价与放弃原因，再给出 0.9.1 的实测证据。

## 1. 降级实验（2026-11-16，可复现）

```bash
cd /Users/alexwang/work/pitta-mygo
go get github.com/egoist/mygo@v0.6.4
go build ./...
```

编译期只有 3 个 api 缺失，逐个都能量身替代：

| 0.9.1 的 API | 用在 | v0.6.4 的替代 |
| --- | --- | --- |
| `Element.TruncateMiddle()` | `internal/diff/render.go:124`、`internal/side/{changes,commits,file}.go`、`internal/app/rail.go:334` | 去掉该方法，自己在送进 `ui.Text` 前做中段省略（`elidePath`） |
| `Element.ScrollbarAxes(horizontal, vertical bool)` | `internal/diff/render.go:156` | 去掉，退回默认滚动条（0.6.4 只有 `ScrollbarInsets`） |
| `Element.FillEqually()`（`ui.Segmented`） | `internal/ops/access.go:44`、`internal/app/rail.go:101` | 去掉，分段按钮按文字宽度排（与 React 等宽分段有视觉差） |
| `Element.RowClicked() bool` | `internal/side/changes.go:127,160`、`commits.go:122`、`file.go:84` | 改成在行元素上挂 `OnClick`（0.6.4 没有列表行点击回调） |

替代后 `go build ./...`、`go vet ./...` 通过，只剩一个测试红：
`internal/app/input_test.go` 的 `TestIMECompositionDoesNotSend` 报
`draft after commit = "nihao\n你好"`（期望 `"你好"`）。

## 2. 放弃 0.6.4 的原因：组词期间的 Enter

同一个测试在 0.6.4 上的行为拆解：

1. `tt.Compose("nihao", 5)` 建立预编辑，此时 draft 仍是空（这一步 0.6.4 也是对的）；
2. 预编辑期间按 Enter：0.6.4 的 `editor.commitCompose()`（`ui/editor.go:182`）先把 `"nihao"`
   落进缓冲区，再走常规 Enter 分支插入换行 → draft 变成 `"nihao\n"`；
3. 继续 `tt.Type("你好")` 才得到 `"nihao\n你好"`。

换成真机场景就是：用中文输入法打到一半（拼音还在候选框里）按 ⌘Enter 发送，客户端会把未上屏的
拼音串当成正文发出去。这正好踩在目标要求的「中文 IME 候选词」上——0.6.4 在编辑器层就替输入法
做了决定，应用层拿不到「是否正在组词」的信号（`TextKey` 只给最终文本），没法用 app 代码绕过。

0.9.1 在这个路径上是「组词期间 Enter 既不提交也不换行」，预编辑只由输入法上屏（`tt.Type` 提交），
所以同一个测试直接通过。

## 3. 0.9.1 的验证证据

```bash
cd /Users/alexwang/work/pitta-mygo
go build ./... && go vet ./... && go test ./internal/... -count=1
```

`api / app / diff / i18n / icons / ops / side / theme / turns` 全部 ok，其中直接覆盖差异点的是：

- `internal/app/input_test.go`：`TestPasteInsertsClipboardText`、`TestSelectAllAndBackspaceClears`、
  `TestIMECompositionDoesNotSend`（组词不发不换行、`tt.Type` 上屏、⌘Enter 发出中文）；
- `internal/app/snapshot_test.go`：浅/深色关键像素断言（主题 token 真的落在画面上）；
- `internal/app/long_test.go`、`e2e_test.go`、`writeops_live_test.go`：真实后端 200 条会话、
  流式、markdown、patch、切模型/重命名/push dry-run。

## 4. 结论与复评条件

- 交付版本：`github.com/egoist/mygo v0.9.1`（`go.mod:5`），构建用 `go tool mygo build`。
- 0.9.1 是升上来的，不是顺手拿的最新版：0.6.4 在 IME 上会漏拼音，属于本项目硬需求内的阻断项。
- 若将来必须回到 0.6.4（例如上游把 0.9 的 API 收回），需要同时做三件事：按上表补回四个替代，
  在 `internal/app` 层禁止组词期间用键盘发送（或改成只能点 Send 按钮），并把
  `TestIMECompositionDoesNotSend` 的期望改成「组词期间 Enter 不发送」的降级断言。
