import { defineDoc, type DocumentReader, type Harness } from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";
import { config } from "./config.ts";
import { ConversationProjectDoc, ProjectsDoc } from "./projects.ts";
import { log } from "./log.ts";
import { record } from "./audit.ts";

/**
 * One state decides who may use a Feishu chat and at what level.
 *
 * - `closed` — nobody; a chat the bot was added to, or a person it has never met.
 * - `owner`  — only the owner. A chat listed in `.env`, and the owner's own DM.
 * - `group`  — everyone in it, with the same tool surface as the owner and the group's own memory.
 *
 * Gate, isolation and `/allow` all read this one value. Before, three docs (`.env` chat list,
 * `app.allowed-chats`, `app.guest-chats`) and a derived rule answered the same question, and
 * which one won depended on where you looked.
 */
export type ChatLevel = "closed" | "owner" | "group";

const ChatAccess = defineDoc<{ chats: Record<string, ChatLevel> }>({
  kind: "app.chat-access",
  version: 1,
  scope: "session",
  initial: () => ({ chats: {} }),
  checkpointWhen: () => false,
});

/** The level for one chat. `.env` is the fallback for chats this process has not stored yet. */
export function levelOf(chatId: string, access: Record<string, ChatLevel>, envChatIds: readonly string[] = []): ChatLevel {
  const stored = access[chatId];
  if (stored !== undefined) return stored;
  return envChatIds.includes(chatId) ? "owner" : "closed";
}

/**
 * What a tool may read: the agent's own workspace, plus the repository this conversation is
 * about. Without the project root a question like "why does this check fail" could not read the
 * code it asks about — the chats bound to ~/work/example-service were unreadable.
 *
 * One answer for "where am I allowed to look", used by `project_search`.
 */
export async function readRoots(
  read: DocumentReader,
  conversationId: number,
  context: Context,
): Promise<string[]> {
  const roots = [config.workspace];
  const link = await read.snapshot(ConversationProjectDoc, context).catch(() => undefined);
  const projectId = link?.byConversation?.[String(conversationId)];
  if (projectId === undefined) return roots;
  const projects = (await read.snapshot(ProjectsDoc, context).catch(() => undefined))?.projects ?? [];
  const project = projects.find((candidate) => candidate.id === projectId);
  if (project !== undefined) roots.push(project.path);
  return roots;
}

export async function chatAccess(harness: Harness, context: Context): Promise<Record<string, ChatLevel>> {
  return (await harness.snapshot(ChatAccess, context).catch(() => undefined))?.chats ?? {};
}

export async function setChatLevel(
  harness: Harness,
  context: Context,
  chatId: string,
  level: ChatLevel,
): Promise<void> {
  await harness.commit(async (tx) => {
    (await tx.doc(ChatAccess)).chats[chatId] = level;
  }, context);
  log("access", "chat level set", { chatId, level });
  await record(harness, context, { kind: "access", chat: chatId, level, detail: "level set" });
}

/** Docs from before the single state; read once, then nothing writes them again. */
const LegacyGuestChats = defineDoc<{ ids: string[] }>({
  kind: "app.guest-chats",
  version: 1,
  scope: "session",
  initial: () => ({ ids: [] }),
  checkpointWhen: () => false,
});

const LegacyAllowedChats = defineDoc<{ extra: string[] }>({
  kind: "app.allowed-chats",
  version: 1,
  scope: "session",
  initial: () => ({ extra: [] }),
  checkpointWhen: () => false,
});

/**
 * Runs once at bridge start: imports the chats the previous model stored, and the chats
 * configured in `.env`, so an upgrade never silently closes a group the owner opened.
 */
