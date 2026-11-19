/**
 * What a running turn has finished, and which step it is on.
 *
 * The live snapshot shows one tool round at a time, so a card that only renders the snapshot
 * forgets every round that ended: a five-minute job reads "bash · 0s" and nothing else. This keeps
 * the trail instead — one line per finished round, plus the one running now — and it is pure, so
 * the card copy can be read in a test rather than by waiting five minutes.
 */

import type { LiveSnapshot } from "./live.ts";
import { describeCall } from "./progress.ts";

export type Round = { label: string; ms: number; tail?: string };

export type Trail = {
  /** Rounds that ended, oldest first. */
  rounds: Round[];
  /** Identity of the round running now: call ids, so two `bash` rounds in a row stay two. */
  key?: string;
  /** What that round is called on the card. */
  label?: string;
  since: number;
  /** Last activity line seen in this round; the only thing a hanging round says. */
  tail?: string;
  turn?: number;
};

/** Enough history to answer "what did it do" without turning the card into a log. */
const MAX_ROUNDS = 40;

function identity(live: LiveSnapshot): string {
  if (live.callIds !== undefined && live.callIds.length > 0) return live.callIds.join(",");
  return live.tools.join(" · ");
}

/** The round's name on the card: `跑 npm test`, not `bash`. */
function labelOf(live: LiveSnapshot): string {
  return live.tools
    .map((name, index) => describeCall(name, live.args?.[live.callIds?.[index] ?? ""]))
    .join(" · ");
}

/** Folds one snapshot into the trail: a changed identity ends the previous round. */
export function advance(trail: Trail, live: LiveSnapshot, now: number): Trail {
  const key = identity(live);
  const next: Trail = { ...trail, key, label: labelOf(live), turn: live.turns ?? trail.turn };
  if (key !== trail.key) {
    const finished = trail.key !== undefined && trail.key.length > 0 && trail.label !== undefined;
    if (finished) {
      next.rounds = [...trail.rounds, { label: trail.label ?? "", ms: now - trail.since, ...(trail.tail === undefined ? {} : { tail: trail.tail }) }];
      if (next.rounds.length > MAX_ROUNDS) next.rounds = next.rounds.slice(next.rounds.length - MAX_ROUNDS);
    }
    next.since = now;
    delete next.tail;
  }
  if (live.activity !== undefined && live.activity.length > 0) next.tail = live.activity;
  return next;
}

function seconds(ms: number): string {
  const value = Math.max(0, Math.round(ms / 1000));
  if (value < 60) return `${value}s`;
  return `${Math.floor(value / 60)}m${String(value % 60).padStart(2, "0")}s`;
}

/** The trail as card lines: finished rounds ticked, the running one marked, oldest dropped first. */
export function renderSteps(trail: Trail, now: number, options: { limit?: number } = {}): string {
  const limit = options.limit ?? 5;
  const lines: string[] = [];
  const hidden = Math.max(0, trail.rounds.length - limit);
  if (hidden > 0) lines.push(`⋯ 前面还有 ${hidden} 步`);
  for (const round of trail.rounds.slice(hidden)) {
    lines.push(`✓ ${round.label} · ${seconds(round.ms)}${round.tail === undefined ? "" : ` · ${round.tail}`}`);
  }
  if (trail.key !== undefined && trail.key.length > 0 && (trail.label ?? "").length > 0) {
    lines.push(`▸ ${trail.label} · ${seconds(now - trail.since)}`);
  }
  return lines.join("\n");
}

/** How many steps to report: rounds that ended, and whether one is running. */
export function stepCount(trail: Trail): { done: number; running: boolean } {
  return { done: trail.rounds.length, running: trail.key !== undefined && trail.key.length > 0 };
}
