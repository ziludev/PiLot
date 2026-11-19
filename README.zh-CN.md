<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/readme/logo-dark.png">
    <img alt="PiLot" src="assets/readme/logo.png" width="330">
  </picture>
</p>

<p align="center"><a href="README.md">English</a> · <strong>中文</strong></p>

<p align="center"><em>PiLot：一个 Pi，无限可能。</em></p>

<p align="center">
  <img alt="Node 22.6+" src="https://img.shields.io/badge/node-%E2%89%A522.6-informational">
  <img alt="平台：macOS" src="https://img.shields.io/badge/platform-macOS-lightgrey">
  <img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-green">
  <img alt="Durable execution" src="https://img.shields.io/badge/%E6%AF%8F%E8%BD%AE%E4%BB%BB%E5%8A%A1-durable-blueviolet">
</p>

跑在 [Pi Durable](https://earendil.com/posts/pi-durable/) 上的个人助理：飞书里聊、桌面上看、终端里也能用，进程挂了不丢对话。

![桌面客户端：左栏会话与花费，中间折叠的工具时间轴与回答，底部 composer](assets/readme/client.png)

![飞书卡片从「处理中」到「答完了」](assets/readme/card.gif)

- **引擎**：`@earendil-works/pi-durable` 1.5.2。每个模型请求、每次工具调用、每次压缩都是一个 durable task，每步先落 checkpoint；进程被 `kill -9`，重启后从断点继续。
- **模型**：`@earendil-works/pi-ai`。默认走本机 magpie 代理（OpenAI 兼容），可换任意内置 provider；模型/思考级别可**逐会话**切换。
- **存储**：SQLite（`.data/agent.sqlite`），单进程独占，重启原样恢复。
- **三个入口**：
  - 飞书自建应用（长连接由 `feishu-gw/` 这个独立进程持有，不需要公网 IP / 域名 / 证书）
  - `client-go/`：原生 macOS 桌面 app（Go + mygo/ui，Metal 直绘，无 webview）
  - `durable`：CLI

![三个入口，两个进程，一份 SQLite](assets/readme/architecture.png)

![一轮的生死：消息落成一个 durable task，工具调用要么直接出答案、要么等审批，循环到卡片收尾；kill -9 之后从最后一个 checkpoint 续跑](assets/readme/turn.png)

---

## 能力

### 对话

| | |
|---|---|
| 飞书 | 群/单聊各自一个 conversation；群里 @ 才响应；**被 @ 就回在话题里**（群里不刷屏），话题里的追问 → 该话题独立 fork |
| 客户端 | 自建会话（每个自动分配 git worktree）、流式渲染、步骤时间线（见下） |
| 模型 | 飞书里 `/model` 一张卡片换本会话模型（也可 `/model <id>`、`/model thinking high`、`/model default <id>`，只有主人能发）；卡片底部那行写明本轮用的模型、thinking 档、token 与花费 |
| CLI | `durable ask "…"` / `durable chat`（REPL）、`durable sessions` |
| 输入 | 文字、图片（飞书 image/post 消息；客户端粘贴截图）、语音（本地 whisper） |
| 中断 | `/stop`、客户端停止按钮；后台任务不受影响 |
| 插话 | `/steer 内容`：不排队，直接插进正在跑的那一轮，下一个工具调用边界生效（空闲时就当普通消息走）。普通消息仍然排队——一条随手发的「对了…」不该把在跑的事掰弯。底层是 pi-durable 的 `whenBusy: "steer"`，默认一次只插第一条 |
| 早报 | 每天 `AGENT_DIGEST_AT` 发一条：近 12 小时干了几轮、几轮声明达成、没达成的每轮带原因、审批发生了什么、审计尾部。安静的一夜不发消息。`/digest`（或 `/早报`）随时手查 |
| 回顾 | `/ketchup`（或 `/进度`）一句话回到现场：在跑的任务、排在后面的、每件**等你决定**的审批（含已等多久）、最近几条审计。只有主人能用，因为审批行会写你自己的工具调用 |
| 完成提醒 | 答案只是**原地更新卡片**，飞书不会为"更新"推送通知。所以一轮超过 45 秒时，收尾会额外发一条短消息（`✅ 答完了 · 53s` + 结论前 60 字），这样锁屏也能收到；话题里的提问，这条也发在同一个话题里。`FEISHU_DONE_PING_SECONDS` 可调，设 0 关闭 |
| 卡片状态 | 卡片顶部一直写着这轮处在哪：蓝 `⏳ 处理中` → 绿 `✅ 答完了`（续跑过一次写「续跑一次」）/ 绿 `✅ 目标达成`、橙 `⚠️ 目标未达成`（模型用 `report_done` 声明了结果）/ 橙 `⏸️ 到了 80 步上限` / 灰 `⚠️ 这次没答出来 · 原因` / 红 `❌ 出错了`。飞书的 patch 是**整张重建**，所以每次刷新都重新带上 header，否则状态条会在第一次流式刷新后消失 |
| 没答完能接着跑 | 橙卡和灰卡带一个「继续」按钮，点它在**同一张卡片**里续跑；连点只跑一次（注册表里先认领再提交）。卡片上的「继续」和 `durable chat` 里的 `/continue` 走同一条路径 |
| 目标达成 / 未达成 | 色条上用哪个词由模型自己声明：干了活的一轮在回答前调 `report_done({ met, why })`，`why` 写怎么验证的（跑了什么、看到什么）或卡在哪。没声明就还是普通「答完了」——不声明不等于失败。这份声明卡片读一次、`app.turn-verdicts` 存一次，决策轨迹和早报里都能看到 |
| 执行详情 | 步骤时间线默认折叠在卡片里（`本轮执行详情 · 5 步 · 62s · $0.0031`），不再单发一条「查看详情」消息；同一面板先列出**决策轨迹**：问了你什么、等了多久、你允许还是拒绝、为什么结束 |

### 运行时间线

一轮回复被拆成可读的步骤，而不是一坨日志。transcript 里一轮只占一行 `⋯ 4 步 · 12s`，点开是每一步是什么：

```
⋯ 4 步 · 12s
  bash     cd ~/work/example-cli && timeout 90 node dist
  bash     python3 - <<'EOF' import json,re …
  思考
```

飞书卡片把同一轮折成 `Turn details · 5 步 · 62s · $0.0031`，两个入口数的是同一笔账。等审批的那段算在这个跨度里，所以「12 秒的一轮」其实大半在等你。

跑着的时候窗口不会自己看起来停了：live 行写着当前在跑哪个工具（`正在执行 bash`），答案就在下面流式长出来。这一轮失败的话，错误行上有「重试」，同一段话不用重打。

### 会话与项目

- **project** = 本机一个 git 仓库（内置 `workspace/` 是默认项目，删不掉）；ThinkRail 的 project → workspace → chats
- **chat** = 项目里的一个会话，自动 `git worktree add` 到 `<project>/.worktrees/conv-<id>`，分支 `agent/conv-<id>`
- 每个会话记录 **fork 点**和**当前基线**，基线可改（换成别的分支会提示"会把它上面的提交也算进来"）
- 会话可重命名 / 归档 / 清理 worktree（有未合并提交时拒绝清理）

### 代码

每个会话有自己的 worktree（`<project>/.worktrees/conv-<id>`，分支 `agent/conv-<id>`），桥能对工作区做 diff、提交、`--no-ff` 合回基线——`/api/changes`、`/api/git` 就是上一版客户端那两个面板的底座。Go 客户端现在不画这些面板：要 diff 就问一句，或者你自己在终端里看。transcript 里的工具输出就是文本，工具怎么打就怎么显示。

`workspace/` 及每个 project 的 `.worktrees/` 都写进仓库自己的 `.git/info/exclude`，不污染 `git status`，也不动用户的 `.gitignore`。

### 记忆

三层，作用域决定是否进入 prompt：

| 层 | 内容 | 注入 |
|---|---|---|
| `global_memory` | 关于你的稳定事实、偏好 | ✅ 最近 60 条 |
| `chat_memory` | 当前对话的上下文事实 | ✅ 最近 20 条 |
| `work_memory` | 项目/环境细节 | ❌ 只留一行索引，靠 `recall` 搜 |

- 工具：`remember(scope)` / `forget` / `recall`
- **群专属记忆**（飞书命令，owner 可改，群里所有人受益）：`/memo` 看、`/memo add 本群代号是金刚狼` 加、`/memo del 2` 删第 2 条、`/memo clear` 清空。写的就是该会话的 `chat_memory`，每次回答自动注入；guest 群也能用（只影响本群，不碰全局记忆）
- **手动整理**：读最近会话，只产出**建议**（带 scope + 理由），接受后才写入；`global` 每次最多 3 条。接口是 `POST /api/memory/curate`，Go 客户端还没有这个按钮
- 删除过的记忆进 `app/memory-tombstones`，不会被模型"复活"；去重用字符 bigram 相似度（换说法也能识别）

### 安全与成本

- **群里也能用 `/allow`**：飞书把 @ 渲染成 `@_user_1` 记号，之前替换成 `@` 后 `/allow` 变成 `@ /allow`，所有斜杠命令在群里都失效（单聊没 @ 所以看不出来）。现在先取到 bot 自己的 open_id，@ 它时整段去掉；找不到身份时命令匹配也会剥掉开头的 @ 前缀
- **本会话内全部允许**：卡片和客户端都有这个按钮。它按**风险类**（`rm` / `force-push` / `write-outside` / `chmod-777`…）在**当前会话**内记住，存在 `app.session-grants`，重启后仍有效；别的会话、别的风险类照样会问。风险类 key 有单测钉住，"同一个炸点不会被反复问，也不会一次放行所有危险操作"
- **有审计**：拒绝（guest 越界用工具）、级别变更、审批结果、两个只读查询各记一条，存在 `app.audit`（环形 300 条），`durable audit` 直接看；`durable doctor` 会打印每个会话的级别
- **审批**：`guard` hook 拦危险调用（`rm -rf`、`git push --force`、`sudo`、写工作区外…），**卡片发到你的私聊**（不再发在群里，群里只会看到“等主人确认”）；decision 存 durable memo，重启不重问。只有 `FEISHU_ALLOWED_SENDER_IDS` 里的 open_id 能点。**10 分钟没人点按拒绝处理**，和手点拒绝走同一条回调，卡片上写清是超时
- **同一份待审批**同时出现在飞书卡片和客户端（审批条替换掉输入框），任一处都可决定；**fail closed**（没有审批通道时默认拒绝）
- **审批等待**会记录时长并计入该轮时间线
- **额度**：`pi.usage` 汇总 → 每日上限（`AGENT_DAILY_BUDGET_USD`），超了拒绝新请求，跨天重置
- **一个群一个级别**：`app.chat-access` 里只有三档——`closed`（不对任何人）/ `owner`（只对你）/ `group`（所有人，guest 待遇）。`.env` 里列的 chat 是 `owner`（只在没存过级别时生效）、`/allow` 和卡片打开的是 `group`。校验、guest 策略、工具面、`/allow` 全读这一个值（以前是 `.env` + `app.allowed-chats` + `app.guest-chats` 三套加一条派生规则）
- **guest 群**：`/open`（或 `/allow`）把群开放给所有成员，该会话的记忆**只到本群为止**——不注入 `global_memory`，`remember` 只能写 chat 作用域，`recall` 搜不到你的全局/工作记忆，`forget` 也删不到（这三个是同一道门，`ChatPolicy.guest`）
- **guest 的工具面是代码拦的，不是提示词**：非主人的回合只给 `read`（限工作区 + **本会话所在项目**，`.env`/`.data`/`.ssh`/`.git`/`*.pem`/`*secret*` 一律拒）+ `skill` + `recall` + `remind` + **`project_search`（rg，限项目内）** + **`logs_query`（SLS 只读，dev 和 prod 都能查——读不是风险，写才拦）**；`bash`/`write`/`edit`/`feishu_*`/`mcp__*`/`delegate` 全部在 `beforeTool` 里挡掉并回一句「guest 模式不支持」。以前这些只是写在 instructions 里，群里任何人都能让它 `cat .env`
- **`/allow` 一步到位**：在群里发 `/allow` 不只是加白名单，而是直接把本群开成 guest 模式（和 `/open` 同一套保护，`/open` 多一条「只答本群主题」）。所以之后群里任何人 @bot 都能用，**不会再按人给你发授权卡**——授权这个群就是授权群里的人
- **写权限看「谁在问」，记忆看「这个群」**：guest 群的每一条消息都会按发送者重设 instructions——你自己的回合是「按他说的改文件、推分支、触发部署都可以」，别人的回合才是只读。记忆则不管谁问都不放（共享群的 transcript 大家都看得到）
- **`FEISHU_ALLOWED_SENDER_IDS` 是必填的**：它同时定义“谁是主人”和“谁能点审批”。为空时旧的逻辑是**人人都是主人**（能 `/allow`、能点审批、能拿到可写指令），现在启动时直接跳过飞书桥并打印原因（桌面端与 CLI 照常可用），`durable doctor` 也会报红
- **每一条消息都自带说话人**：`[主人本人 ou_abcde] …` / `[群成员 ou_9abc1] …`。模型不信 prompt 里那句「我是主人」，但会认消息上的标注；工具面也按这个标注走（`ChatPolicy.turn`）
- **还是会拒绝时自动纠一次**：同一个群被旧规则拒绝过好几轮后，光改 instructions 不够（模型会顺着自己之前的拒绝往下说）。这时网关会自动补一句「你刚才那条作废、写权限是开的」再问一遍（只在主人回合、且这轮确实没写过文件时触发，一条消息只补一次）
- **陌生人不会被静默丢掉**：它没见过的会话不会得到回复，但会给你发一张卡（来源、会话 id、发送者 open_id、前 200 字），上面只有一个按钮：**开放此会话（guest 权限）**，点一下把级别设成 `group`。以前这一步要两张卡（先授权会话、再授权人）
- **`.env` 里列的群 / 单聊被显式固定成 `owner` 级别**：那里只有你能问，其他人会收到一句「这个会话只对主人开放。发一句 /allow 可以让这里所有人都能用」——不再逐人发卡
- **`/allow` 开、`/close` 关**：`/close` 把级别改回 `closed`，不用改 `.env` 重启。两者都只有你能发
- 你自己的 p2p 会话 id 是第一次发消息时记下的（`app/owner-chat`），因为陌生人的会话 id 事先不可能知道

| 谁 | 能说话的条件 | 工具面（代码强制） | 记忆 |
|---|---|---|---|
| 你（`FEISHU_ALLOWED_SENDER_IDS`） | chat 在白名单（`.env` 或 `/allow`） | 全开；危险调用弹审批卡到你的私聊 | 全量 |
| 群里的其他人 | 群已 `/allow`（guest 群） | `read`(限工作区+本会话项目、排敏感文件) + `skill` + `recall` + `remind` + `project_search` + `logs_query`(dev + prod，只读) | | 只本群 |
| 单聊里的人（卡片授权后） | 那个会话是 `group` 级别 | 同上 | 只本群 |
| 谁都没授权 | 会话级别 `closed` | 不回应，只给你发一张卡（一个按钮） | — |

### 主动性

- `remind` 工具创建 **durable 定时任务**：deadline 落库、任务睡觉、重启后照样到点触发
- `routine` 工具创建 **常驻定时任务**：到点自动在聊天里开一轮（`daily 09:00` / `weekdays 09:00` / `weekly mon 09:00` / `every 6h`），跑完结果发回那个聊天，然后接着睡。到点时机器是关着的，醒来只补一次，不会把落下的全补一遍。`routines` 列出/取消
- `POST /api/hook/<name>`：给飞书以外的东西（git hook、SLS 告警、别的机器上的 cron）一个叫醒 agent 的入口，起的是一整轮 turn，不是一条通知；立刻回 202，因为一轮要跑几分钟
- 左栏「运行中」显示后台任务 + 红黄绿状态点 + 触发时刻（`● 伸个懒腰 12:15`）
- 客户端窗口在后台时，一轮回答结束发原生通知

### 扩展

| | |
|---|---|
| Skills | 扫描 `./.pi/skills`、`./.agents/skills`、`~/.pi/agent/skills`、`~/.agents/skills`、`~/.claude/skills`（与 pi 一致，另加 Claude Code 目录）。prompt 只放名字，`skill` 工具按需取全文 |
| MCP | `mcp.json` 里的 servers（stdio / streamable HTTP）启动时连接，工具注册为 `mcp__<server>__<tool>` |
| 多角色 | `delegate` / `delegate_parallel` / `build_feature`；每个子 agent 是 task-owned conversation，崩了续跑、abort 级联、只允许一层 |
| 飞书原生 | 走本机已登录的 `lark-cli`（你的用户身份）：`feishu_agenda` 日历、`feishu_tasks` 待办、`feishu_create_task`、`feishu_doc_read` |

---

## 配置

都在 `.env` 里，每个键的说明见 `.env.example`。

| 键 | 作用 |
|---|---|
| `FEISHU_APP_ID`、`FEISHU_APP_SECRET` | 飞书自建应用；缺了飞书桥不启动 |
| `FEISHU_GW_URL`、`FEISHU_GW_TOKEN` | 桥怎么找到 feishu-gw（默认 `http://127.0.0.1:8788`）以及它认的 token |
| `FEISHU_ALLOWED_SENDER_IDS` | 谁是主人 —— 必填，也只有这些人能点审批 |
| `FEISHU_ALLOWED_CHAT_IDS` | 固定成 `owner` 级别的会话（留空则每个会话都从 `closed` 开始） |
| `AGENT_OWNER_NAME` | 提示词里怎么称呼主人；只影响措辞，不影响权限 |
| `FEISHU_DONE_PING_SECONDS` | 超过多少秒额外发一条完成消息；`0` 关闭 |
| `AGENT_DIGEST_AT` | 早报几点发（`08:30`，Asia/Shanghai）；`off` 关闭 |
| `AGENT_PROVIDER`、`AGENT_MODEL`、`AGENT_THINKING` | 默认 provider、模型、思考档 |
| `MAGPIE_BASE_URL`、`MAGPIE_API_KEY` | `AGENT_PROVIDER=magpie` 时的本机代理地址与 key |
| `AGENT_WORKSPACE`、`AGENT_DB` | 工具能碰的目录，和那个 SQLite 文件 |
| `AGENT_MAX_TURNS` | 一轮多少步工具调用后停住并给出「继续」 |
| `AGENT_DAILY_BUDGET_USD` | 每日花费上限，`0` 不限 |
| `AGENT_WORKTREE`、`AGENT_SKILLS_DIRS`、`AGENT_MCP_CONFIG` | worktree 模式、skill 目录、MCP 配置 |
| `AGENT_HTTP_PORT`、`AGENT_HTTP_HOST`、`AGENT_HTTP_TOKEN` | 客户端与 CLI 用的本机桥；`0` 关闭 |
| `SLS_LOGSTORE_PREFIX` | `logs_query` 的 logstore 名是 `<prefix>-<env>-stdout` |
| `AGENT_ASR` | 本地 whisper 转写：`auto` 或 `off` |

## 安装

macOS（launchd 与桌面端只做了 macOS），Node 22+，`git`。`ffmpeg`、`whisper`、`lark-cli` 是可选扩展：
语音转写、语音消息、飞书原生工具。

```bash
git clone https://github.com/ziludev/PiLot.git && cd PiLot
./bin/durable setup
```

`setup` 走六步，正好覆盖容易卡住的几处：

1. **Node 与依赖** —— 检查版本；`node_modules` 不在就跑 `npm install`。
2. **模型网关** —— DeepSeek / OpenAI / Anthropic / 本机 OpenAI 兼容代理；问 key 和模型 id，写进 `.env`。
3. **飞书应用** —— 凭证与基础信息里的 App ID、App Secret。
4. **你的 open_id** —— 这条决定谁是主人。留空的话，机器人所在每个群的每个人都算主人，能点审批、能改代码。
5. **把控制台读回来** —— 凭证、机器人能力、订阅方式、已发布版本、已订阅事件、`card.action.trigger`、权限、机器人是否已在某个会话；每个 ✗ 都写明去哪个菜单改。
6. **launchd** —— 交出去，开机自启、崩溃重启。

飞书开发者后台要点什么，只有一页：**[docs/feishu-setup.zh-CN.md](docs/feishu-setup.zh-CN.md)** ——
机器人能力、六个权限、长连接、一个回调、一个事件、发版。`setup` 读回来的就是这一页的内容，
所以它打 ✗ 的地方，去那一页找对应行即可。

之后：

```bash
durable doctor            # 同一套读回校验，随时可跑
./scripts/agentctl logs   # 看 agent 在做什么
./scripts/agentctl status # pid、runs、退出码
```

**为什么没有「一键装飞书应用」**：飞书自建应用是租户内的，App ID 和 Secret 只能你自己在自己的租户里建
（跨租户就意味着做 ISV 应用、走 OAuth 和审核上架）。所以自动化停在墙前一步：拿到两个凭据之后的所有事都脚本化并校验。
同租户的同事可以共用一个应用、各跑各的实例，谁能控制什么由主人名单决定。

### 桌面客户端

```bash
cd client-go
go run .                         # 直接连本地桥，开窗
go tool mygo build -skip-dmg     # 产出 build/darwin-arm64/PiLot.app
rsync -a --delete build/darwin-arm64/PiLot.app ~/Applications/   # 然后 open ~/Applications/PiLot.app
```

窗口由 mygo/ui 在 Metal 上直绘：没有 webview，没有前端构建步骤，也没有要一起打包的运行时。它对着桥开一条 websocket，飞书那边起的一轮会自己出现在窗口里；窗口里有三块——rail（会话 / 任务）、transcript、composer，审批条来时替换掉输入框。详见 `client-go/README.md`。

---

## CLI

```bash
ln -sf "$PWD/bin/durable" ~/.local/bin/durable   # 放到 PATH

durable                 # 命令总览
durable setup           # 引导式安装：依赖、网关、飞书凭据、体检、launchd
durable doctor          # 本地 + 飞书双侧体检
durable ask "…"         # 一次性提问（流式）
durable chat            # 多轮 REPL（/new /stop /exit）
durable sessions        # 会话列表（id / 来源 / 项目 / 标题）
durable approvals       # 待审批；durable approve <id> / deny <id>
durable access          # 每个会话的访问级别（closed / owner / group）+ 审批卡发往哪里
durable audit [n]       # 最近 n 条权限决定：谁、哪个会话、什么级别、拦了什么、谁批的
```

命令名是 `durable` 而不是 `agent`：`agent` 已被 grok 的 CLI 占用。

对话类命令走**本机 HTTP/WS 桥**，所以 launchd 托管着 agent 时也能用，不需要停进程。

**关于「一键授权飞书」**：飞书自建应用是租户级的，App ID/Secret 只能本人在自己租户的开发者后台创建（跨租户要发布成 ISV 商店应用并过审）。所以 CLI 自动化的是创建之后的一切——写配置、校验每个开关、启动、诊断。同租户的同事可以共用一个应用、各自跑实例。

---

## 运维

```bash
./scripts/agentctl install          # 渲染 plist 并装载（开机自启 + 崩溃重启）
./scripts/agentctl install-gateway  # 飞书长连接，单独一个进程 :8788
./scripts/agentctl logs-gateway [n]
./scripts/agentctl install-backup   # 每天 04:10 的 sqlite 热备份
./scripts/agentctl start|stop|restart|status
./scripts/agentctl logs [n]
./scripts/agentctl doctor           # 本地自检（node/git、.env、DB、写者锁、进程数、端口、launchd 重启次数、worktree）
./scripts/agentctl backup|backups|restore <file>
./scripts/agentctl rotate           # 立刻切分日志（5MB × 3，`LOG_MAX_BYTES` 可调）
./scripts/agentctl uninstall
```

- plist 是模板（`launchd/*.plist.tmpl`），`install` 时渲染实路径，换机器不用手改
- **两个 launchd job，一份会话状态**：`com.pitta.agent` 跑桥，`com.pitta.agent.feishu-gw` 跑飞书长连接（`logs/feishu-gw.log`，`KeepAlive`，10 秒节流），互相重启不到对方。gateway 挂掉没有任何症状——群里就是不回话，`agentctl status` 是唯一会说出来的地方（`feishu: ok (gateway connected)`）
- `agentctl` 现在是 Go 程序（`agentctl/`，构建到 `bin/agentctl`），`./scripts/agentctl` 只是转发、并在源码更新时自动重建的 shim：命令和 launchd label 一个没变，只是控制逻辑不再是那个会被引号绊倒的 357 行 shell
- 备份：`sqlite3 .backup` 热备 → `journal_mode=DELETE` 收成单文件 → 完整性校验 → 保留 14 份；恢复会先停进程、等它真正退出
- **一份 storage 只能一个进程写**：`openRuntime()` 用 `wx` 独占创建 `.data/agent.owner`（pid），第二个进程直接报错而不是把运行中的会话搞坏。**pid 被系统复用不会挡住启动**：除了 `kill(pid,0)`，还要 `ps -p <pid> -o command=` 里确实是 `node … main.ts` 才认（硬杀或重启后，那个号可能已经归别人；以前这种情况 launchd 会一直重启失败，看起来就是"开机后进程没起来"）
- **写坏了会自己重开**：storage 一旦报 `Session failed after a storage error`（比如撞上重复 id），session 就永久废了——只有重开进程能救。所以 bridge 遇到这种错误会直接 `exit`，让 launchd 起新进程；10 分钟内不重复触发（标记在 `.data/storage-selfheal`），避免真的坏库变成重启死循环
- 日志：`logs/agent.log`、`logs/agent.err.log`、`logs/backup.log`；`[report] <时间戳> <kind>` 是 hook/tool/listener 的失败（不会抛进 harness，只能在这看）
- `doctor` 的判据是**相对量**：重启次数与上次体检比（`+N since last check`），错误只看近两天；旧日志里没有时间戳的行单独计数，不会永远冒充"新故障"

---

## 结构

```
src/
  main.ts          进程入口（bridge + 飞书 + 优雅退出；harness 失败即退出让 launchd 重启）
  server.ts        本机 HTTP/WS 桥：客户端和 CLI 的唯一入口
  harness.ts       Harness.open、扩展装配、chat↔conversation 映射、project/base 解析
  feishu.ts        卡片流式、审批按钮、图片/语音、命令、额度闸门（长连接在 feishu-gw/）
  feishu-gw.ts     一个走 gateway 的飞书 IM 客户端：发送、回复、改写、取发送人、附件
  cli/main.ts      `durable` 子命令；cli/feishu-check.ts 是飞书侧体检
  personal.ts      人设 / 时间 / 工作区 section
  roles.ts         子 agent 角色：指令 + 工具白名单 + 轮次上限
  subagents.ts     delegate / delegate_parallel / build_feature（durable 子会话）
  settle.ts        任务收尾：写终态时把「已经是终态 / 已被中止 / session 已关」当成正常，别的错照样抛
  memory.ts        global / chat / work 三层记忆 doc、remember/forget/recall
  curator.ts       手动整理：读会话 → 只产出建议
  reminders.ts     durable Reminder task（sleepUntil）+ remind 工具
  routines.ts      常驻定时任务：会开一轮 turn 的提醒，永不 settle
  approval.ts      guard hook：危险调用识别 + 审批
  approvals.ts     审批注册表（飞书卡片与客户端共用）
  budget.ts        每日花费 doc + 闸门
  usage-days.ts    按日 usage 采样（后台 durable 任务）+ 方块图数据
  projects.ts      project 列表、会话↔项目/基线绑定
  worktree.ts      每会话一个 git worktree/分支；merge 回基线
  git-info.ts      commit 列表、单 commit patch、push、PR/MR 链接
  changes.ts       工作区 status + unified diff + 提交
  skills.ts        SKILL.md 扫描（名进 prompt，全文按需取）
  mcp.ts           MCP client，把 server 的工具注册成 durable 工具
  lark-tools.ts    飞书原生工具（走 lark-cli 用户身份）
  asr.ts           本地 whisper 转写（ffmpeg + whisper）
  access.ts        一个 chat 一个级别（closed/owner/group）+ guest 策略 + 主人私聊；guest.ts / allowed.ts 已合并进来
  diagnostics.ts   给 guest 用的两个只读工具：project_search（rg 限项目）、logs_query（SLS，prod 需主人本人）
  decisions.ts     一轮的决策轨迹（问了什么、你答了什么、为什么结束）
  done.ts          report_done 工具 + 卡片照抄的那个结论
  verdicts.ts      结论环形缓冲，早报读它
  digest.ts        每天一条的早报（结论 + 审计拼成），以及那个 sleep 的任务
  ketchup.ts       `/ketchup` 的按需读取
  audit.ts         权限决定的环形审计（app.audit）：拒绝/级别变更/审批/查询
  surface-meta.ts / threads.ts / cards.ts / details.ts
  live.ts / log.ts / similar.ts / channel.ts / runtime-ref.ts / storage-lock.ts / models.ts

evals/             离线评测台：guard（审批判定）+ contract（config↔.env.example↔README），`npm run eval`
docs/            只有一页需要细读的：飞书控制台按菜单列，以及每行配错会出现的报错

client-go/        桌面 app（Go + mygo/ui on Metal，无 webview）
  main.go         窗口、菜单、启动
  internal/app    rail / transcript / composer / 审批 / 任务页 / 偏好 / 快照
  internal/api    桥的 HTTP 客户端、WS 订阅（自愈重连）、/api/health
  internal/bridge 地址、token、agentctl
  internal/protocol / theme / i18n / icons
feishu-gw/        飞书长连接单独一个 Go 进程：ack、转发、上传下载
agentctl/         install / start / stop / logs / doctor / backup 的 Go 实现（bin/agentctl）
toolbox-go/       编码工具做成 unix socket 上的 Go 服务：bash / read / write / edit
```

---

## 实测过的行为

| 场景 | 结果 |
|---|---|
| 崩溃续跑 | `sleep 10` 跑到一半 `kill -9`，新进程把工具调用标为中断，模型改后台重跑并答完 |
| 重启保持 | conversation id、记忆、会话↔项目映射、会话↔卡片登记都不变；不重投，因为认领（`live→delivering`）在 transaction 里，两个进程只有一个能拿到 |
| pid 被复用 | `.data/agent.owner` 里的 pid 已被别的进程占用时照常启动（`ps` 校验命令行为 `node … main.ts` 才拦） |
| 工具审批 | `rm -rf` 被拦 → 飞书卡片点「允许」/ 客户端 API 允许 → 工具真实执行；等待时长计入时间线 |
| worktree 隔离 | 会话 A 写文件，主树与其他会话看不到；合并 `--no-ff` 后出现在主树 |
| 子 agent | designer → implementer → tester 全跑通；子会话独立计费、崩了续跑 |

其余的证词 —— 图片识别、未合并保护、存储写者锁、58 条守卫用例 —— 在 `evals/` 和 `tests/` 里，是跑出来的，不是写出来的。

---

## 测试与评测

```bash
npm test                      # 纯逻辑单测（node --test），秒级
npm run eval                  # 离线评测：守卫判定 + 配置/文档契约，不需要模型
npm run eval -- --strict      # 让「已知缺口」也算失败
npm run eval -- --json        # 一个 envelope，给脚本读
```

- `tests/` 钉函数行为，`evals/` 钉**契约**：`approvalReason()` 对 58 条真实命令的判定，以及 `config.ts` / `.env.example` / `README` 三者还对不对得上
- 最要紧的指标是**逃逸**：危险调用没人问就跑了。已知漏洞不装看不见——case 标 `gap: true`，输出里列出来、`--strict` 变红，修好后自动提示删掉那行
- 第一次跑就抓到 9 处 `.env.example` 缺项（`FEISHU_DONE_PING_SECONDS`、`AGENT_HTTP_*`、`AGENT_WORKTREE` …）和 1 处默认值写错：例子还写着 `AGENT_MODEL=auto/*` 路由组，而代码注释明确说路由组会让同一个任务一天成一天败
- 详见 `evals/README.md`

---

## 已知限制

- Go 客户端这一版只有 rail / transcript / composer / 审批。上一版客户端里的改动、提交、文件、记忆整理面板都还没有搬回来——桥上的接口还在，少的只是按钮
- 测试覆盖的是纯逻辑 + 契约（记忆去重、审批注册表、diff 采集、远端链接、危险命令判定、文档与配置对齐），不是端到端；`npm test` 和 `npm run eval` 都是秒级
- 一轮最多 80 步工具调用（`AGENT_MAX_TURNS`）。到上限不是出错：卡片会写「跑到 N 步上限」并提示回「继续」，transcript 都在，接着说就行。以前默认 40 步，两个真排查任务（SLS/DMS 日志、lark-cli 拉人）都死在这条线上
- Pi Durable 是 **experimental**，1.x 之间 API 会变；升级前先 `npm run typecheck`。注意 pi-durable 会连带锁死 chord 版本：1.5.1 起 chord v2 成为包根，`Context` 只在 `@earendil-works/chord/context`，从包根 import 会编译失败
- SQLite 用 `synchronous=NORMAL`：进程崩不丢，掉电可能丢最后一个 commit
- `bash` 理论上能 `cd` 出工作区；靠 guard hook 兜危险性，不是沙箱
- 跨租户"一键装飞书"做不到（见上文 CLI 一节）
- 方块图从采样器启动那天开始积累，更早的历史没有逐日数据
- 飞书卡片不是 Markdown，它只认自己那套子集：没有表格、没有行内代码、没有标题、没有引用块、没有 `---`。卡片构造时会把它们翻译过去（表格变原生 `table` 元素，`##` 变加粗，`---` 变分隔线，行内反引号去掉，`>` 变 `▸`），这套之外的语法会原样显示
- 一张卡片上限 9000 字。超长的答案在这条线上截断（会把围栏补齐并加省略号），不做跨卡拆分；目前最长的真实答案是 3543 字

---

## 下一步候选

- **follow-up 计时器**：让它自己约「两小时后再看一次这个 MR」。`sleepUntil` 已经有了，缺的是判断规则。
- **五条具名原则**：把散在 `personal.ts`、`SECURITY.md`、`AGENTS.md` 的二十来条约束收成 5 条有名字的（少加实体 / 先量再改 / 别在代码里修网络 / 状态只有一个来源 / 说话要有证据），prompt 只放名字。
- **子 agent 的终态契约**：`build_feature` 的角色产出从自由文本改成 PASS / ISSUES / BLOCKED + 证据，父 agent 才能机械判定。

量过不值得做的：空转成本（`/api/state` 10–20ms / 21 个会话，占空比 0.1%）、主动发件箱（`app.reminder` 历史触发 0 次）。

做完的：会话级花费与耗时、项目移除、话题在客户端可读、断线告警、纯逻辑单测、`/ketchup`、决策轨迹、`report_done`、每日早报。

---

## 许可

MIT，见 [LICENSE](LICENSE)。
