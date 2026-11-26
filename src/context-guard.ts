import { defineExtension, hook, GenerationTask } from "@earendil-works/pi-durable";
import { createHash } from "node:crypto";
import type { Message } from "@earendil-works/pi-ai";
import { log } from "./log.ts";
import { messageTokens } from "./tokens.ts";

/**
 * Keep a long run's context from filling with tool output nobody reads again.
 *
 * The real failures here were 40-turn diagnoses where the same log query and the same `git show`
 * came back several times: every result stayed in the request until compaction, so each later
 * request carried all of them. This does not summarise or rewrite content — it replaces the *older*
 * tool outputs with one line saying where they went, and only once a request is already large, so
 * an ordinary turn is untouched and the model can re-run a tool it still needs.
 */

/** Tool results kept verbatim at the end of the request. */
export const KEEP_RECENT = 12;
/**
 * Below this many tokens of reference material, nothing is touched.
 *
 * This used to be 60k characters, which is a whole compaction cycle late: measured runs spent most
 * of their history re-sending pages and file reads the model had already used. 24k is the point
 * where the reused material costs more than it informs.
 *
 * Counted with `messageTokens`, not the upstream estimate: at 3.5 characters per token a Chinese
 * run only reached this figure at ~84k characters, so the threshold never fired on our own logs.
 */
export const ELIDE_MIN_TOKENS = 24_000;
/** What a trimmed result keeps at each end: enough to recognise the shape, not to re-read it. */
const HEAD_CHARS = 1_500;
const TAIL_CHARS = 1_000;
/**
 * Tools whose output is reference material — read once, quoted from, rarely re-read whole.
 * Edits, writes and reports say what happened in a few lines; trimming those only loses evidence.
 */
export const REFERENCE_TOOLS = new Set([
  "read",
  "bash",
  "logs_query",
  "db_query",
  "project_search",
  "recall",
  "skill",
]);

/**
 * How many messages and tokens the last request carried, tracked per conversation so concurrent
 * turns (and subagents) never overwrite each other's compaction triggers.
 */
const MAX_TRACKED_CONVS = 500;
const statsByConv = new Map<number, { messages: number; tokens: number }>();
const digestByConv = new Map<number, string>();
let latestConvId: number | undefined;

function setBounded<K, V>(map: Map<K, V>, key: K, value: V): void {
  if (map.has(key)) map.delete(key);
  map.set(key, value);
  if (map.size > MAX_TRACKED_CONVS) {
    const oldest = map.keys().next().value;
    if (oldest !== undefined) map.delete(oldest);
  }
}

export function recordRequestStats(conversationId: number, messages: number, tokens: number): void {
  latestConvId = conversationId;
  setBounded(statsByConv, conversationId, { messages, tokens });
}

export function lastRequestMessages(conversationId?: number): number {
  const id = conversationId ?? latestConvId;
  if (id === undefined) return 0;
  return statsByConv.get(id)?.messages ?? 0;
}

/**
 * The estimated size of the last request, in tokens.
 *
 * Message count says nothing about cost on its own — the same 400 messages are 7k tokens in a chat of
 * short replies and 94k in one full of tool output — so the compaction trigger reads this too.
 */
export function lastRequestTokens(conversationId?: number): number {
  const id = conversationId ?? latestConvId;
  if (id === undefined) return 0;
  return statsByConv.get(id)?.tokens ?? 0;
}

/** Tracks per-conversation system prompt digests; returns true only when an existing prefix changed. */
export function noteSystemDigest(conversationId: number, digest: string): boolean {
  const previous = digestByConv.get(conversationId);
  setBounded(digestByConv, conversationId, digest);
  return previous !== undefined && previous !== digest;
}

/** Test hook to reset in-memory request tracking. */
export function forgetContextStats(conversationId?: number): void {
  if (conversationId === undefined) {
    statsByConv.clear();
    digestByConv.clear();
    latestConvId = undefined;
    return;
  }
  statsByConv.delete(conversationId);
  digestByConv.delete(conversationId);
  if (latestConvId === conversationId) latestConvId = undefined;
}

/**
 * One request in eight, write down how big it was.
 *
 * A turn costs its rounds times this number, and nothing else reports it: the usage sampler sees the
 * bill hours later, and compaction only speaks when it actually cuts. Logging every request would
 * drown the log, so this keeps a window-scale sample to check a settings change against.
 */
const LOG_EVERY = 8;
let requests = 0;
let providerRequests = 0;

const OMITTED = (chars: number) => `\n……〔此处省略约 ${Math.round(chars / 100) / 10}k 字符〕……\n`;

/** Head and tail of a long result, which is where the model finds what it was looking for. */
export function trimMiddle(text: string): string {
  if (text.length <= HEAD_CHARS + TAIL_CHARS) return text;
  const cut = text.length - HEAD_CHARS - TAIL_CHARS;
  // Never split a surrogate pair: a lone surrogate makes some providers reject the JSON.
  const headEnd = /[\uD800-\uDBFF]/.test(text.charAt(HEAD_CHARS - 1)) ? HEAD_CHARS - 1 : HEAD_CHARS;
  const tailStart = /[\uDC00-\uDFFF]/.test(text.charAt(text.length - TAIL_CHARS))
    ? text.length - TAIL_CHARS + 1
    : text.length - TAIL_CHARS;
  return `${text.slice(0, headEnd)}${OMITTED(cut)}${text.slice(tailStart)}`;
}

