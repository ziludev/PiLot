import { defineDoc, defineExtension, defineTool, section } from "@earendil-works/pi-durable";
import { obj, opt, str } from "@earendil-works/pi-schema";
import type { Harness } from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";
import { ChatPolicy, isIsolatedPolicy } from "./access.ts";

/**
 * One remembered line, plus where it came from. The receipt matters: a fact the
 * window cannot trace is one the user has to take on faith, and the first thing
 * they want when a note is wrong is the conversation it was said in.
 * `chat:<id>` is the agent remembering mid-turn, `curator` the nightly pass,
 * `feishu` a memo typed into chat.
 */
export type Note = { at: string; text: string; source?: string };

/** The receipt for a line the agent wrote while talking in this conversation. */
export function chatSource(conversationId: number): string {
  return `chat:${conversationId}`;
}

/** Notes that belong to one chat. */
export const ChatMemory = defineDoc<{ notes: Note[] }>({
  kind: "app.memory",
  version: 1,
  scope: "conversation",
  history: "latest",
  fork: "initial",
  initial: () => ({ notes: [] }),
  checkpointWhen: () => false,
});

/**
 * Notes that follow the owner everywhere. Session scope so every chat shares them:
 * "我偏好先给结论" should not have to be repeated per conversation.
 */
export const GlobalMemory = defineDoc<{ notes: Note[] }>({
  kind: "app.global-memory",
  version: 1,
  scope: "session",
  initial: () => ({ notes: [] }),
  checkpointWhen: () => false,
});

/** Injected every request, so keep it bounded: recent memory only, everything else via `recall`. */
/**
 * Project/environment notes: real knowledge, but noise in every prompt. Kept out of
 * the injected sections and reachable through `recall` instead.
 */
export const WorkMemory = defineDoc<{ notes: Note[] }>({
  kind: "app.work-memory",
  version: 1,
  scope: "session",
  initial: () => ({ notes: [] }),
  checkpointWhen: () => false,
});

const MAX_GLOBAL_NOTES = 60;
const MAX_CHAT_NOTES = 20;
/** Partial hits are leads, not answers: a short list, ranked by how many terms they share. */
const MAX_PARTIAL_HITS = 8;
/** A global note past this length stops being an index line and becomes an archive. */
const INDEX_LIMIT = 160;
/** Past this, a global note is refused: the detail belongs in work memory. */
const HARD_LIMIT = 400;

const remember = defineTool({
  name: "remember",
  description:
    [
      "记住一条会改变以后行为的稳定事实，写成「触发 → 动作」：什么情况下做什么。",
      "例：「说「出方案」= 写技术方案/设计文档」「Ghostty 写 zshrc 会被 sandbox 拒」。",
      "一句话一条，一条只讲一件事。",
      "global（默认）每轮都注入，所以它是指引不是档案：只写触发条件 + 动作，细节（路径、配置、多步流程）写 work，" ,
      "在 global 留一句带 recall 关键词的索引。work 不注入、可以长，靠 recall 搜。",
      "不要记流水账、临时代号、验证码、一次性口令或本轮任务的中间结果。",
    ].join(""),
  parameters: obj({
    text: str({ description: "要记住的内容，一句话" }),
    scope: opt(
      str({
        description:
          "global（默认，跨对话共享，每次注入）/ chat（只属于当前对话，每次注入）/ work（项目与环境细节，不注入，靠 recall 搜）",
      }),
    ),
  }),
  execute: async (args, api, context) => {
    const at = new Date().toISOString();
    const text = args.text.trim();
    // An isolated conversation (a shared group) writes to its own chat memory only: what a
    // colleague tells the bot is not the owner's shared memory to inherit.
    const policy = await api.snapshot(ChatPolicy, api.conversationId, context).catch(() => undefined);
    const isolated = isIsolatedPolicy(policy);
    const requested = args.scope === "chat" ? "chat" : args.scope === "work" ? "work" : "global";
    const scope = isolated && requested !== "chat" ? "chat" : requested;
    if (scope === "global" && text.length > HARD_LIMIT) {
      return {
        output: [
          {
            type: "text" as const,
            text: `没记：这条 ${String(text.length)} 字，全局记忆是索引不是档案。把触发条件 + 动作压成一句放到 global，细节（路径/配置/多步流程）用 scope="work" 再记一条。`,
          },
        ],
      };
    }
    let total = 0;
    let duplicate = false;
    await api.commit(async (tx) => {
      const doc =
        scope === "global"
          ? await tx.doc(GlobalMemory)
          : scope === "work"
            ? await tx.doc(WorkMemory)
            : await tx.doc(ChatMemory, api.conversationId);
      duplicate = doc.notes.some((note) => note.text.trim() === text);
      if (!duplicate) doc.notes.push({ at, text, source: chatSource(api.conversationId) });
      total = doc.notes.length;
    }, context);
    return {
      output: [
        {
          type: "text" as const,
          text: duplicate
            ? `这条已经在记忆里了（${scope}，共 ${total} 条）`
            : scope === "global" && text.length > INDEX_LIMIT
              ? `已记住（global，共 ${total} 条），但这条 ${String(text.length)} 字偏长：全局是索引不是档案，细节建议改存 work，global 只留一句带关键词的索引。`
              : `已记住（${scope}，共 ${total} 条）：${text}`,
        },
      ],
    };
  },
});

