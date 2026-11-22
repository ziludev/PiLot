# Lessons

每一轮收尾时往下加一条：**这次哪里不顺手，下次怎么不一样**。
没有就不写，别凑数。这里不记流水账、不记「做了什么」，只记「下次会做什么不同的」。

攒到十几条，回头改对应 skill（`~/.claude/skills/` 里的 verify / route / correct）。
改完把旧条目划掉，别直接删——删错了没人记得为什么删。

格式：`- YYYY-MM-DD 现象一句话 → 下次的做法`

---

- 2026-11-22 有 4 个测试红着就推了 `5b2fa08`：`go test` / `npm test` 的输出被 `tail` 截断，只看见最后几行像绿的 → 跑测试看**退出码**，管道用 `set -o pipefail`；汇总行（`# fail 0`、`ok`）要真看见。
- 2026-11-22 被问「沉淀了什么 skill」，先去翻 `~/.claude/skills` 目录清单 → 先问「这个 skill 是谁写的」，社区装的（lark/baoyu/alibabacloud）不算沉淀。
- 2026-11-22 临时探针 `zz_shot_test.go` 和 6 张截图留在 `git status` 里差点跟着提交 → 探针一律带 `zz_` 前缀，提交前先 `git status --short | grep zz`。
- 2026-11-22 改了 UI 只凭控件坐标判断「对」→ 桌面 UI 必须截图看像素，配方见 `verify` skill 的「桌面 UI」一节。
- 2026-11-22 `go tool mygo build` 报 `Cannot find package 'mygo-cli'`：根 `node_modules` 被清过，而 manifest 里本来就没这条依赖 → `npm i mygo-cli@0.10.0 --no-save` 再构建，版本要跟 `go.mod` 的 mygo v0.10.0 对齐。
- 2026-11-22 裁图找 caret 找了三轮，因为它被裁在画面外 → 裁完先确认目标元素真在框里（含弹窗右边界），别把「没生成」当「没渲染」。
