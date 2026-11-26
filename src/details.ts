import { defineDoc, type Harness } from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";
import type { DecisionNote } from "./decisions.ts";

export type ToolStep = {
  name: string;
  ok: boolean;
  /**
   * What the call was made with, one line: the command, the path, the pattern. A turn that runs
   * `bash` forty times is unreadable as forty identical rows — this is the only field that tells
   * them apart, and it is what the card and the client show.
   */
  hint?: string;
  /** Short output preview so the owner can see what actually happened. */
  preview: string;
};

const MAX_HINT = 60;

/**
 * The line that tells two `bash` steps apart: what the call was actually given. It is read off the
 * arguments, which arrive as a JSON string, and cut to one line — the card shows it inside inline
 * code, where a newline would break the row and an essay would bury the next step.
 */
export function stepHint(name: string, args: unknown): string {
  const given = asRecord(args);
  if (given === undefined) return "";
  const keys = keysFor(name);
  for (const key of keys) {
    const value = given[key];
    if (typeof value === "string" && value.trim().length > 0) return oneLine(value);
  }
  const first = keys.find((key) => typeof given[key] === "number");
  return first === undefined ? "" : String(given[first]);
}

function asRecord(args: unknown): Record<string, unknown> | undefined {
  if (typeof args === "string") {
    try {
      const parsed: unknown = JSON.parse(args);
      return typeof parsed === "object" && parsed !== null ? (parsed as Record<string, unknown>) : undefined;
    } catch {
      return undefined;
    }
  }
  return typeof args === "object" && args !== null ? (args as Record<string, unknown>) : undefined;
}

/** Which argument answers "what did it do" differs per tool: a command, a pattern, a path. */
function keysFor(name: string): string[] {
  const base = name.toLowerCase();
  if (/(bash|shell|exec|run|terminal)/.test(base)) return ["command", "cmd", "script", "args", "path"];
  if (/(grep|search|find|glob)/.test(base)) return ["pattern", "query", "glob", "path"];
  if (/(write|edit|patch|apply)/.test(base)) return ["path", "file_path", "filePath", "file"];
  return ["command", "path", "file_path", "filePath", "query", "pattern", "url", "name", "id", "prompt", "summary"];
}

function oneLine(value: string): string {
  const squeezed = value.replace(/\s+/g, " ").trim();
  return squeezed.length > MAX_HINT ? `${squeezed.slice(0, MAX_HINT - 1)}…` : squeezed;
}

export type TurnDetails = {
  steps: ToolStep[];
  seconds: number;
  costUsd: number;
  /** Approvals asked and why the turn ended, when either happened. */
  decisions?: DecisionNote[];
  /** Full user prompt that triggered the turn, shown inside the folded detail panel. */
  prompt?: string;
  /** Model + thinking level that ran this turn (e.g. `deepseek-flash (high)`). */
  model?: string;
  at: number;
};

/**
 * Per-message record of what the agent actually did. The answer card carries the folded panel;
 * this is what the client and cards sent before that read back.
 */
const TurnDetailsDoc = defineDoc<{ turns: Record<string, TurnDetails> }>({
  kind: "app.turn-details",
  version: 1,
  scope: "session",
  initial: () => ({ turns: {} }),
  checkpointWhen: () => false,
});

const MAX_TURNS = 200;

export async function saveTurnDetails(
  harness: Harness,
  context: Context,
  messageId: string,
  details: Omit<TurnDetails, "at">,
): Promise<void> {
  await harness.commit(async (tx) => {
    const doc = await tx.doc(TurnDetailsDoc);
    doc.turns[messageId] = { ...details, at: Date.now() };
    const keys = Object.keys(doc.turns);
    if (keys.length > MAX_TURNS) {
      for (const key of keys.slice(0, keys.length - MAX_TURNS)) delete doc.turns[key];
    }
  }, context);
}

export async function readTurnDetails(
  harness: Harness,
  context: Context,
  messageId: string,
): Promise<TurnDetails | undefined> {
  const doc = await harness.snapshot(TurnDetailsDoc, context);
  return doc?.turns[messageId];
}
