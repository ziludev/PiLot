/**
 * Where compaction starts, in tokens of live context.
 *
 * `contextWindow - reserveTokens` is the hard limit, and on a 1M-token model it sits at 984k —
 * nothing ever reached it. Measured requests were already carrying 185k tokens: every round pays for
 * that in prefill, and the model attends worse to the middle. Cap the trigger at 160k, the figure
 * PiBot uses, and express it through `backgroundTokens` (the soft threshold, i.e. the distance below
 * the hard limit). Raising `reserveTokens` instead would also raise the summary's output budget,
 * since that is `0.8 * reserveTokens`.
 */

export const COMPACTION_TRIGGER_CAP = 160_000;
/** The library default; also the floor for windows too small to reach the cap. */
export const DEFAULT_BACKGROUND_TOKENS = 32_768;

export interface CompactionPolicy {
  reserveTokens: number;
  keepRecentTokens: number;
  backgroundTokens: number;
}

export function compactionPolicy(contextWindow: number): CompactionPolicy {
  const reserveTokens = 16_384;
  const blocking = contextWindow - reserveTokens;
  return {
    reserveTokens,
    keepRecentTokens: 10_000,
    backgroundTokens: Math.max(DEFAULT_BACKGROUND_TOKENS, blocking - COMPACTION_TRIGGER_CAP),
  };
}
