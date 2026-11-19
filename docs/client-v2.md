# 客户端 v2：重写范围与设计

> **2026-11-19 之后**：下面「砍掉」那张表不再是终局。v2 落地后发现砍掉的东西里有每天要用的
> （工具输出的结构化渲染、会话改名/归档、模型选择、编码回路），决定把这些有过的功能用 mygo
> 手写补回来，不再依赖任何 UI 库。待补清单在 [client-go/docs/gaps.md](../client-go/docs/gaps.md)，
> 这张表从此只作为「当初为什么砍」的记录，不是现状。

旧客户端在 tag `client-v1`（45 个文件 / 18.5k 行 app / 28.9k 行全包）。那不是要迁移的资产，是参考。

## 为什么重写

一份客户端只该回答三件事：agent 在干什么、我要不要批、我怎么找回来。v1 一半代码在回答没人问的事：git 面板、日志面板、⌘K 面板、多窗口、菜单栏托盘、毛玻璃、窗口透明度、图片画廊、rail 六个 filter、199 条 i18n。

v2 借 assistant-ui 的两样东西：**组件模型**（thread / message / composer / approval 各管一段，互不渗透）和**状态流**（snapshot-first + 增量事件 → reducer → 投影），不借它的 React 代码。

## 保留（命门，丢了就不是这个产品）

1. **transcript**：流式文本、thinking 折叠、工具步骤折成一行、图片、错误行、花费。
2. **composer**：多行、IME、Enter 发送、粘贴图片、↑ 历史、发送/停止、排队条。
3. **审批**：阻塞式 —— 待审时它取代 composer 的位置；rail 红点；Dock bounce。
4. **任务视图**：working / waiting / sleeping 分组 + 下次触发时刻。这是 v1 的差异化，v2 不做成脚注。
5. **会话列表**：一个过滤框、按最近排序、来源标记（飞书/客户端）、未读点。
6. **暗/亮跟随系统**，中/英两份文案（约 60 键）。

## 砍掉

| 砍 | v1 行数 | 理由 |
| --- | --- | --- |
| git 面板 + diff + 高亮（`side`） | 319 + 1203 | 终端更顺手 |
| 日志面板 | 235 | `tail -f` |
| ⌘K palette + 搜索面板 | 459 | rail 一个过滤框 |
| 多窗口 / switcher / hub | 352 | 单窗口 |
| 菜单栏托盘 | 194 | Dock badge 够 |
| 玻璃 / 透明度 / 窗口宽度适配 | 286 | 系统默认外观 |
| 图片画廊 | 344 → 80 | 粘贴 + 缩略就够 |
| 设置面板 | 153 | 并进菜单（主题跟系统） |
| 深链 `pilot://` / ⌘1–9 | 119 | 没有第二个入口需要 |
| `ops` / `turns` 两个中间层 | 3704 | 需要的部分内联，不留抽象 |

目标：app 包 ≤ 4k 行，全仓 ≤ 7k 行。

## 结构

```
client-go/
  main.go           窗口、菜单、生命周期（~150 行，砍掉托盘/透明/多窗口/深链）
  internal/app/
    app.go          App、快照轮询、增量合并（~500）
    shell.go        rail + main 骨架、顶栏（~400）
    rail.go         会话列表（~350）
    tasks.go        任务列表（~250）
    transcript.go   会话滚动区、分页、跟随底部（~600）
    rows.go         message / thinking / step / error 的行（~450）
    composer.go     输入、队列、审批卡（~450）
  internal/{api,protocol,theme,i18n,bridge}   沿用，i18n 裁到 ~60 键
```

## 状态流（唯一值得抄的地方）

视图不持有数据，只持有投影：

- `snapshot`（`/api/state`）每 5s 一次，只用来**对账**：多出来的会话、变了的 task 状态。它不该让整棵树重排。
- 增量事件（SSE / 已有的事件流）只改动对应的 entry，`store` 记住 `(conversationID, entryID) → version`。
- 每个视图每帧算一次投影：`railRows()`、`threadRows(convID)`。投影是纯函数，测试直接对结构断言，不截图。
- 折叠状态（steps 展开、thinking 展开）挂在 view-model 里，用 `(conversationID, turnID)` 做键 —— 不再放在行结构里跟着数据跑。

## 视觉（从 assistant-ui 借三件事）

1. 用户消息右对齐、淡底、圆角；assistant 全宽无气泡，行距松。工具步骤和 thinking 都是「一行摘要 + 可展开」，摘要 12px muted。
2. 消息 hover 出 action bar：只有图标（复制、重发），没有常驻按钮。
3. composer 是一张圆角卡片浮在底部，聚焦时描边；待审时整张卡换成审批卡（标题 + 允许/拒绝），不弹模态。

不做装饰性动画。动效只有两处：流式文本的插入、任务计时的秒针。

## P0（第一刀，能真用的最小闭环）

- 单窗口：左 rail 260pt + 右主区；`TitleBarHidden`，红绿灯压在 rail 顶部。
- rail：过滤框、`会话 | 任务` 分段、两种列表。
- transcript：只读渲染**真实** bridge 数据（`/api/state` + `/api/conversation?id=N&limit=120`）：用户气泡、assistant 全宽、thinking/步骤折叠、图片缩略。
- composer：能发（`POST /api/submit` 同 v1）、能停、↑ 历史、粘贴图片、排队条。
- 审批：待审时替换 composer，允许/拒绝打到 `POST /api/approvals`，同 v1。
- 验收：`go vet ./...`、`go test ./...`、`go tool mygo build -skip-dmg`、暗/亮两张快照肉眼可读、真发一条消息看到回复。

P0 之后：任务视图细化、未读、错误行、花费、跨会话搜索框过滤。
