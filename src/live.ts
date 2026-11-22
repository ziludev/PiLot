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
 * What a push buys.
 *
 * The gap after a push is paid for by the size of that push: a short frame may go again almost at
 * once, a large one waits. That is the difference between a card that lags a word behind and one
 * that redraws forty kilobytes of thinking every 300 ms. The ceiling keeps a quiet minute from
 * starving the reader.
 *
 * The floor is the same 300 ms `src/feishu.ts` uses as its paint gate (`PAINT_MIN_MS`), because that
 * gate, not this one, is what Feishu enforces: a push that arrives sooner is folded into the next
 * card frame and buys nothing. Tuning the floor below it would only produce a number this module
 * cannot actually deliver.
 */
const MIN_INTERVAL_MS = 300;
const MAX_INTERVAL_MS = 1500;
const BYTES_PER_SECOND = 32 * 1024;

export type LiveWatchOptions = {
  /** The shortest gap between two pushes, whatever the frame's size. Older name: `intervalMs`. */
  minIntervalMs?: number;
  /** The longest a change may wait, whatever its size. */
  maxIntervalMs?: number;
  /** The pace one push buys: a frame of N bytes keeps the card quiet for N / bytesPerSecond. */
  bytesPerSecond?: number;
};

/** How long a frame of `bytes` keeps the card quiet: its size, floored and capped. */
export function liveQuietMs(bytes: number, options: LiveWatchOptions = {}): number {
  const min = options.minIntervalMs ?? MIN_INTERVAL_MS;
  const max = Math.max(min, options.maxIntervalMs ?? MAX_INTERVAL_MS);
  const bytesPerSecond = options.bytesPerSecond ?? BYTES_PER_SECOND;
  return Math.min(max, Math.max(min, (bytes / bytesPerSecond) * 1000));
}

/**
 * Follows a conversation and reports the live snapshot on change.
 *
 * Two things are deliberately not pushed: a snapshot identical to the last one (the end of a run,
 * a tool round repeating its tail), and nothing at all when the only change would arrive before
 * the floor — the timer carries it instead, so a delta that lands early is never dropped.
 */
export async function watchLive(
  conversation: Conversation,
  onSnapshot: (snapshot: LiveSnapshot) => void,
  options: LiveWatchOptions & { intervalMs?: number } = {},
): Promise<LiveWatcher> {
  const minIntervalMs = options.minIntervalMs ?? options.intervalMs ?? MIN_INTERVAL_MS;
  const maxIntervalMs = Math.max(minIntervalMs, options.maxIntervalMs ?? MAX_INTERVAL_MS);
  const pacing: LiveWatchOptions = { minIntervalMs, maxIntervalMs, bytesPerSecond: options.bytesPerSecond };
  const watch = await conversation.watch(context);
  let latest = readLive(watch.value);
  let lastPush = 0;
  let lastFrame = "";
  let timer: NodeJS.Timeout | undefined;

  const quietFor = (bytes: number): number => liveQuietMs(bytes, pacing);

  const push = (frame: string) => {
    lastPush = Date.now();
    lastFrame = frame;
    onSnapshot(latest);
  };

  // The scheduled wake-up re-measures: the frame it was booked for may have grown since, and a
  // push that no longer fits the floor is booked again rather than spent.
  const flush = () => {
    timer = undefined;
    const frame = JSON.stringify(latest);
    if (frame === lastFrame) return;
    const wait = quietFor(frame.length) - (Date.now() - lastPush);
    if (wait > 0) {
      timer = setTimeout(flush, wait);
      return;
    }
    push(frame);
  };

  push(JSON.stringify(latest));
  watch.start(async (call) => {
    if (call.kind !== "value") return;
    latest = readLive(call.value);
    const frame = JSON.stringify(latest);
    if (frame === lastFrame) return;
    const wait = quietFor(frame.length) - (Date.now() - lastPush);
    if (wait <= 0) {
      if (timer !== undefined) {
        clearTimeout(timer);
        timer = undefined;
      }
      push(frame);
      return;
    }
    if (timer !== undefined) return;
    timer = setTimeout(flush, wait);
  });

  return {
    async stop() {
      if (timer !== undefined) clearTimeout(timer);
      timer = undefined;
      await watch.stop();
    },
  };
}