export async function adoptChatLevels(
  harness: Harness,
  context: Context,
  envChatIds: readonly string[],
): Promise<Record<string, ChatLevel>> {
  const current = await chatAccess(harness, context);
  const next: Record<string, ChatLevel> = { ...current };
  const groups: string[] = [];
  const owners: string[] = [];
  const adopt = (chatId: string, level: ChatLevel, changed: string[]): void => {
    if (next[chatId] !== undefined) return;
    next[chatId] = level;
    changed.push(chatId);
  };

  // `/allow` and `/open` both opened a group before; the two legacy lists end up as groups.
  const legacyGuests = (await harness.snapshot(LegacyGuestChats, context).catch(() => undefined))?.ids ?? [];
  const legacyAllowed = (await harness.snapshot(LegacyAllowedChats, context).catch(() => undefined))?.extra ?? [];
  for (const chatId of [...legacyGuests, ...legacyAllowed]) adopt(chatId, "group", groups);
  for (const chatId of envChatIds) adopt(chatId, "owner", owners);

  if (groups.length > 0 || owners.length > 0) {
    await harness.commit(async (tx) => {
      const doc = await tx.doc(ChatAccess);
      for (const chatId of [...groups, ...owners]) {
        if (doc.chats[chatId] === undefined) doc.chats[chatId] = next[chatId] as ChatLevel;
      }
    }, context);
    log("access", "adopted chat levels", { groups, owners });
    // One line, not one per chat: the interesting fact is that an upgrade opened these at all.
    await record(harness, context, {
      kind: "access",
      detail: `adopted ${groups.length} group(s) and ${owners.length} owner chat(s)`,
    });
  }
  return next;
}

/**
 * Per-conversation policy.
 *
 * `isolated` means this conversation runs on its own chat memory: nothing it learns is written
 * to the shared memories the owner's chats read. It is a memory boundary, not a permission —
 * a group member gets the same tools as the owner.
 *
 * `guest` is the same flag under its old name, read for docs written before the rename;
 * `isIsolatedPolicy` is the only place that looks at it, and nothing writes it any more.
 * `turn` used to be `"guest"`; only `"owner"` is ever matched, so an old value reads as a
 * member turn without a migration.
 */
/** How much this chat wants to hear from the agent without having asked anything. */
export type ProactiveMode = "off" | "observe" | "auto";

export const ChatPolicy = defineDoc<{
  isolated?: boolean;
  /** Legacy spelling of `isolated`; read-only. */
  guest?: boolean;
  topic?: string;
  turn?: "owner" | "member";
  /** Absent reads as `auto`: the behaviour before this switch existed. */
  proactive?: ProactiveMode;
}>({
  kind: "app.chat-policy",
  version: 1,
  scope: "conversation",
  history: "latest",
  fork: "current",
  initial: () => ({ isolated: false }),
  checkpointWhen: () => false,
});

/**
 * The one compatibility read: new docs say `isolated`, docs written before the rename say
 * `guest`. A predicate, so callers that also read `policy.turn` get a non-undefined value.
 */
export function isIsolatedPolicy<T extends { isolated?: boolean; guest?: boolean }>(
  policy: T | undefined,
): policy is T {
  return policy !== undefined && (policy.isolated === true || policy.guest === true);
}

export async function setIsolatedPolicy(
  harness: Harness,
  context: Context,
  conversationId: number,
): Promise<void> {
  await harness.commit(async (tx) => {
    const doc = await tx.doc(ChatPolicy, conversationId as never);
    doc.isolated = true;
    // Drop the old spelling on write: one flag, one name, and a migrated doc reads the same.
    delete doc.guest;
  }, context);
}

/**
 * Marks whose turn the conversation is about to run. The tool surface no longer depends on it —
 * the audit trail and the memory rule still do. Absent reads as a member turn.
 */
export async function setChatTurn(
  harness: Harness,
  context: Context,
  conversationId: number,
  turn: "owner" | "member",
): Promise<void> {
  await harness.commit(async (tx) => {
    const doc = await tx.doc(ChatPolicy, conversationId as never);
    doc.turn = turn;
  }, context);
}