/** Manual chat-memory editing from Feishu: `/memo add …` writes what the model will see. */
export async function chatNotes(harness: Harness, context: Context, conversationId: number): Promise<Note[]> {
  const doc = await harness.snapshot(ChatMemory, conversationId as never, context).catch(() => undefined);
  return doc?.notes ?? [];
}

export async function addChatNote(
  harness: Harness,
  context: Context,
  conversationId: number,
  text: string,
): Promise<{ added: boolean; total: number }> {
  const trimmed = text.trim();
  if (trimmed.length === 0) return { added: false, total: (await chatNotes(harness, context, conversationId)).length };
  let added = false;
  let total = 0;
  await harness.commit(async (tx) => {
    const doc = await tx.doc(ChatMemory, conversationId as never);
    if (doc.notes.some((note) => note.text.trim() === trimmed)) {
      total = doc.notes.length;
      return;
    }
    doc.notes.push({ at: new Date().toISOString(), text: trimmed, source: "feishu" });
    total = doc.notes.length;
    added = true;
  }, context);
  return { added, total };
}

export async function removeChatNote(
  harness: Harness,
  context: Context,
  conversationId: number,
  index: number,
): Promise<{ removed?: string; total: number }> {
  let removed: string | undefined;
  let total = 0;
  await harness.commit(async (tx) => {
    const doc = await tx.doc(ChatMemory, conversationId as never);
    total = doc.notes.length;
    if (index < 1 || index > doc.notes.length) return;
    removed = doc.notes[index - 1]?.text;
    doc.notes.splice(index - 1, 1);
    total = doc.notes.length;
  }, context);
  return { removed, total };
}

export async function clearChatNotes(harness: Harness, context: Context, conversationId: number): Promise<number> {
  let removed = 0;
  await harness.commit(async (tx) => {
    const doc = await tx.doc(ChatMemory, conversationId as never);
    removed = doc.notes.length;
    doc.notes = [];
  }, context);
  return removed;
}

const forget = defineTool({
  name: "forget",
  description:
    "删掉一条记忆，按内容关键词匹配。全局、本对话、工作都会搜；共享群里只动本群的记忆。",
  parameters: obj({ keyword: str({ description: "要删除内容里的关键词" }) }),
  execute: async (args, api, context) => {
    const removed: string[] = [];
    // Same rule as remember: an isolated conversation owns its notes and nothing else.
    const policy = await api.snapshot(ChatPolicy, api.conversationId, context).catch(() => undefined);
    const isolated = isIsolatedPolicy(policy);
    await api.commit(async (tx) => {
      const chat = await tx.doc(ChatMemory, api.conversationId);
      const docs = isolated
        ? [chat]
        : [await tx.doc(GlobalMemory), chat, await tx.doc(WorkMemory)];
      for (const doc of docs) {
        const kept = doc.notes.filter((note) => !note.text.includes(args.keyword));
        for (const note of doc.notes) {
          if (note.text.includes(args.keyword)) removed.push(note.text);
        }
        doc.notes.splice(0, doc.notes.length, ...kept);
      }
    }, context);
    return {
      output: [
        {
          type: "text" as const,
          text: removed.length === 0 ? "没有匹配的记忆" : `删除了 ${removed.length} 条：\n- ${removed.join("\n- ")}`,
        },
      ],
    };
  },
});

/**
 * Deleted notes stay dead: transcripts still contain the text, so a curator or a
 * model could otherwise resurrect something the owner removed on purpose.
 */
export const MemoryTombstones = defineDoc<{ texts: string[] }>({
  kind: "app.memory-tombstones",
  version: 1,
  scope: "session",
  initial: () => ({ texts: [] }),
  checkpointWhen: () => false,
});

