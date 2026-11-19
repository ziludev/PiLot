import { defineDoc, type Conversation, type EntryId, type Harness } from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";

/**
 * message id -> the transcript entry it produced. Needed to fork a reply at the
 * exact point it answers, and to survive restarts (a thread keeps its parent).
 */
const MessageIndex = defineDoc<{
  messages: Record<string, { conversationId: number; entryId: number; at: number }>;
}>({
  kind: "app.message-index",
  version: 1,
  scope: "session",
  initial: () => ({ messages: {} }),
  checkpointWhen: () => false,
});

/** thread key -> its forked conversation, plus where the fork came from. */
const ThreadMap = defineDoc<{
  threads: Record<
    string,
    { conversationId: number; parentConversationId?: number; surfaceId?: string; title?: string; at: number }
  >;
}>({
  kind: "app.threads",
  version: 1,
  scope: "session",
  initial: () => ({ threads: {} }),
  checkpointWhen: () => false,
});

const MAX_ENTRIES = 500;

export async function recordMessage(
  harness: Harness,
  context: Context,
  messageId: string,
  conversationId: number,
  entryId: number,
): Promise<void> {
  await harness.commit(async (tx) => {
    const doc = await tx.doc(MessageIndex);
    doc.messages[messageId] = { conversationId, entryId, at: Date.now() };
    const keys = Object.keys(doc.messages);
    if (keys.length > MAX_ENTRIES) {
      for (const key of keys.slice(0, keys.length - MAX_ENTRIES)) delete doc.messages[key];
    }
  }, context);
}

export async function lookupMessage(
  harness: Harness,
  context: Context,
  messageId: string,
): Promise<{ conversationId: number; entryId: number } | undefined> {
  const doc = await harness.snapshot(MessageIndex, context);
  const hit = doc?.messages[messageId];
  return hit === undefined ? undefined : { conversationId: hit.conversationId, entryId: hit.entryId };
}

export async function recordThread(
  harness: Harness,
  context: Context,
  threadKey: string,
  conversationId: number,
  parentConversationId?: number,
  surfaceId?: string,
  title?: string,
): Promise<void> {
  await harness.commit(async (tx) => {
    const doc = await tx.doc(ThreadMap);
    // A fork's opening message is the topic's name; later turns must not rename it.
    const previous = doc.threads[threadKey];
    doc.threads[threadKey] = {
      conversationId,
      parentConversationId: parentConversationId ?? previous?.parentConversationId,
      surfaceId: surfaceId ?? previous?.surfaceId,
      title: title ?? previous?.title,
      at: Date.now(),
    };
    const keys = Object.keys(doc.threads);
    if (keys.length > MAX_ENTRIES) {
      for (const key of keys.slice(0, keys.length - MAX_ENTRIES)) delete doc.threads[key];
    }
  }, context);
}

/** conversationId -> the thread it belongs to, for grouping topics under their chat. */
export type ThreadInfo = { key: string; parent?: number; surfaceId?: string; title?: string };

export async function threadIndex(harness: Harness, context: Context): Promise<Record<string, ThreadInfo>> {
  const doc = await harness.snapshot(ThreadMap, context);
  const index: Record<string, ThreadInfo> = {};
  for (const [key, value] of Object.entries(doc?.threads ?? {})) {
    index[String(value.conversationId)] = {
      key,
      parent: value.parentConversationId,
      surfaceId: value.surfaceId,
      title: value.title,
    };
  }
  return index;
}

/** The fork behind a thread key, and the name it was given when it forked (if any). */
export async function lookupThread(
  harness: Harness,
  context: Context,
  threadKey: string,
): Promise<{ conversationId: number; title?: string } | undefined> {
  const doc = await harness.snapshot(ThreadMap, context);
  const found = doc?.threads[threadKey];
  return found === undefined ? undefined : { conversationId: found.conversationId, title: found.title };
}

/**
 * A topic is named by the message that opened it, so a rail row reads like the
 * question rather than like `话题 8f1a4d`. One line, no mention, cut short.
 */
export function topicTitle(text: string): string {
  return text.replace(/\s+/g, " ").replace(/^@\s*/, "").trim().slice(0, 32);
}

/**
 * Fork a conversation at one entry. The fork sees the parent's history up to that
 * point without copying it, and runs independently from then on.
 */
export async function forkAt(
  parent: Conversation,
  entryId: number,
  agent: Record<string, unknown>,
  context: Context,
): Promise<Conversation> {
  return parent.fork(entryId as unknown as EntryId, { ownership: { kind: "ownerless" }, agent }, context);
}
