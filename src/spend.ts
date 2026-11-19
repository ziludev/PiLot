import { defineDoc, UsageDoc, type TaskRuntime } from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";
import { logError } from "./log.ts";

type Bucket = { cost?: { total?: number }; totalTokens?: number };

export type Spend = { cost: number; tokens: number; at: number };

/**
 * What each task's last run cost, keyed by task id.
 *
 * A task that fires on its own is the only work nobody is watching, so it is also the only work
 * whose price is worth showing: a chat that gets expensive is visible while it happens. The
 * framework's usage ledger is per conversation and cumulative, so a run's price is the difference
 * between the ledger before and after — measured around the turn, where the task already waits.
 */
export const TaskSpend = defineDoc<{ tasks: Record<string, Spend> }>({
  kind: "app.task-spend",
  version: 1,
  scope: "session",
  initial: () => ({ tasks: {} }),
  checkpointWhen: () => false,
});

const KEEP = 60;

/** The conversation's cumulative spend, summed over its ledger. Read-only: commits nothing. */
export async function spentSoFar(
  runtime: TaskRuntime<unknown, unknown, unknown, object>,
  context: Context,
  conversationId: number,
): Promise<{ cost: number; tokens: number }> {
  let total = { cost: 0, tokens: 0 };
  await runtime.commit(async (tx) => {
    const usage = await tx.doc(UsageDoc, conversationId as never);
    for (const bucket of [...Object.values(usage.models ?? {}), ...Object.values(usage.tools ?? {})]) {
      const record = bucket as unknown as Bucket;
      total = { cost: total.cost + (record.cost?.["total"] ?? 0), tokens: total.tokens + (record.totalTokens ?? 0) };
    }
    return undefined;
  }, context);
  return total;
}

/** Records one run's price, keeping only the newest few dozen tasks. */
export async function recordSpend(
  runtime: TaskRuntime<unknown, unknown, unknown, object>,
  context: Context,
  taskId: number,
  spend: Spend,
): Promise<void> {
  try {
    await runtime.commit(async (tx) => {
      const doc = await tx.doc(TaskSpend);
      doc.tasks[String(taskId)] = spend;
      const kept = Object.entries(doc.tasks).sort((a, b) => b[1].at - a[1].at);
      for (const [id] of kept.slice(KEEP)) delete doc.tasks[id];
      return undefined;
    }, context);
  } catch (error) {
    logError("spend", `could not record spend for task ${taskId}`, error);
  }
}
