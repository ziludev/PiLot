# 第一版客户端有、这个客户端还没有的

`94c8c28` 把桌面客户端重写成三块（rail / transcript / composer），第一版里的面板和快捷键没有跟着
搬回来。**服务端接口全都还在**，所以下面每一条都是纯客户端的工作，不是需求。

这份文件是活的待办：做完一项就删掉那一行，别让它变成第二份 `parity.md`（那份 313 行的对照表已经被
删了，因为它逐行写着 `internal/side`、`internal/ops` 这些现在不存在的包）。

## 1. 一览

| 缺的 | 第一版怎么做的 | 接口 | mygo 原语 |
| --- | --- | --- | --- |
| 会话菜单里的「清理 worktree」 | `ChatMenu.tsx`（未合并的分支拒绝并提示） | `POST /api/worktree/remove` | 现在重命名 / 归档直接进 `button.Menu`，加一项即可 |
| 右栏剩下的：提交 / 推送 / 合并的写操作、项目栏 | `SidePanel.tsx` + `side/*.go` | `/api/file`、`/api/commit`、`/api/git/push`、`/api/merge`、`/api/git/base`、`/api/projects` | 改动 / 提交两栏已经回来了（`internal/app/changes.go`，只读）；写操作得走批准路径 |
| 权限页 | `AccessPanel.tsx` | `GET /api/access`、`/api/audit` | `ui.Segmented` 过滤 + 表格 |
| 拖拽排序 / 置顶 | `Rail.tsx` + `prefs.Order/Pinned` | 无（本地） | `ui.List` 的 `Reorder` |
| 每步耗时竖轨 | `TurnRail.tsx` / `TurnDurations.tsx` | 无 | 现在折叠成一行 `⋯ 4 步 · 12s` |

## 2. 搬回来时别踩的坑

- **`ui.Popover` 会把 Enter 当成自己的关闭键**。模型面板要 Enter 选中，所以它是 composer 上方的内联
  面板（`internal/app/model.go`）而不是浮层；别的面板要 Enter 也别用 Popover，或者在 Popover 里自己拦键。
- **`Exit` 动效挂在 `ui.List` 的行上不生效**（v0.10.0 仍是）：行在数据消失的那一帧就被丢掉，没有元素
  可以挂。要收拢动画就自己记账 + `AnimateTo`，或者等 `ListState.MoveAnimation` 覆盖这个场景。
- **transcript 的步骤折叠是手写的**（`rows.go` 里数 agent / tool 行数生成一行摘要）。往里面加结构化
  输出时不要改折叠结构，只在展开后的内容里分派。
- **rail 的分区是 `rows.go` 的 `chatSections` + `prefs.Sections`**。新的分区（比如「已归档」）照抄这套，
  默认折叠、点分区头开合、选过的状态进 `~/.pitta/window.json`。
- **任务行能点开**（`internal/app/taskdetail.go`）：列表回答「有没有东西坏了」，不回答「为什么」。
  对话框给类型/会话/开始时间/输入/结果/子 agent，运行中的才有「停止任务」——点完不假装停了，
  而是再问一次 bridge（abort 是异步的）。
- **用量日历**（`internal/app/usage.go`）：开销是唯一不能靠看屏幕核对的东西，所以给个形状——
  17 周 × 7 的方格，颜色由浅到深按当天花费，底下是总额/总 tokens/最贵一天。只在任务 tab 出现，
  因为它数的是那个 tab 列的工作。色阶用混色而不是 alpha：半透明叠在弹层上会糊。
- **消息操作条照 assistant-ui 的样子**（`internal/app/message.go`）：每个气泡下面都有，平时 `Opacity(0)`、
  `IgnoreInput`，指针进来才亮——所以它既不让版面跳动，也不会误点。「编辑重发」只把文字放回输入框，
  不改历史：transcript 是 agent 的记录，改过的问句会让答案对着没人说过的话。
- **回到最新的浮标**（`transcript.go` 的 `jumpToLatest`）：对话在跟随最新一轮，读者只有滚上去才能看旧答案，
  那就得给一条回来的路。查找条开着时不显示，两者抢同一个「看哪儿」。
- **文件和它的改动并排**（`changes.go` 的 `fileTextView`）：改动回答「动了什么」，全文回答「现在写着什么」，
  读 diff 去回答后者是猜。切换按钮只在 worktree 的文件上有——一个提交的 diff 背后没有「全文」这回事。
  行号放在同一串里靠 figure space 右对齐：行首普通空格会被排版吃掉，行号就会贴上文件自己的缩进。
- **右栏是抽屉，两个抽屉轮着来**（`internal/app/changes.go`、`memory.go`）：同一个右边缘，打开一个
  就关另一个——两个 460 点的栏会把 transcript 挤成一条。窄窗口时抽屉 `Shrink(1)` 到 300，rail 的 260
  之外还得给 transcript 留活路。
- **diff 一行一个 `ui.List` 行**：一个重新生成的 lock 文件是几千行，不虚拟化就会在渲染上卡住。
- **设置收在一个面板里**（`internal/app/settings.go`）：rail 页脚现在只有一个齿轮。齿轮打开对话框时
  每帧重读 prefs，面板本身不存状态——多一份副本就会漂移。
- **查找条是手写的**（`internal/app/find.go`）：`ui.FindBar` 用窗口语言说 "1 of 3"，而这个 app 的语言
  是自己存的，中文窗口里冒出英文比多写四个小按钮更糟。命中按「行」算，滚到当前命中并给它上底色。
- **中文界面默认**，文案都走 `internal/i18n/dict.go`，zh 与 en 两套一起加；`i18n` 测试钉着键数上限
  （现在 122 = 上限），加键就抬上限并在 `maxKeys` 上写清是哪一块加的。
- **记忆抽屉是右栏，不是 rail 的分区**（`internal/app/memory.go`）：记忆是句子，260 点的 rail 会把它们
  折成一团。它跟着打开的会话走（`selectConversation` 里重读 chat 那一档），整理是一轮模型调用，所以
  按钮上写着在跑、失败也照实说。
