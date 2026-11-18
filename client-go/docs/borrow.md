# 别人的客户端里值得抄的东西

对照对象：Raycast、Linear、Cursor、Warp、Claude Desktop、Zed、Things、Slack、VS Code、GitHub Desktop。
判断标准只有一条：**日常用会不会撞到**。撞不到的花活不进这一轮（见 §3）。

可行性一栏按 mygo v0.9.1 的实际能力写，不按"理论上能做"写：`ui.List` 的行为、`OnWindowShortcut` 的修饰键匹配、没有通知以外的系统音频、没有 HTML 渲染，这些都在前面几十轮里踩过。

## 1. 这一轮要做（6 项）

| # | 借鉴 | 具体交互 | mygo 怎么落 | 为什么值得 |
| --- | --- | --- | --- | --- |
| 1 | Raycast 命令面板 | 一个面板同时给**动作**和**会话**：输入 `>theme` 执行命令，直接输入就是搜会话；↑↓ 选，Enter 执行 | 复用已内联的 `⌘K` 切换器，加一个命令前缀判定；命令表复用 `shortcuts()` 之外的动作表 | 现在"切会话"和"改主题/开右栏/导出"是两套入口，用起来要记两处 |
| 2 | VS Code / Zed 文件预览 | 右栏 FileView 给代码上色（现在我们只有等宽纯文本） | `internal/markdown` 的代码块高亮走的是 mygo 的 `editor` 高亮；文件预览可按扩展名套同一套 tokenizer | 看改动时纯文本要肉眼找结构，是每天都在付的成本 |
| 3 | Linear 键盘导航 | rail 里 `j`/`k` 移动、`Enter` 打开、`p` 置顶、`⌫` 归档（输入框聚焦时不触发） | `c.OnWindowShortcut` 只收带修饰键的键；字母键要在 rail 自身 `OnKeyDown` 上拦，输入框聚焦时让路 | 键盘流比鼠标点 rail 快，且 Linear/Things 都是这么做的 |
| 4 | Cursor / Slack 输入历史 | composer 里 `↑` 召回上一条自己发的话，`↓` 回到底部草稿 | `TextArea` 的 `OnTextKey` 已能拦 Enter/方向键；草稿进 `prefs` 之外的内存栈 | 改一句话重发是最常见的动作，现在要手打或复制 |
| 5 | Claude Desktop 通知跳转 | 点 macOS 通知直接落到那轮回答所在的会话 | `mygo.NewNotification` + `Notification.OnClick` + `mygo.App.OnNotificationClick`（v0.9.1 都有） | 现在通知只是提示，点开会把 app 拉到前台但不换会话 |
| 6 | GitHub Desktop / Raycast 深链 | `pilot://chat/123` 打开指定会话（含 `?window=new`） | `mygo.App.OnOpenURL`（`Protocol` 模块）+ `open` 系统注册 | 从飞书/终端/脚本直接跳到对应会话，是替换 Web 版后唯一还缺的入口 |

## 2. 往后排（能做，但这一轮不做）

| 借鉴 | 交互 | 为什么不在这轮 |
| --- | --- | --- |
| Zed 大纲 | 长回答的目录/锚点，点击跳转 | 收益集中在前 5% 的超长会话；我们已有 ⌘F 与跳转条，重复度偏高 |
| Warp 命令块 | 每个工具调用作为一个可折叠"块"，带独立状态灯 | 现有 step 折叠 + 圆角时间轴已覆盖 80%，再抽象一次收益不明显 |
| Slack 未读分隔线 | "以下为新消息"隔断 | 已有未读圆点与跳转条；分隔线要在流式里稳定插入，容易和跟随逻辑打架 |
| Things 快速录入 | 全局热键弹一个只有输入框的浮窗 | `GlobalShortcut` 可用，但需要一个独立窗口与第二套状态同步，代价大于收益 |
| Linear 命令反馈 | 动作后右下角 toast 带撤销 | 撤销需要每个写操作都有反向接口（归档、置顶、删除 worktree），先有 toast |
| Cursor 行内补全 | composer 里 @文件 引用 | 需要文件索引与模糊匹配；先做 §1.2 的文件预览高亮更实在 |
| VS Code 面包屑 | transcript 顶部显示会话→回合→步骤层级 | 单列对话没有真正的层级，画出来是装饰 |
| Raycast 快捷预览 | 空格预览选中会话的首条回答 | 会引入"预览面板"这一新视图，与右栏职责重叠 |

