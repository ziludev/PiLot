import { defineExtension, hook, ToolTask } from "@earendil-works/pi-durable";
import { log } from "./log.ts";

/**
 * Calls that change something — a file, the record, someone else's queue — as opposed to calls that
 * only look. The line between them is what tells a long run from a stuck one.
 *
 * `bash` counts as looking on purpose: it is both `git commit` and `grep`, and guessing wrong in
 * the safe direction (counting a commit as dry) only makes the warning arrive a round later.
 */
/** Tools that change something; the batching hint reuses the split. */
export const PROGRESS_TOOLS = new Set([
  "edit",
  "write",
  "remember",
  "forget",
  "remind",
  "routine",
  "delegate",
  "delegate_parallel",
  "build_feature",
  "report_done",
  "feishu_create_task",
]);

/**
 * How many calls in a row may change nothing before the run is told to wrap up.
 *
 * A stuck run does not look like a wrong answer; it looks like forty rounds of the same grep, one
 * request each, until the turn ceiling stops it minutes later. Forty is past any real read-through
 * of one question and short of the ceiling by two hundred.
 */
export const DRY_STEPS = 40;

/** Once warned, again every this many dry calls — a run that ignores it is not going to change. */
export const WARN_EVERY = 20;

/** A pause this long between two calls is a new run, not the same streak. */
const STALE_MS = 10 * 60_000;

type Streak = { dry: number; at: number };

const streaks = new Map<number, Streak>();

/** Count one call and say whether this is a round worth warning about. Pure apart from `streaks`. */
export function noteCall(
  conversationId: number,
  tool: string,
  now = Date.now(),
): { dry: number; warned: boolean } {
  const previous = streaks.get(conversationId);
  if (previous === undefined || now - previous.at > STALE_MS) {
    streaks.set(conversationId, { dry: 0, at: now });
  }
  const streak = streaks.get(conversationId) as Streak;
  streak.at = now;
  if (PROGRESS_TOOLS.has(tool)) {
    streak.dry = 0;
    return { dry: 0, warned: false };
  }
  streak.dry += 1;
  const warned = streak.dry >= DRY_STEPS && (streak.dry - DRY_STEPS) % WARN_EVERY === 0;
  return { dry: streak.dry, warned };
}

/** Test hook, and the way to keep a stale conversation from leaking into a new day. */
export function forgetStreak(conversationId: number): void {
  streaks.delete(conversationId);
}

/**
 * The line appended to a tool result the model is about to read.
 *
 * It goes on the result rather than on a request because a result is how the model is spoken to
 * mid-run: it arrives in the same place as the output it already reacts to, it costs no extra
 * round, and a provider cannot reject it for being in the wrong place.
 */
export function stallWarning(dry: number): string {
  return (
    `\n\n（自动提醒，不是 alex 说的）这一轮已经连续 ${dry} 步只读没写：没有改文件，也没有提交。` +
    "如果卡住了，现在就停下来说清卡在哪、缺什么，别再试同一件事；如果确实还要查，" +
    "换个角度查，或者先写下已经确认的东西，不要重复上一条命令。"
  );
}

export const stallExtension = defineExtension({
  name: "stall",
  tasks: [],
  hooks: [
    hook(ToolTask, {
      afterTool: (call, result, api) => {
        const { dry, warned } = noteCall(api.conversationId as unknown as number, call.name);
        if (!warned) return undefined;
        log("stall", "told a dry run to wrap up", { tool: call.name, dry });
        // Appending to an error result still leaves it an error; a failed dry call reads the same
        // way it would have.
        const output = result.output ?? [];
        return {
          ...result,
          output: [...output, { type: "text", text: stallWarning(dry) }],
        };
      },
    }),
  ],
});
