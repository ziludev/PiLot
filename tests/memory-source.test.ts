import assert from "node:assert/strict";
import { test } from "node:test";
import { ChatMemory, GlobalMemory, WorkMemory, memoryTools, type Note } from "../src/memory.ts";
import { applySuggestions } from "../src/curator.ts";

/**
 * Every remembered line carries its receipt. The window shows it ("来自 会话 #12")
 * and lets you jump back to the turn it came from — without it a note is a claim
 * you can only believe, and the first question about a wrong note is where it was
 * said. The three writers stamp different receipts: the agent mid-turn, the
 * nightly pass, and a memo typed into Feishu.
 */

type Notes = { global: Note[]; chat: Note[]; work: Note[] };

function fakeApi(notes: Notes) {
  const docOf = (doc: unknown): { notes: Note[] } => {
    if (doc === GlobalMemory) return { notes: notes.global };
    if (doc === ChatMemory) return { notes: notes.chat };
    if (doc === WorkMemory) return { notes: notes.work };
    throw new Error("unexpected doc");
  };
  return {
    conversationId: 12,
    snapshot: async () => ({ notes: [] }),
    commit: async (body: (tx: { doc: (doc: unknown) => { notes: Note[] } }) => Promise<void>) => {
      await body({ doc: docOf });
    },
  };
}

type Tool = {
  name: string;
  execute: (args: Record<string, unknown>, api: never, context: never) => Promise<unknown>;
};

function remember(): Tool {
  const found = memoryTools.find((candidate) => candidate.name === "remember");
  assert.ok(found !== undefined, "remember exists");
  return found as unknown as Tool;
}

test("a note the agent wrote mid-turn names the conversation it was said in", async () => {
  const notes: Notes = { global: [], chat: [], work: [] };
  await remember().execute({ text: "周五不提测" }, fakeApi(notes) as never, undefined as never);

  assert.equal(notes.global.length, 1);
  assert.equal(notes.global[0]?.source, "chat:12", "the receipt points at the turn");
});

test("the nightly pass signs its lines as its own", async () => {
  const notes: Notes = { global: [], chat: [], work: [] };
  const docOf = (doc: unknown) =>
    doc === GlobalMemory ? notes.global : doc === WorkMemory ? notes.work : notes.chat;
  const harness = {
    commit: async (body: (tx: { doc: (doc: unknown) => { notes: Note[] } }) => Promise<void>) => {
      await body({ doc: (doc) => ({ notes: docOf(doc) }) as { notes: Note[] } });
    },
  };

  await applySuggestions(harness as never, undefined as never, [
    { text: "分支只在个人分支出提 MR", scope: "global" },
  ]);

  assert.equal(notes.global.length, 1);
  assert.equal(notes.global[0]?.source, "curator", "a line nobody said out loud says so");
});
