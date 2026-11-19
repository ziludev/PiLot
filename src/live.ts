import type { Conversation } from "@earendil-works/pi-durable";
import { context } from "./harness.ts";

export type LiveSnapshot = {
  /** Text committed so far of the answer being streamed. */
  text: string;
  /** Human-readable label of the tool round currently running. */
  tools: string[];
  /** Tail of the running tool's output: this is what makes progress visible. */
  activity?: string;
  /** Identities of the calls in this round: two `bash` rounds in a row differ here, not by name. */
  callIds?: string[];
  /** What the model is thinking right now, as it writes it: the card's window into the why. */
  thinking?: string;
  /** Tool rounds the run has taken so far, as the engine counts them. */
  turns?: number;
  busy: boolean;
};

type Json = Record<string, unknown>;

function asRecord(value: unknown): Json | undefined {
  return typeof value === "object" && value !== null ? (value as Json) : undefined;
}

function textOfMessage(message: unknown): string {
  return partsOfMessage(message, "text", "text");
}

/**
 * The model's thinking, which arrives as its own content block before the answer does. A provider
 * that withholds it sends `redacted_thinking` instead, which has no words to show — those stay empty
 * rather than turning into a placeholder that claims a thought happened.
 */
function thinkingOfMessage(message: unknown): string {
  return partsOfMessage(message, "thinking", "thinking");
}

function partsOfMessage(message: unknown, type: string, key: string): string {
  const record = asRecord(message);
  const content = record?.["content"];
  if (!Array.isArray(content)) return "";
  let text = "";
  for (const part of content) {
    const item = asRecord(part);
    if (item?.["type"] === type && typeof item[key] === "string") text += item[key] as string;
  }
  return text;
}

/** Reads `pi.live` out of a conversation view. */
function readLive(view: unknown): LiveSnapshot {
  const docs = asRecord(asRecord(view)?.["docs"]);
  const live = asRecord(docs?.["pi.live"]);
  if (live === undefined) return { text: "", tools: [], busy: false };

  const generation = asRecord(live["generation"]);
  const text = textOfMessage(generation?.["message"]);
  const thinking = thinkingOfMessage(generation?.["message"]);

  const tools: string[] = [];
  const callIds: string[] = [];
  let activity: string | undefined;
  const slots = live["tools"];
  if (Array.isArray(slots)) {
    for (const slot of slots) {
      const record = asRecord(slot);
      const name = record?.["name"];
      if (typeof name !== "string") continue;
      tools.push(name);
      const callId = record?.["callId"];
      if (typeof callId === "string") callIds.push(callId);
      if (record?.["status"] !== "running") continue;
      const output = record?.["output"];
      if (typeof output !== "string") continue;
      const tail = output
        .split("\n")
        .map((line) => line.trim())
        .filter((line) => line.length > 0)
        .pop();
      if (tail !== undefined) activity = tail.slice(0, 120);
    }
  }

  const run = asRecord(live["run"]);
  const turns = run?.["turns"];
  return {
    text,
    tools,
    callIds,
    ...(thinking.length === 0 ? {} : { thinking }),
    ...(typeof turns === "number" ? { turns } : {}),
    activity,
    busy: run !== undefined,
  };
}

/**
 * Whether a run is in flight right now — one read, no subscription.
 *
 * The difference between "this message joins what is running" and "this message starts something"
 * is the whole point of steering, and it has to be decided before submitting, not after.
 */
export async function runInFlight(conversation: Conversation): Promise<boolean> {
  const watch = await conversation.watch(context);
  const busy = readLive(watch.value).busy;
  await watch.stop();
  return busy;
}

type LiveWatcher = { stop(): Promise<void> };

/**
 * Follows a conversation and reports the live snapshot on change.
 * Throttled: Feishu card updates are rate limited, so we push at most every `intervalMs`.
 */
export async function watchLive(
  conversation: Conversation,
  onSnapshot: (snapshot: LiveSnapshot) => void,
  options: { intervalMs?: number } = {},
): Promise<LiveWatcher> {
  const intervalMs = options.intervalMs ?? 1500;
  const watch = await conversation.watch(context);
  let latest = readLive(watch.value);
  let lastPush = 0;
  let timer: NodeJS.Timeout | undefined;

  const push = () => {
    lastPush = Date.now();
    onSnapshot(latest);
  };

  push();
  watch.start(async (call) => {
    if (call.kind !== "value") return;
    latest = readLive(call.value);
    const elapsed = Date.now() - lastPush;
    if (elapsed >= intervalMs) {
      if (timer !== undefined) {
        clearTimeout(timer);
        timer = undefined;
      }
      push();
      return;
    }
    if (timer !== undefined) return;
    timer = setTimeout(() => {
      timer = undefined;
      push();
    }, intervalMs - elapsed);
  });

  return {
    async stop() {
      if (timer !== undefined) clearTimeout(timer);
      timer = undefined;
      await watch.stop();
    },
  };
}
