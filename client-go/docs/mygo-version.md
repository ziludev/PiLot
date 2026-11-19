# mygo 的版本

当前：`github.com/egoist/mygo v0.10.0`（`go.mod`），构建走 `go tool mygo build`。

## 为什么不是 v0.6.4

`0.5 → 0.6` 有过一次破坏性变化，所以目标里 2026-11-16 曾写「钉死 v0.6.4」。真降了一次，发现它在
**中文输入法组词期间按下 Enter 时会把未上屏的拼音串提交进正文**：`ui/editor.go` 先
`commitCompose()` 把 `"nihao"` 落进缓冲区，再走常规 Enter 分支插入换行，应用层拿不到「是否正在组词」
的信号（`TextKey` 只给最终文本），绕不过去。用中文打到一半按 ⌘Enter 就会发出一串拼音，这是本项目
的硬需求内的阻断项，所以退回最新版。同一个测试（`internal/app/input_test.go` 的
`TestIMECompositionDoesNotSend`）在 0.9.1 之后是绿的：组词期间 Enter 既不提交也不换行。

若将来必须回 0.6.4：组词期间禁止键盘发送（或只留 Send 按钮），并把上面那个测试的期望改成降级版本。

## v0.9.1 → v0.10.0

48 个 commit，没有破坏性 API 变化，`go build ./...`、`go vet ./...`、`go test ./...` 一次通过。

| 变化 | 对我们的意义 |
| --- | --- |
| 共享 atlas 在别的窗口移动后能重新绘制（#1921） | 曾经「带 `Transition` 的列表行画出重复行与白色块」的原因就在这里；v0.10.0 下未复现。现在 rail 行没挂过渡，将来要挂可以试 |
| `ListState.MoveAnimation`（#1904） | 行进出、换位的滑行交给框架。现在没用；要做「归档后行自动收拢」先看它 |
| `Exit` 动效挂在 `ui.List` 的行上仍不生效 | 行在数据消失的那一帧就被丢掉，没有元素挂 Exit。要用收拢动画得 app 侧记账 + `AnimateTo` |
| 布局少读元素属性（#1928） | 2000 行大文件首帧开销仍在阈值内 |

## 可用的东西里容易被忘的

- `ui.Popover` 把 Enter 当成自己的关闭键。要 Enter 选中的面板（模型、命令面板）要么内联，要么自己拦键。
- 没有 HTML 渲染、没有 `backdrop-filter`；毛玻璃用 `plugins/glass`，纯色+边高光用 theme token。
- 剪贴板只给文本（`windowHost.ReadText`），图片只能走路径选择。
