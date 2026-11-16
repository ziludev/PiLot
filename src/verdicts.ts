import { defineDoc, type Harness, type Tx } from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";

/**
 * What the model claimed about each finished turn, kept so a morning can be read back.
 *
 * The claim itself lives in memory for the card (`done.ts`), which is right for one card and
 * useless for "how did last night go". This is the durable half: a bounded ring, written once at
 * settle, read by the digest. One verdict per turn, with the reason the turn ended when there is
 * one — "target missed" and "ran out of steps" are different failures and both belong in the log.
 */
export type Verdict = {
  at: number;
  /** Chat or surface that asked, for "who wanted this". */
  chat?: string;
  met: boolean;
  why: string;
  seconds?: number;
  /** Settle reason when the turn did not finish on its own terms (`turn_limit`, `aborted`, …). */
  reason?: string;
};

/** A week of nights; older verdicts fall off the front. */
export const VERDICT_LIMIT = 100;

export const VerdictDoc = defineDoc<{ verdicts: Verdict[] }>({
  kind: "app.turn-verdicts",
  version: 1,
  scope: "session",
  initial: () => ({ verdicts: [] }),
  checkpointWhen: () => false,
});

/** Pure so the ring behaviour is testable without a runtime. */
export function pushVerdict(verdicts: readonly Verdict[], verdict: Verdict, limit = VERDICT_LIMIT): Verdict[] {
  const next = [...verdicts, verdict];
  return next.length <= limit ? next : next.slice(next.length - limit);
}

export type DocWriter = { commit: (body: (tx: Tx) => Promise<void>, context: Context) => Promise<unknown> };

export async function recordVerdict(writer: DocWriter, context: Context, verdict: Omit<Verdict, "at">): Promise<void> {
  await writer
    .commit(async (tx) => {
      const doc = await tx.doc(VerdictDoc);
      doc.verdicts = pushVerdict(doc.verdicts, { ...verdict, at: Date.now() });
    }, context)
    .catch(() => {
      // A verdict write must never fail the turn it describes.
    });
}

export async function recentVerdicts(harness: Harness, context: Context, sinceMs: number): Promise<Verdict[]> {
  const doc = await harness.snapshot(VerdictDoc, context).catch(() => undefined);
  return (doc?.verdicts ?? []).filter((verdict) => verdict.at >= sinceMs);
}
