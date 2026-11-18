import assert from "node:assert/strict";
import { test } from "node:test";
import { ChatMemory, GlobalMemory, WorkMemory, memoryTools, type Note } from "../src/memory.ts";
import { ChatPolicy } from "../src/access.ts";
import { CURATOR_PROMPT } from "../src/curator.ts";

/**
 * Global memory is injected every turn, so it has to stay an index: one trigger
 * and one action per line. Left unchecked it turns into a single ever-growing
 * summary, which research on memory harnesses (Meta FAIR, self-improving memory)
 * found scores *below* having no memory at all on codebase work. So a global note
 * that has grown into a document is refused, and one that is merely long gets a
 * nudge; work memory, which is fetched by keyword, may be as long as it needs.
 */

type Notes = { global: Note[]; chat: Note[]; work: Note[] };

function fakeApi(notes: Notes, guest = false) {
  const docOf = (doc: unknown): { notes: Note[] } => {
    if (doc === GlobalMemory) return { notes: notes.global };
    if (doc === ChatMemory) return { notes: notes.chat };
    if (doc === WorkMemory) return { notes: notes.work };
    throw new Error("unexpected doc");
  };
  return {
    conversationId: 1,
    snapshot: async (doc: unknown) => (doc === ChatPolicy ? { guest } : docOf(doc)),
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

function remember() {
  const found = memoryTools.find((candidate) => candidate.name === "remember");
  assert.ok(found !== undefined, "remember exists");
  return found as unknown as Tool;
}

const empty = (): Notes => ({ global: [], chat: [], work: [] });

test("a global note that became a document is refused, not stored", async () => {
  const notes = empty();
  const api = fakeApi(notes);
  const long = "分支规范：".repeat(90);
  const result = await remember().execute({ text: long }, api as never, undefined as never);

  assert.equal(notes.global.length, 0, "nothing was written");
  const said = result.output.map((part) => part.text).join("");
  assert.match(said, /没记/);
  assert.match(said, /work/);
});

test("a merely long global note is stored with a nudge toward work", async () => {
  const notes = empty();
  const api = fakeApi(notes);
  const text =
    [
      "终端：Ghostty 为主、Kitty 尝鲜；starship preset catppuccin-powerline，关掉语言段与时间段，cmd_duration 关通知",
      "代理 7890，国内镜像与阿里云必须直连",
      "docker daemon 常没起、kubectl 无 context",
      "改配置前先 grep 官方默认值，评估新工具先看 stars/issues，别凭印象",
    ].join("；") + "。";
  assert.ok(text.length > 160 && text.length <= 400, `fixture is ${String(text.length)} chars`);
  const result = await remember().execute({ text }, api as never, undefined as never);

  assert.equal(notes.global.length, 1);
  const said = result.output.map((part) => part.text).join("");
  assert.match(said, /偏长/);
});

test("work memory takes the same text without complaint", async () => {
  const notes = empty();
  const api = fakeApi(notes);
  const long = "分支规范：".repeat(90);
  const result = await remember().execute({ text: long, scope: "work" }, api as never, undefined as never);

  assert.equal(notes.work.length, 1, "detail belongs in work memory");
  const said = result.output.map((part) => part.text).join("");
  assert.doesNotMatch(said, /没记/);
});

test("a short trigger line goes in without noise", async () => {
  const notes = empty();
  const api = fakeApi(notes);
  const result = await remember().execute(
    { text: "说「出方案」= 写技术方案/设计文档。" },
    api as never,
    undefined as never,
  );

  assert.equal(notes.global.length, 1);
  assert.match(result.output.map((part) => part.text).join(""), /已记住（global/);
});

test("the curator asks for failure rules and entry points first", () => {
  // The highest-value notes in the harness study were symptom → fix pairs and
  // the paths/jobs needed to act on them, not summaries of what happened.
  assert.match(CURATOR_PROMPT, /踩过的坑与修法/);
  assert.match(CURATOR_PROMPT, /症状 → 什么做法/);
  assert.match(CURATOR_PROMPT, /job 叫什么|日志怎么查/);
  assert.match(CURATOR_PROMPT, /触发 → 动作/);
  assert.match(CURATOR_PROMPT, /不是一段总结/);
});