export const recall = defineTool({
  name: "recall",
  description:
    "在长期记忆里搜索（全局/本对话/工作三档）。多个关键词用空格分开，全词命中的排在前面；一条都没全词命中时会退一步列部分命中的，并标出命中几个词。记忆很多、prompt 里只显示最近若干条时用它找旧信息。",
  parameters: obj({ query: str({ description: "关键词，空格分隔" }) }),
  replay: "safe",
  execute: async (args, api, context) => {
    const terms = args.query
      .split(/\s+/)
      .map((term) => term.trim())
      .filter((term) => term.length > 0);
    const global = await api.snapshot(GlobalMemory, context);
    const chat = await api.snapshot(ChatMemory, api.conversationId, context);
    const work = await api.snapshot(WorkMemory, context);
    // An isolated conversation may search its own notes only: the owner's memories are not
    // injected there, so letting recall read them would reopen the same door.
    const policy = await api.snapshot(ChatPolicy, api.conversationId, context).catch(() => undefined);
    const isolated = isIsolatedPolicy(policy);
    const hits: string[] = [];
    const searched = isolated
      ? ([['本对话', chat?.notes ?? []]] as const)
      : ([
          ['全局', global?.notes ?? []],
          ['本对话', chat?.notes ?? []],
          ['工作', work?.notes ?? []],
        ] as const);
    if (terms.length === 0) {
      return { output: [{ type: "text" as const, text: "recall 需要一个关键词" }] };
    }
    const lowered = terms.map((term) => term.toLowerCase());
    const rows: Array<{ label: string; note: Note }> = [];
    for (const [label, notes] of searched) for (const note of notes) rows.push({ label, note });
    // One line of BM25's idea: a word nearly every note contains (缓存) says little about which
    // note you want, a rare one (ghostty) says a lot. Only ranks partial hits — exact ones keep
    // their order — so a note sharing the rare terms comes first.
    const seen = new Map<string, number>();
    for (const { note } of rows) {
      const hay = note.text.toLowerCase();
      for (const term of lowered) if (hay.includes(term)) seen.set(term, (seen.get(term) ?? 0) + 1);
    }
    const rarity = (term: string) => Math.log(1 + rows.length / (seen.get(term) ?? 1));
    const partial: Array<{ line: string; hits: number; weight: number }> = [];
    for (const { label, note } of rows) {
      const hay = note.text.toLowerCase();
      const matched = lowered.filter((term) => hay.includes(term));
      if (matched.length === 0) continue;
      const line = `- [${label}] ${note.at.slice(0, 10)} ${note.text}`;
      if (matched.length === terms.length) hits.push(line);
      else {
        const weight = matched.reduce((sum, term) => sum + rarity(term), 0);
        partial.push({ line: `${line}（命中 ${matched.length}/${terms.length} 词）`, hits: matched.length, weight });
      }
    }
    // Exact hits are the answer. Only when nothing matched every term do the partial ones show up,
    // as leads with their score on the line.
    if (hits.length === 0 && partial.length > 0) {
      partial.sort((a, b) => b.weight - a.weight || b.hits - a.hits);
      const shown = partial.slice(0, MAX_PARTIAL_HITS);
      const more = partial.length - shown.length;
      hits.push("没有全词命中的记忆，以下是部分命中的：", ...shown.map((entry) => entry.line));
      if (more > 0) hits.push(`（另有 ${more} 条部分命中，换个更短的词能更准）`);
    }
    return {
      output: [
        {
          type: "text" as const,
          text: hits.length === 0 ? `没有匹配「${args.query}」的记忆` : hits.join("\n"),
        },
      ],
    };
  },
});

function renderNotes(notes: Note[] | undefined, limit: number): string | undefined {
  if (notes === undefined || notes.length === 0) return undefined;
  const hidden = Math.max(0, notes.length - limit);
  const lines = notes
    .slice(-limit)
    .map((note) => `- ${note.at.slice(0, 10)} ${note.text}`);
  if (hidden > 0) lines.unshift(`（还有 ${hidden} 条更早的，用 recall 搜索）`);
  return lines.join("\n");
}

/** Memory sections + tools, ready for the personal extension. */
const memorySections = [
  section("global_memory", async (input, context) => {
    // Isolated conversations (shared groups) must never see the owner's memory.
    const policy = await input.read.snapshot(ChatPolicy, input.conversationId, context).catch(() => undefined);
    if (isIsolatedPolicy(policy)) return undefined;
    const doc = await input.read.snapshot(GlobalMemory, context);
    return renderNotes(doc?.notes, MAX_GLOBAL_NOTES);
  }),
  section("work_memory_index", async (input, context) => {
    const doc = await input.read.snapshot(WorkMemory, context);
    const count = doc?.notes.length ?? 0;
    if (count === 0) return undefined;
    return `另有 ${count} 条工作记忆（项目/环境细节），不在上下文里，需要时用 recall 关键词搜索。`;
  }),
  section("chat_memory", async (input, context) => {
    const doc = await input.read.snapshot(ChatMemory, input.conversationId, context);
    return renderNotes(doc?.notes, MAX_CHAT_NOTES);
  }),
];

export const memoryTools = [remember, forget, recall];

export const memoryExtension = defineExtension({
  name: "memory",
  sections: memorySections,
  tools: memoryTools,
});