function lengthOf(message: Message): number {
  const content = (message as { content?: unknown }).content;
  if (typeof content === "string") return content.length;
  if (!Array.isArray(content)) return 0;
  return content.reduce((sum, part) => sum + String((part as { text?: string }).text ?? "").length, 0);
}

function trimmed(message: Message): Message {
  const content = (message as { content?: unknown }).content;
  if (typeof content === "string") return { ...message, content: trimMiddle(content) } as Message;
  if (!Array.isArray(content)) return message;
  return {
    ...message,
    content: content.map((part) =>
      typeof (part as { text?: unknown }).text === "string"
        ? { ...part, text: trimMiddle((part as { text: string }).text) }
        : part,
    ),
  } as Message;
}

/** Tools whose result counts toward the trimmed set: reference material only. */
const isReference = (message: Message): boolean =>
  message.role === "toolResult" && REFERENCE_TOOLS.has(String((message as { toolName?: string }).toolName ?? ""));

export function elideOldToolOutputs(
  messages: readonly Message[],
  keep = KEEP_RECENT,
  minTokens = ELIDE_MIN_TOKENS,
): { messages: Message[]; elided: number } {
  const results = messages
    .map((message, index) => ({ message, index }))
    .filter((entry) => isReference(entry.message));
  if (results.length <= keep) return { messages: [...messages], elided: 0 };

  const total = results.reduce((sum, entry) => sum + messageTokens(entry.message), 0);
  if (total < minTokens) return { messages: [...messages], elided: 0 };

  const stale = new Set(results.slice(0, results.length - keep).map((entry) => entry.index));
  let elided = 0;
  const next = messages.map((message, index) => {
    if (!stale.has(index)) return message;
    const after = trimmed(message);
    // A result that was already short (an `ls`, a `git status`) is not worth a placeholder.
    if (lengthOf(after) >= lengthOf(message) * 0.9) return message;
    elided += 1;
    return after;
  });
  return { messages: next, elided };
}

/**
 * Cheap hash of the head and tail of the system prompt: any edit inside it changes one of the two.
 *
 * Which field holds the prompt depends on the provider — Anthropic sends `system`, others open the
 * messages with a system entry, some use `instructions` — so look in all three rather than hashing
 * `undefined` and reporting a prefix that never moves.
 */
export function systemDigest(payload: unknown): string {
  const body = payload as
    | { system?: unknown; instructions?: unknown; messages?: Array<{ role?: string; content?: unknown }> }
    | undefined;
  const system =
    body?.system ?? body?.instructions ?? body?.messages?.find((message) => message.role === "system")?.content ?? "";
  const text = typeof system === "string" ? system : JSON.stringify(system);
  const edges = text.slice(0, 4000) + "\u0000" + text.slice(-4000);
  return createHash("sha1").update(`${text.length}:${edges}`).digest("hex").slice(0, 8);
}

export const contextGuardExtension = defineExtension({
  name: "context-guard",
  hooks: [
    hook(GenerationTask, {
      beforeRequest: (request, api) => {
        const conversationId = api.conversationId as unknown as number;
        const { messages, elided } = elideOldToolOutputs(request.messages);
        const tokens = messages.reduce((sum, message) => sum + messageTokens(message), 0);
        recordRequestStats(conversationId, request.messages.length, tokens);
        requests += 1;
        if (requests % LOG_EVERY === 0) {
          log("context", "request size", { conversationId, messages: messages.length, tokens });
        }
        if (elided === 0) return undefined;
        log("context", "elided old tool output", { conversationId, elided, messages: messages.length });
        return { messages };
      },
      // The messages are only part of what a round costs: the tools and the system prompt are sent
      // with every request too, and they are the part a compaction can never touch. Measure the
      // built body instead of guessing which half to trim.
      beforeProviderRequest: (request, api) => {
        const conversationId = api.conversationId as unknown as number;
        providerRequests += 1;
        const body = request.payload as { messages?: unknown; input?: unknown; tools?: unknown; system?: unknown };
        // A prompt cache is only worth anything if the prefix is byte-identical between requests.
        // Log every change to the system prompt: if this fires on consecutive requests, something
        // per-turn has moved into it and the whole history is being paid for again.
        const digest = systemDigest(body);
        if (noteSystemDigest(conversationId, digest)) {
          log("context", "system prompt changed", { conversationId, digest });
        }
        if (providerRequests % LOG_EVERY !== 0) return undefined;
        const chars = (value: unknown): number => (value === undefined ? 0 : JSON.stringify(value).length);
        log("context", "request bytes", {
          total: chars(request.payload),
          messages: chars(body.messages ?? body.input),
          tools: chars(body.tools),
        });
        return undefined;
      },
    }),
  ],
});
