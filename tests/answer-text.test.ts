import assert from "node:assert/strict";
import { test } from "node:test";
import { answerBlocksOf, answerHead, answerTextInTx, answerTextOf, composeAnswer, textPartsOf } from "../src/answer.ts";

const assistant = (...parts: unknown[]) => ({ model: [{ role: "assistant", content: parts }] });
const text = (value: string) => ({ type: "text", text: value });

test("a turn that ends with a tool call keeps what it said before the call", () => {
  // The shape that swallowed answers: analysis, report_done, one-line follow-up. The submission's
  // `answer` points at the last entry only, so the range read must be what carries the analysis.
  const entries = [
    { model: [{ role: "user", content: "帮我看看这个项目" }] },
    assistant(text("第一段：结论。")),
    assistant(text("第二段：对比表。")),
    { model: [{ role: "toolResult", content: [text("✅ 目标达成")] }] },
    assistant(text("你在飞书里随时说做哪一件。")),
  ];
  assert.equal(
    answerTextOf(entries),
    "第一段：结论。\n\n第二段：对比表。\n\n你在飞书里随时说做哪一件。",
  );
});

test("tool results and the owner's own words are not answer text", () => {
  const entries = [
    { model: [{ role: "user", content: "问题" }] },
    { model: [{ role: "toolResult", content: [text("命令输出")] }] },
  ];
  assert.equal(answerTextOf(entries), "");
});

test("thinking and tool calls are not answer text", () => {
  assert.equal(
    textPartsOf(assistant({ type: "thinking", thinking: "想想" }, { type: "toolCall", name: "bash" })),
    "",
  );
});

test("text parts inside one entry stay in one block", () => {
  assert.equal(textPartsOf(assistant(text("前半"), text("后半"))), "前半后半");
});

test("empty and malformed entries are skipped", () => {
  assert.equal(answerTextOf([{}, { model: [] }, { model: [{ role: "assistant" }] }]), "");
});

test("a run is read across pages and in order", async () => {
  // A page holds 200 entries and a run that hit the step limit goes past that, so the scan has to
  // follow the cursor — and entries have to arrive oldest first or the answer reads backwards.
  const pages = [
    [assistant(text("第0段")), assistant(text("第1段"))],
    [assistant(text("第2段")), assistant(text("第3段"))],
    [assistant(text("第4段"))],
  ];
  const queries: string[] = [];
  const tx = {
    entry: async () => pages[2]![0],
    scanEntries: async (query: { minEntryId: number; maxEntryId: number }, limit: number, cursor?: string) => {
      const page = Number(cursor ?? 0);
      queries.push(`${String(query.minEntryId)}-${String(query.maxEntryId)}/${String(limit)}/${String(cursor)}`);
      return { items: pages[page] ?? [], next: page + 1 < pages.length ? String(page + 1) : undefined };
    },
  };
  const answer = await answerTextInTx(tx as never, 1 as never, 99 as never, 2 as never);
  assert.equal(answer, "第0段\n\n第1段\n\n第2段\n\n第3段\n\n第4段");
  assert.deepEqual(queries, ["2-99/200/undefined", "2-99/200/1", "2-99/200/2"]);
});

test("without a starting entry only the answer entry is read", async () => {
  // A "continue" run places no entry of its own: reading back to the owner's message would
  // repeat what an earlier card already showed.
  let scans = 0;
  const tx = {
    entry: async () => assistant(text("接着说完的那段")),
    scanEntries: async () => {
      scans += 1;
      return { items: [] };
    },
  };
  assert.equal(await answerTextInTx(tx as never, 1 as never, 99 as never, undefined), "接着说完的那段");
  assert.equal(scans, 0);
});

/** A turn narrates, calls a tool, narrates again: the card used to open with four orphan headings. */

const narration = (text: string, ...tools: string[]) => ({
  model: [
    {
      role: "assistant",
      content: [
        { type: "text", text },
        ...tools.map((name) => ({ type: "toolCall", name, arguments: {} })),
      ],
    },
  ],
});

test("the answer comes first and the narration follows it", () => {
  const blocks = answerBlocksOf([
    narration("现在把清理返回值改成实际删除数，并在截断时告警：", "edit"),
    narration("现在把清空动作从事务里挪出来：拆成独立的前置方法。", "bash"),
    narration("三轮改完了，但 OCR 第四轮又提了 3 条 medium，按你说的上限我停手没再改，也还没部署——先跟你确认。另外附上这三条的判断和部署计划，等你点头。"),
  ]);
  const text = composeAnswer(blocks);
  assert.match(text, /^三轮改完了/, "the conclusion is the first thing read");
  assert.match(text, /_过程 2 段：_/);
  assert.match(text, /· 现在把清理返回值改成实际删除数，并在截断时告警（edit）/);
  assert.match(text, /· 现在把清空动作从事务里挪出来：拆成独立的前置方法。（bash）/);
});

test("a one-line ending keeps the analysis above it", () => {
  const blocks = answerBlocksOf([
    narration("排查结论：这个 case 是换男方清残留，relay 的 source 没推进，所以更早的气泡一直留着。改法是先把清场从事务里拆出来，再按实际删除数推进水位。"),
    narration("已部署到 release/test。", "report_done"),
  ]);
  const text = composeAnswer(blocks);
  assert.match(text, /^排查结论/, "the analysis is the answer here, so it stays first");
  assert.ok(text.includes("已部署到 release/test。"));
  assert.ok(!text.includes("_过程"), text);
});

