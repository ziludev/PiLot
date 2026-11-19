# 第一版客户端有、这个客户端还没有的

`94c8c28` 把桌面客户端重写成三块（rail / transcript / composer），第一版里的面板和快捷键没有跟着
搬回来。**服务端接口全都还在**，所以下面每一条都是纯客户端的工作，不是需求。

这份文件是活的待办：做完一项就删掉那一行，别让它变成第二份 `parity.md`（那份 313 行的对照表已经被
删了，因为它逐行写着 `internal/side`、`internal/ops` 这些现在不存在的包）。

## 1. 一览

| 缺的 | 第一版怎么做的 | 接口 | mygo 原语 |
| --- | --- | --- | --- |
| 会话菜单：重命名 / 归档 / 清理 worktree | `ChatMenu.tsx`（未合并的分支拒绝并提示） | `POST /api/surface-meta`、`POST /api/worktree/remove` | `ui.Popover` + `ui.MenuBase` + `ui.Modal` |
| 跨会话搜索 | `SearchBox.tsx` 提交走 `/api/search`，列命中、点进会话 | `GET /api/search` | 列表 + 防抖 |
| 模型选择（含思考档） | `ModelPicker.tsx` | `GET /api/models`、`POST /api/model`、`POST /api/default-model` | 内联面板（见 §2） |
| 改动 / 文件 / 提交 / 合并 / 项目栏 | `SidePanel.tsx` + `side/*.go` | `/api/changes`、`/api/git/diff`、`/api/file`、`/api/git`、`/api/commit`、`/api/git/push`、`/api/merge`、`/api/git/base`、`/api/projects` | `ui.Split` 右栏 + `ui.Tabs` + 折叠卡片 |
| 任务详情对话框 + 用量日历 | `TaskDialog.tsx`、`UsageCalendar.tsx` | `GET /api/tasks`、`POST /api/task/cancel`、`GET /api/usage-calendar` | `ui.Modal`、`ui.Table` |
| 权限页 | `AccessPanel.tsx` | `GET /api/access`、`/api/audit` | `ui.Segmented` 过滤 + 表格 |
| 记忆整理 | `SidePanel.tsx` 的 memory 标签 | `GET /api/memory`、`POST /api/memory/curate`、`POST /api/memory/apply` | `ui.Tabs` + 勾选行 |
| 会话内查找 ⌘F | `ConvSearch.tsx`（上一个/下一个命中） | 无（本地） | `ui.FindBar` |
| 消息级操作：复制 / 编辑重发 / 复制整段 | `Transcript.tsx` 的 hover 动作条 | 无 | 行尾小图标 |
| 拖拽排序 / 置顶 | `Rail.tsx` + `prefs.Order/Pinned` | 无（本地） | `ui.List` 的 `Reorder` |
| 每步耗时竖轨 | `TurnRail.tsx` / `TurnDurations.tsx` | 无 | 现在折叠成一行 `⋯ 4 步 · 12s` |

## 2. 搬回来时别踩的坑

- **`ui.Popover` 会把 Enter 当成自己的关闭键**。模型面板要 Enter 选中，所以第一版把它做成 composer
  上方的内联面板而不是浮层；现在若要做，同样别用 Popover，或者在 Popover 里自己拦键。
- **`Exit` 动效挂在 `ui.List` 的行上不生效**（v0.10.0 仍是）：行在数据消失的那一帧就被丢掉，没有元素
  可以挂。要收拢动画就自己记账 + `AnimateTo`，或者等 `ListState.MoveAnimation` 覆盖这个场景。
- **transcript 的步骤折叠是手写的**（`rows.go` 里数 agent / tool 行数生成一行摘要）。往里面加结构化
  输出时不要改折叠结构，只在展开后的内容里分派。
- **rail 的分区是 `rows.go` 的 `chatSections` + `prefs.Sections`**。新的分区（比如「已归档」）照抄这套，
  默认折叠、点分区头开合、选过的状态进 `~/.pitta/window.json`。
- **中文界面默认**，文案都走 `internal/i18n/dict.go`，zh 与 en 两套一起加；`i18n` 测试钉着键数上限，
  加键前先看还有多少余量。