## 3. 不抄的

- **花哨的窗口动画**（缩放式打开、弧线过渡）：mygo 的 `Transition` 能画，但桌面端窗口由系统合成，绕过它只会显得不像原生。
- **AI 输入框里的"思考中"打字机效果**：我们已有流式光标；把流式再包一层假打字会掩盖真实进度。
- **命令面板里塞脚本市场/插件**：范围外，且 mygo 没有插件沙箱。
- **Radix 那种 data-state 驱动的组件树**：mygo 是即时模式，状态就是值，再抽象一层只会更难读。

## 4. 落地结果（本轮收尾时回填）

6 项全部落地，其中两项按 mygo 的能力做了取舍，理由都写在这里而不是留在提交信息里。

| # | 交付物 | 与计划的差别 | 证据 |
| --- | --- | --- | --- |
| 1 | `internal/app/palette.go`：`⌘K` 里 `>` 走命令（16 条），否则搜会话 | 无 | `TestPaletteRunsCommands`、`TestPaletteStillJumpsToChats` |
| 2 | `internal/side/highlight.go`：按扩展名套 `editor` 的 tokenizer 给文件预览上色，带缓存 | 只上色，不做折叠/大纲 | `internal/side/highlight_test.go`（四种：多色、未知扩展名回退、2000 行 <50ms、缓存命中） |
| 3 | `internal/app/railnav.go`：rail 内 `⌥↑`/`⌥↓` 行走、`⌘⇧P` 置顶、`⌘⇧⌫` 归档 | **`j`/`k` 没有做**：`OnWindowShortcut` 只收带修饰键的组合，纯字母键要在每个焦点元素上让路，而 composer 是常驻焦点——用无修饰字母会让"在输入框里打 j"有歧义 | `TestAltArrowsWalkTheRail`、`TestPinAndArchiveFromTheKeyboard`（置顶、取消置顶与归档各按一次） |
| 4 | `internal/app/history.go`：`↑`/`↓` 召回上一条发送（上限 50，只在内存里，不进 `ui.json`——昨天的草稿不是今天的草稿） | 只在 composer 无双修饰键时拦，输入法组词中不拦 | `TestComposerRecallsSentMessages`、`TestRecallLeavesArrowsAloneInAMultilineDraft` |
| 5 | `internal/notify` + `main.go` 的 `OnNotificationClick`：通知带 `chat-<id>`，点开落到那条会话（`?window=new` 时另开窗） | 无 | `internal/notify/notify_test.go`、`TestNotificationCarriesItsChat`（client-go/internal/app/notify_test.go） |
| 6 | `internal/app/deeplink.go` + `mygo.config.ts` 的 `urlSchemes`：`pilot://chat/123`、`?window=new` | 只认 `chat`，`project/*` 明确拒绝（没有对应视图，认了就是撒谎） | `internal/app/deeplink_test.go`、真机 `open "pilot://chat/9753"` → 日志 `client attached {"conversationId":9753}` |

## 5. 本轮取舍（t-pick）

做 §1 的 6 项，顺序按"日常撞到频率"排：4（输入历史）→ 3（rail 键盘导航）→ 1（命令面板）→ 2（文件高亮）→ 5（通知跳转）→ 6（深链）。

理由：前两项是纯客户端、零协议依赖、失败也不影响主流程；命令面板是既有组件的重组；文件高亮要在 markdown 高亮与 `editor` 之间确认接口；后两项依赖系统集成（通知、URL scheme），拿不到环境证据时按目标里的规则如实记录，不阻塞。