test("a closing offer after a long answer is a footnote, not the answer", () => {
  // The shape that made a 1000-char plan render as one line: the model writes the answer, calls
  // report_done, then adds one sentence of its own. Absolute length alone can't tell that apart
  // from a real follow-up — relative length can.
  const plan = [
    "**方案已写到 docs/feishu-go-gateway.md（ab748c7）。核心判断：不整包迁移，加一个 Go 中间进程。**",
    "为什么：SDK 只碰传输层，19 个调用点归结为 5 个操作，而卡片内容是纯 JSON，与 SDK 无耦合。",
    "架构：feishu-gw 用 oapi-sdk-go/v3 收长连接、发消息，桥接层通过 HTTP 调它，卡片 JSON 原样透传。",
    "按键回调是唯一要小心的：飞书给 card.action.trigger 的响应有 3 秒超时，所以对它是同步转交。",
    "分四阶段：只收事件 → 接上发送层 → 切默认观察七天 → 删掉 Node 路径。成本 2 到 2.5 人日。",
  ].join("\n\n");
  const blocks = answerBlocksOf([
    narration(plan, "report_done"),
    narration("如果你需要，我可以先把 P1（只收事件、不发送消息）跑通，再决定是否继续做发送层。"),
  ]);
  const text = composeAnswer(blocks);
  assert.match(text, /^\*\*方案已写到/, "the plan is the answer, so it leads");
  assert.ok(text.includes("成本 2 到 2.5 人日"), "the plan is not condensed into one bullet");
  assert.ok(text.includes("我可以先把 P1"), "the closing offer is kept as its own line");
  assert.ok(!text.includes("_过程"), text);
});

test("a long narration line ends on a sentence, not mid-parenthesis", () => {
  const blocks = answerBlocksOf([
    narration(
      "查到了，就是刚才 20:10:53 那一次。**这次转换失败的原因是调用 ElevenLabs 转换接口时 HTTP 请求超时（8 秒上限），重试一次就过了，所以既不是稳定复现的 bug，也不是音色数据的问题，而是超时阈值太紧。**除此之外没有任何报错。",
      "report_done",
    ),
    narration("两点补充说明，方便你直接回复熊非：超时阈值改到 20s 并加一次重试；陪陪那边不用重新录制，任务已经关闭。"),
  ]);
  const text = composeAnswer(blocks);
  assert.match(text, /_过程：_/, "one narration block needs no segment count");
  const line = text.split("\n").find((item) => item.startsWith("· ")) ?? "";
  assert.ok(!line.includes("**"), line);
  assert.ok(line.includes("20:10:53 那一次。"), line);
  assert.ok(
    !/（[^（）]*…$/.test(line.replace(/（report_done）$/, "")),
    `the cut is not inside a parenthesis: ${line}`,
  );
});

test("a narration with no sentence inside the limit cuts on a clause", () => {
  const blocks = answerBlocksOf([
    narration(
      "先把数据库里那条 submission 的账查清楚，再看 bridge 的重启记录，然后把两条日志对齐逐个时间点比对，最后确认是投递重复而不是回合重跑，顺带把 accepted 计数也拉出来核对一遍。",
      "bash",
    ),
    narration(
      "结论：重复投递来自 ack 时机——SDK 要等 handler resolve 才确认，而 handler 等整个回合，于是同一批消息被投了三次。已改成 handler 立即返回，ops 那侧的计数也一起改了。",
    ),
  ]);
  const text = composeAnswer(blocks);
  const line = (text.split("\n").find((item) => item.startsWith("· ")) ?? "").replace(/（bash）$/, "");
  assert.ok(line.endsWith("…") || line.endsWith("。"), line);
  assert.ok(line.includes("，"), "the visible part is whole clauses, not a chopped word");
});

test("the notification line is the first thing worth reading, not the markup", () => {
  // The ping is the only thing that reaches a locked phone, so it has to be the sentence, not
  // "## 结论" or a table rule.
  assert.equal(answerHead("## 结论\n\n定位到了：轮询把 startedAt 覆盖了。"), "结论");
  assert.equal(answerHead("\n\n| 项 | 值 |\n| --- | --- |\n| 分支 | main |"), "项 · 值");
  assert.equal(answerHead("```\ngo test ./...\n```\n\n全绿。"), "全绿。");
  assert.equal(answerHead("- **已修**：审批条"), "已修：审批条");
});

test("a long first line keeps a whole sentence, and only an endless one gets cut", () => {
  // Forty characters ended the ping a word into the conclusion, so every report read as truncated.
  const sentence = "重启后那些蓝着的「处理中」卡片，查了代码发现早就实现了恢复循环，所以这一项不用做。";
  assert.equal(answerHead(sentence), sentence);
  // Two sentences fit; three do not, and the cut lands on the sentence end rather than mid-clause.
  assert.equal(answerHead(sentence + sentence), sentence + sentence);
  assert.equal(answerHead(sentence.repeat(3)), sentence.repeat(2));
  // No sentence end and no clause to land on: cut, but say so.
  const endless = answerHead("一".repeat(200));
  assert.equal(endless.length, 120);
  assert.ok(endless.endsWith("…"));
  // A clause boundary beats an arbitrary character.
  assert.equal(answerHead(`${"甲".repeat(60)}，${"乙".repeat(120)}`), `${"甲".repeat(60)}…`);
  assert.equal(answerHead(""), "");
  assert.equal(answerHead("---"), "");
});
