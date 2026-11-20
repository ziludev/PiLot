import assert from "node:assert/strict";
import { test } from "node:test";
import { ChatMemory, GlobalMemory, WorkMemory, memoryTools, type Note } from "../src/memory.ts";
import { ChatPolicy } from "../src/access.ts";

/**
 * A group conversation is a shared group opened with /allow or /open. Its members get the same
 * tools as the owner, so the only thing keeping the owner's memories out of their answers is the
 * isolation flag — a memory boundary, not a permission. These assert it still holds, under the
 * new name and under the old one.
 */

type Notes = { global: Note[]; chat: Note[]; work: Note[] };

/** The policy document as it is on disk: new docs say isolated, older ones said guest. */
type Policy = { isolated?: boolean; guest?: boolean };

function note(text: string): Note {
  return { at: "2026-11-11T00:00:00.000Z", text };
}

/** Just enough of the tool api: the three memory documents plus the chat policy. */
function fakeApi(notes: Notes, policy: Policy) {
  const docOf = (doc: unknown): { notes: Note[] } => {
    if (doc === GlobalMemory) return { notes: notes.global };
    if (doc === ChatMemory) return { notes: notes.chat };
    if (doc === WorkMemory) return { notes: notes.work };
    throw new Error("unexpected doc");
  };
  return {
    conversationId: 1,
    snapshot: async (doc: unknown) => (doc === ChatPolicy ? policy : docOf(doc)),
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

function tool(name: string): Tool {
  const found = memoryTools.find((candidate) => candidate.name === name);
  assert.ok(found !== undefined, `tool ${name} exists`);
  return found as unknown as Tool;
}

function seeded(): Notes {
  return {
    global: [note("项目代号金刚狼")],
    chat: [note("本群在改项目 X")],
    work: [note("项目部署在 dev 命名空间")],
  };
}

/** Write whatever the doc looks like on disk: `{ guest: true }` is what old rows hold. */
const ISOLATED: Policy = { isolated: true };
const LEGACY: Policy = { guest: true };
const OWNER: Policy = { isolated: false };

async function recallText(notes: Notes, policy: Policy, query: string): Promise<string> {
  const result = await tool("recall").execute({ query }, fakeApi(notes, policy) as never, {} as never);
  return result.output[0]?.text ?? "";
}

test("a group conversation recalls its own notes only", async () => {
  const text = await recallText(seeded(), ISOLATED, "项目");
  assert.match(text, /本群在改项目 X/);
  assert.doesNotMatch(text, /金刚狼/, "global memory must not leak into a shared group");
  assert.doesNotMatch(text, /dev 命名空间/, "work memory must not leak into a shared group");
  assert.doesNotMatch(text, /\[全局\]|\[工作\]/);
});

test("a doc written before the rename still isolates, the same way", async () => {
  const text = await recallText(seeded(), LEGACY, "项目");
  assert.equal(text, await recallText(seeded(), ISOLATED, "项目"));
});

test("the owner still recalls every scope", async () => {
  const text = await recallText(seeded(), OWNER, "项目");
  assert.match(text, /\[全局\].*金刚狼/);
  assert.match(text, /\[本对话\].*项目 X/);
  assert.match(text, /\[工作\].*dev 命名空间/);
});

test("a group conversation cannot delete the owner's notes", async () => {
  const notes = seeded();
  await tool("forget").execute({ keyword: "项目" }, fakeApi(notes, ISOLATED) as never, {} as never);
  assert.equal(notes.chat.length, 0, "the group's own note is the one it may remove");
  assert.deepEqual(
    notes.global.map((entry) => entry.text),
    ["项目代号金刚狼"],
  );
  assert.deepEqual(
    notes.work.map((entry) => entry.text),
    ["项目部署在 dev 命名空间"],
  );
});

test("the owner can still forget across every scope", async () => {
  const notes = seeded();
  await tool("forget").execute({ keyword: "项目" }, fakeApi(notes, OWNER) as never, {} as never);
  assert.equal(notes.global.length + notes.chat.length + notes.work.length, 0);
});

// The members have the owner's tools now, so `remember scope=global` from a group would be the
// one write that reaches every future owner turn. It must land in the group's own memory.
test("a group member's remember lands in the group's memory, whoever asked", async () => {
  const notes = seeded();
  await tool("remember").execute(
    { text: "测试库口令问运维", scope: "global" },
    fakeApi(notes, ISOLATED) as never,
    {} as never,
  );
  assert.deepEqual(
    notes.global.map((entry) => entry.text),
    ["项目代号金刚狼"],
    "global memory is the owner's, and a group write must not touch it",
  );
  assert.deepEqual(notes.chat.map((entry) => entry.text), ["本群在改项目 X", "测试库口令问运维"]);
});
