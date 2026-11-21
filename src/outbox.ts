/**
 * Outbox: a message that cannot be delivered right now is kept, not dropped.
 *
 * Delivery fails for reasons that pass — the gateway restarts, a request times out, Feishu refuses
 * a body for a moment. Before this, the fallback chain in `deliverAnswer` ended at the log: the
 * answer existed only in `logs/agent.log`, and a routine's notice was simply gone. Holding the text
 * and retrying also keeps a chat in order: if one message is stuck, later ones for the same chat
 * wait behind it instead of arriving first.
 *
 * The document is session-scoped, so it lands in sqlite and a restart resumes the retries.
 */
import { defineDoc, type Harness } from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";

export type OutboxEntry = {
  id: string;
  chatId: string;
  /** Reply to this message when it is known; otherwise post into the chat. */
  replyTo?: string;
  thread?: boolean;
  /** Card body, or plain text when `plain` is true. */
  text: string;
  plain: boolean;
  meta?: string;
  header?: { title: string; template: "green" | "orange" | "grey" | "red" | "blue" };
  /** Held on purpose (chat in `observe` mode): not due, not blocking, waits for `/outbox flush`. */
  paused?: boolean;
  createdAt: number;
  attempts: number;
  nextAt: number;
  lastError?: string;
};

export const Outbox = defineDoc<{ entries: OutboxEntry[] }>({
  kind: "app.outbox",
  version: 1,
  scope: "session",
  initial: () => ({ entries: [] }),
  checkpointWhen: () => false,
});

/** Eight tries over a day, then the entry stays as a record instead of being retried forever. */
const MAX_ATTEMPTS = 8;
const MAX_AGE_MS = 24 * 3_600_000;
/** Enough for a bad hour; a longer backlog than this is a bug, not a queue. */
const MAX_ENTRIES = 50;

/** Waiting gets longer after each failure: a gateway that is down will not be up in 15s. */
export function retryDelay(attempts: number): number {
  const steps = [15_000, 60_000, 5 * 60_000, 15 * 60_000, 60 * 60_000];
  return steps[Math.min(Math.max(0, attempts), steps.length - 1)]!;
}

export type HeldMessage = Omit<OutboxEntry, "id" | "createdAt" | "attempts" | "nextAt" | "lastError">;

/** Puts a message in line. Never throws: a full disk must not turn a failed send into a failed turn. */
export async function hold(harness: Harness, context: Context, message: HeldMessage): Promise<void> {
  const entry: OutboxEntry = {
    ...message,
    id: `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`,
    createdAt: Date.now(),
    attempts: 0,
    nextAt: 0,
  };
  await harness.commit(async (tx) => {
    const doc = await tx.doc(Outbox);
    doc.entries.push(entry);
    if (doc.entries.length > MAX_ENTRIES) doc.entries.splice(0, doc.entries.length - MAX_ENTRIES);
    return entry.id;
  }, context);
}

export type OutboxReport = {
  sent: number;
  /** Still waiting, or given up on. */
  left: number;
  /** Handed back to the caller because nothing could deliver it at all. */
  gaveUp: OutboxEntry[];
};

/**
 * One attempt per due entry, in arrival order per chat. A chat that fails keeps the rest of its
 * entries behind it — order matters more than throughput when the messages are answers.
 */
export async function drain(
  harness: Harness,
  context: Context,
  send: (entry: OutboxEntry) => Promise<void>,
  now = Date.now(),
): Promise<OutboxReport> {
  const entries = await harness.commit(
    async (tx) => (await tx.doc(Outbox)).entries.map((entry) => ({ ...entry })),
    context,
  );
  if (entries.length === 0) return { sent: 0, left: 0, gaveUp: [] };

  const done = new Set<string>();
  const failed = new Map<string, string>();
  const blocked = new Set<string>();
  for (const entry of entries) {
    if (blocked.has(entry.chatId)) continue;
    // Paused entries are not late, they are held: they must not block the retries behind them.
    if (entry.paused === true) continue;
    if (entry.nextAt > now) {
      // Still cooling down: hold the queue behind it so a later message cannot overtake.
      blocked.add(entry.chatId);
      continue;
    }
    try {
      await send(entry);
      done.add(entry.id);
    } catch (error) {
      blocked.add(entry.chatId);
      failed.set(entry.id, (error as Error)?.message ?? String(error));
    }
  }

  const gaveUp: OutboxEntry[] = [];
  await harness.commit(async (tx) => {
    const doc = await tx.doc(Outbox);
    doc.entries = doc.entries.filter((entry) => {
      if (done.has(entry.id)) return false;
      const error = failed.get(entry.id);
      if (error === undefined) return true;
      // The delay is chosen before the count grows: the first failure waits 15s, not a minute.
      entry.nextAt = now + retryDelay(entry.attempts);
      entry.attempts += 1;
      entry.lastError = error;
      if (entry.attempts >= MAX_ATTEMPTS || now - entry.createdAt > MAX_AGE_MS) {
        gaveUp.push({ ...entry });
        return false;
      }
      return true;
    });
    return doc.entries.length;
  }, context);

  return { sent: done.size, left: entries.length - done.size - gaveUp.length, gaveUp };
}

/** Lets everything `observe` held go out now. Returns how many were released. */
export async function releaseHeld(harness: Harness, context: Context): Promise<number> {
  return harness.commit(async (tx) => {
    const doc = await tx.doc(Outbox);
    let released = 0;
    for (const entry of doc.entries) {
      if (entry.paused !== true) continue;
      delete entry.paused;
      entry.nextAt = 0;
      released += 1;
    }
    return released;
  }, context);
}

/** What is waiting, for `/outbox` and for the health line. */
export async function pending(harness: Harness, context: Context): Promise<OutboxEntry[]> {
  return harness.commit(async (tx) => (await tx.doc(Outbox)).entries.map((entry) => ({ ...entry })), context);
}
