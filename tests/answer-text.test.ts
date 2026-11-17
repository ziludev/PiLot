import assert from "node:assert/strict";
import { test } from "node:test";
import { answerTextInTx, answerTextOf, textPartsOf } from "../src/answer.ts";

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