/** The /open rule for one conversation, re-applied as instructions on every turn. */
export async function setChatTopic(
  harness: Harness,
  context: Context,
  conversationId: number,
  topic: string | undefined,
): Promise<void> {
  await harness.commit(async (tx) => {
    const doc = await tx.doc(ChatPolicy, conversationId as never);
    if (topic === undefined) delete doc.topic;
    else doc.topic = topic;
  }, context);
}

/**
 * How loud this conversation's unprompted messages are allowed to be.
 *
 * `off` and `observe` both keep quiet; the difference is what happens to the message. `off` drops it
 * (a deliberate "do not tell me"), `observe` keeps it in the outbox with `paused`, where `/outbox`
 * can show it and `/outbox flush` can send it. Absent reads as `auto`: the behaviour before this
 * switch existed, so nothing changes until someone turns it down.
 */
export async function proactiveOf(
  harness: Harness,
  context: Context,
  conversationId: number,
): Promise<ProactiveMode> {
  const mode = await harness.commit(
    async (tx) => (await tx.doc(ChatPolicy, conversationId as never)).proactive,
    context,
  );
  return mode === "off" || mode === "observe" ? mode : "auto";
}

export async function setProactive(
  harness: Harness,
  context: Context,
  conversationId: number,
  mode: ProactiveMode,
): Promise<void> {
  await harness.commit(async (tx) => {
    const doc = await tx.doc(ChatPolicy, conversationId as never);
    // `auto` is the default: a default spelled out in every document is one more copy to keep in
    // sync, so writing it means deleting it.
    if (mode === "auto") delete doc.proactive;
    else doc.proactive = mode;
  }, context);
}

export async function chatTopicOf(
  harness: Harness,
  context: Context,
  conversationId: number,
): Promise<string | undefined> {
  const policy = await harness.snapshot(ChatPolicy, conversationId as never, context).catch(() => undefined);
  return policy?.topic;
}

/**
 * The owner's own p2p chat, learned from the first message they send. Needed to report
 * strangers: their chat id is new to us, so it cannot be the address we send to.
 */
export const OwnerChat = defineDoc<{ chatId?: string }>({
  kind: "app.owner-chat",
  version: 1,
  scope: "session",
  initial: () => ({}),
  checkpointWhen: () => false,
});

let cachedOwnerChat: string | undefined;

export async function ownerChatId(harness: Harness, context: Context): Promise<string | undefined> {
  if (cachedOwnerChat !== undefined) return cachedOwnerChat;
  const doc = await harness.snapshot(OwnerChat, context);
  cachedOwnerChat = doc?.chatId;
  return cachedOwnerChat;
}

/**
 * Where a finished background task reports.
 *
 * The work was asked for in a chat, but the report is not chat material: it names branches, file
 * paths and what the reviewer found, and a group timeline is the wrong shelf for it — the owner
 * said so after one 「🧩 需求做完了」 landed in the middle of a conversation. A chat id that is not
 * the owner's own DM is therefore redirected to their DM. Anything that is not a Feishu chat at
 * all (the desktop client, the CLI) keeps its own surface: those notifiers know where to draw.
 */
export function pickReportSurface(origin: string, ownerChat: string | undefined): string {
  if (!origin.startsWith("oc_")) return origin;
  if (ownerChat === undefined || ownerChat.length === 0) return origin;
  return ownerChat;
}

export async function reportSurface(
  reader: DocumentReader,
  context: Context,
  origin: string,
): Promise<string> {
  // A background task holds the document reader, not the harness: same doc, no cache to warm.
  const owner = await reader.snapshot(OwnerChat, context).catch(() => undefined);
  return pickReportSurface(origin, owner?.chatId);
}

export async function rememberOwnerChat(harness: Harness, context: Context, chatId: string): Promise<void> {
  if (cachedOwnerChat === chatId) return;
  cachedOwnerChat = chatId;
  await harness.commit(async (tx) => {
    (await tx.doc(OwnerChat)).chatId = chatId;
  }, context);
  log("access", "owner chat recorded", { chatId });
}
