import assert from "node:assert/strict";
import { test } from "node:test";
import { ChatMemory, GlobalMemory, WorkMemory, memoryTools, type Note } from "../src/memory.ts";
import { ChatPolicy } from "../src/access.ts";

/**
 * recall used to be all-or-nothing: every keyword had to be a substring of the note, or the note
 * did not exist. One wrong extra word ("缓存 命中率 上报" for a note that says "缓存命中率没有上报")
 * returned nothing, and the agent concluded it never learned the fact. Exact hits still win; a
 * partial hit is now shown as a lead, with its score on the line.
 */

type Notes = { global: Note[]; chat: Note[]; work: Note[] };

function note(text: string): Note {
  return { at: "2026-11-11T00:00:00.000Z", text };
}

function fakeApi(notes: Notes) {
  const docOf = (doc: unknown): { notes: Note[] } => {
    if (doc === GlobalMemory) return { notes: notes.global };
    if (doc === ChatMemory) return { notes: notes.chat };
    if (doc === WorkMemory) return { notes: notes.work };
    throw new Error("unexpected doc");
  };
  return {
    conversationId: 1,
    snapshot: async (doc: unknown) => (doc === ChatPolicy ? { isolated: false } : docOf(doc)),
    commit: async (body: (tx: { doc: (doc: unknown) => { notes: Note[] } }) => Promise<void>) => {
      await body({ doc: docOf });
    },
  };
}

type Tool = {
  name: string;
  execute: (
    args: Record<string, unknown>,
    api: never,
    context: never,
  ) => Promise<{ output: Array<{ text: string }> }>;
};

const recall = memoryTools.find((candidate) => candidate.name === "recall") as unknown as Tool;

async function recallText(notes: Notes, query: string): Promise<string> {
  const result = await recall.execute({ query }, fakeApi(notes) as never, {} as never);
  return result.output[0]?.text ?? "";
}

function seeded(): Notes {
  return {
    global: [note("缓存命中率一直没有上报到 telemetry")],
    chat: [note("11-20 讨论过缓存过期策略")],
    work: [note("dev 环境的 redis 只有一个节点")],
  };
}

test("an exact hit is the answer and partial ones stay out of it", async () => {
  const text = await recallText(seeded(), "缓存 命中率");
  assert.match(text, /一直.*没有上报/);
  assert.doesNotMatch(text, /部分命中/, "no partial section when a note matched every term");
  assert.doesNotMatch(text, /过期策略/, "a note sharing one term is not shown next to the answer");
});

test("with no exact hit, partial hits come back ranked, with their score", async () => {
  const text = await recallText(seeded(), "缓存 命中率 上报 恢复");
  assert.match(text, /没有全词命中的记忆，以下是部分命中的/);
  assert.match(text, /一直没有上报到 telemetry（命中 3\/4 词）/);
  assert.match(text, /过期策略（命中 1\/4 词）/);
  const ranked = text.indexOf("一直没有上报");
  assert.ok(ranked < text.indexOf("过期策略"), "more shared terms rank higher");
});

test("a note with no shared term is still not a hit", async () => {
  const text = await recallText(seeded(), "完全 无关");
  assert.equal(text, "没有匹配「完全 无关」的记忆");
});

test("matching ignores ascii case", async () => {
  const notes: Notes = { global: [note("Telemetry 只在 dev 上报")], chat: [], work: [] };
  const text = await recallText(notes, "telemetry");
  assert.match(text, /只在 dev 上报/);
});

test("a rare shared term ranks above a common one, even at the same score", async () => {
  // 缓存 appears in every note, 上报 in three: both leads below match two terms, but the one
  // holding the rare word is the one you wanted.
  const notes: Notes = {
    global: [
      ...Array.from({ length: 10 }, (_, index) => note(`缓存相关第 ${index} 条`)),
      ...Array.from({ length: 3 }, (_, index) => note(`上报第 ${index} 条`)),
      note("缓存 上报 一起出现"),
      note("缓存 ghostty"),
    ],
    chat: [],
    work: [],
  };
  const text = await recallText(notes, "缓存 上报 ghostty 不存在");
  const lines = text.split("\n").filter((line) => line.startsWith("- ["));
  assert.match(lines[0] ?? "", /ghostty/);
  assert.match(lines[1] ?? "", /一起出现/);
});

test("the partial list is capped and says how many were left out", async () => {
  const notes: Notes = {
    global: Array.from({ length: 12 }, (_, index) => note(`缓存相关第 ${index} 条`)),
    chat: [],
    work: [],
  };
  const text = await recallText(notes, "缓存 不存在的词");
  const lines = text.split("\n").filter((line) => line.startsWith("- ["));
  assert.equal(lines.length, 8);
  assert.match(text, /另有 4 条部分命中/);
});

test("an empty query asks for a keyword instead of dumping every note", async () => {
  const text = await recallText(seeded(), "   ");
  assert.equal(text, "recall 需要一个关键词");
});
