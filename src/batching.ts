import { defineExtension, hook, ToolTask } from "@earendil-works/pi-durable";
import { PROGRESS_TOOLS } from "./stall.ts";
import { log } from "./log.ts";

/**
 * Six reads in a row, each in its own request: the file for the config, then the log for the error,
 * then the table for the id. None of them needed the answer to the one before it — they were asked
 * one at a time because that is how the turn started, not because they depend on each other.
 *
 * A tool schema cannot make a model batch its calls. A line on the result it is about to read can:
 * it lands next to the output the model reacts to, at the moment the next request is being built.
 *
 * Six is past a genuine read-then-decide (read the file, then read what it imports) and short
 * enough that a long question still gets told twice.
 */
export const READ_RUN = 6;

/** A pause this long between two calls is a new run, not the same streak. */
const STALE_MS = 10 * 60_000;

type Streak = { reads: number; at: number };

const streaks = new Map<number, Streak>();

/**
 * Count one call and return how many reads in a row it is; the caller warns on exactly READ_RUN,
 * then the streak restarts so an ignored hint is not repeated on every following result.
 */
export function noteReadCall(conversationId: number, tool: string, now = Date.now()): number {
  const previous = streaks.get(conversationId);
  if (previous === undefined || now - previous.at > STALE_MS) {
    streaks.set(conversationId, { reads: 0, at: now });
  }
  const streak = streaks.get(conversationId) as Streak;
  streak.at = now;
  if (PROGRESS_TOOLS.has(tool)) {
    streak.reads = 0;
    return 0;
  }
  streak.reads += 1;
  if (streak.reads < READ_RUN) return streak.reads;
  streak.reads = 0;
  return READ_RUN;
}

/** Test hook, and the way to keep a stale conversation from leaking into a new day. */
export function forgetReads(conversationId: number): void {
  streaks.delete(conversationId);
}

/** The line appended to the result the model is about to read. */
export function batchingHint(): string {
  return (
    `\n\n（自动提醒，不是 alex 说的）刚才 ${READ_RUN} 次只读调用是一条一条发的。` +
    "**现在**就把这一轮剩下的只读查询合成一个请求一起发：同一条消息里放多个工具调用，别一条条等结果。" +
    "查文件用 read、搜代码用 project_search（一次给 200 条），比反复 grep 快。" +
    "只有前一条的答案决定了下一条该查什么，才保持一条条来——除此之外都是白等。"
  );
}

export const batchingExtension = defineExtension({
  name: "batching",
  tasks: [],
  hooks: [
    hook(ToolTask, {
      afterTool: (call, result, api) => {
        const reads = noteReadCall(api.conversationId as unknown as number, call.name);
        if (reads !== READ_RUN) return undefined;
        log("batching", "suggested sending independent reads together", { tool: call.name });
        return {
          ...result,
          output: [...(result.output ?? []), { type: "text", text: batchingHint() }],
        };
      },
    }),
  ],
});
