import { defineExtension, defineTask, defineTool } from "@earendil-works/pi-durable";
import { obj, opt, str } from "@earendil-works/pi-schema";
import { conversationForSurface, startTurn, surfaceForConversation } from "./channel.ts";
import { recordSpend, spentSoFar } from "./spend.ts";
import { log, logError } from "./log.ts";
import { getHarness } from "./runtime-ref.ts";

/**
 * A routine is a reminder that comes back. Everything a routine needs already existed once the
 * reminder did — a durable task that sleeps with a stored deadline — so this is the same shape with
 * two differences: it never settles, and what it does when it wakes is start a *turn* in the chat
 * instead of dropping one line into it. A reminder says something; a routine works.
 *
 * The reason to keep it in the ledger rather than in `launchd` is the same reason the queue is
 * durable: a routine that fires at 09:00 while the machine is asleep must still fire, once, when it
 * wakes up — and `sleepUntil` on a past deadline does exactly that.
 */

const DAY = 86_400_000;
const MIN_EVERY = 5 * 60_000;
const WEEKDAY_NAMES = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];
const WEEKDAY_LABELS = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];
const WEEKDAY_CN: Record<string, number> = { 日: 0, 天: 0, 一: 1, 二: 2, 三: 3, 四: 4, 五: 5, 六: 6 };

export type Schedule =
  | { kind: "daily"; at: number }
  | { kind: "weekdays"; at: number }
  | { kind: "weekly"; weekday: number; at: number }
  | { kind: "every"; everyMs: number };

/** Minutes past local midnight, from `09:00` / `9:5`. */
function parseClock(text: string): number | undefined {
  const match = /^(\d{1,2}):(\d{2})$/.exec(text);
  if (match === null) return undefined;
  const hour = Number(match[1]);
  const minute = Number(match[2]);
  if (hour > 23 || minute > 59) return undefined;
  return hour * 60 + minute;
}

/**
 * `daily 09:00` / `weekdays 09:00` / `weekly mon 09:00` / `every 6h`, plus the Chinese spellings
 * the owner actually types. Returns the error as a string: the caller is a tool, and a tool that
 * throws loses the correction the model needed.
 */
export function parseSchedule(raw: string): Schedule | string {
  const text = raw
    .trim()
    .toLowerCase()
    .replace(/\s+/g, " ")
    .replace(/^每天\s*/, "daily ")
    .replace(/^工作日\s*/, "weekdays ")
    .replace(/^每周([日天一二三四五六])\s*/, (_all, day: string) => `weekly ${WEEKDAY_CN[day]} `)
    .trim();

  if (text.startsWith("every")) {
    const match = /^every (\d+) ?(m|min|minutes|h|hour|hours|d|day|days)$/.exec(text);
    if (match === null) return `看不懂的周期：${raw}（支持 every 30m / every 6h / every 2d）`;
    const unit = (match[2] ?? "m").startsWith("m") ? 60_000 : (match[2] ?? "m").startsWith("h") ? 3_600_000 : DAY;
    const everyMs = Number(match[1]) * unit;
    if (everyMs < MIN_EVERY) return `太频繁了：最快 5 分钟一次（收到的是 ${raw}）`;
    return { kind: "every", everyMs };
  }

  const parts = text.split(" ");
  const head = parts[0] ?? "";
  if (head === "daily" || head === "weekdays") {
    const at = parseClock(parts[1] ?? "");
    if (at === undefined) return `看不出几点：${raw}（写成 ${head} 09:00）`;
    return { kind: head, at };
  }
  if (head === "weekly") {
    const day = parts[1] ?? "";
    const weekday = /^\d$/.test(day) ? Number(day) : WEEKDAY_NAMES.indexOf(day);
    if (weekday < 0 || weekday > 6) return `看不出星期几：${raw}（写成 weekly mon 09:00）`;
    const at = parseClock(parts[2] ?? "");
    if (at === undefined) return `看不出几点：${raw}（写成 weekly mon 09:00）`;
    return { kind: "weekly", weekday, at };
  }

  // A bare clock is the smallest thing that can work: `09:00` means every day at nine.
  const at = parseClock(text);
  if (at !== undefined) return { kind: "daily", at };
  return `看不懂的周期：${raw}（支持 daily 09:00 / weekdays 09:00 / weekly mon 09:00 / every 6h）`;
}

/** Local calendar day as `YYYY-MM-DD`. Wall clock in Shanghai, never UTC: 09:00 is 09:00. */
function localDay(at: number): string {
  return new Date(at).toLocaleDateString("en-CA", { timeZone: "Asia/Shanghai" });
}

/** 0 = Sunday. Derived from the calendar day, so a fixed +08:00 offset cannot shift it. */
function weekdayOf(day: string): number {
  return (Math.floor(Date.parse(`${day}T00:00:00Z`) / DAY) + 4) % 7;
}

export function describe(schedule: Schedule): string {
  if (schedule.kind === "every") {
    const minutes = schedule.everyMs / 60_000;
    if (minutes < 60) return `每 ${minutes} 分钟`;
    if (minutes % 1440 === 0) return `每 ${minutes / 1440} 天`;
    return `每 ${minutes / 60} 小时`;
  }
  const clock = `${String(Math.floor(schedule.at / 60)).padStart(2, "0")}:${String(schedule.at % 60).padStart(2, "0")}`;
  if (schedule.kind === "daily") return `每天 ${clock}`;
  if (schedule.kind === "weekdays") return `工作日 ${clock}`;
  return `每${WEEKDAY_LABELS[schedule.weekday]} ${clock}`;
}

/** First firing strictly after `now`. Anchored for intervals so a restart never drifts the grid. */
export function nextOccurrence(schedule: Schedule, now: number, anchor: number): number {
  if (schedule.kind === "every") {
    const steps = Math.floor((now - anchor) / schedule.everyMs) + 1;
    return anchor + Math.max(1, steps) * schedule.everyMs;
  }
  const clock = `${String(Math.floor(schedule.at / 60)).padStart(2, "0")}:${String(schedule.at % 60).padStart(2, "0")}`;
  for (let offset = 0; offset <= 8; offset += 1) {
    const day = localDay(now + offset * DAY);
    const at = Date.parse(`${day}T${clock}:00+08:00`);
    if (at <= now) continue;
    const weekday = weekdayOf(day);
    if (schedule.kind === "weekdays" && (weekday === 0 || weekday === 6)) continue;
    if (schedule.kind === "weekly" && weekday !== schedule.weekday) continue;
    return at;
  }
  // Eight days covers every weekly constraint; a schedule must never stop the loop regardless.
  return now + DAY;
}

export function formatWhen(at: number): string {
  return new Date(at).toLocaleString("zh-CN", { timeZone: "Asia/Shanghai", hour12: false });
}

type RoutineInput = {
  schedule: Schedule;
  text: string;
  /** Resolved again at firing time: the chat may have moved to another conversation. */
  surfaceId: string;
  /** Creation time; interval schedules keep their grid on it. */
  anchor: number;
};

type RoutineState = { phase: "wait" } | { phase: "fire"; at: number };

const Routine = defineTask<RoutineInput, RoutineState, string>({
  name: "app.routine",
  version: 1,
  initial: () => ({ phase: "wait" }),
  phases: {
    wait: async (task, runtime, context) => {
      const at = nextOccurrence(task.input.schedule, Date.now(), task.input.anchor);
      await runtime.sleepUntil(at, { phase: "fire", at }, context);
    },
    fire: async (task, runtime, context) => {
      const surfaceId = surfaceForConversation(task.conversationId) ?? task.input.surfaceId;
      // What the chat's ledger says before the turn, so the routine can report what its own run
      // cost: nobody is watching an unattended run spend money.
      const chatConversation = conversationForSurface(surfaceId);
      const before =
        chatConversation === undefined ? undefined : await spentSoFar(runtime, context, chatConversation).catch(() => undefined);
      // The turn is told it is unattended, because the expensive failure mode of a routine is a
      // turn that stops halfway to ask a question nobody is there to answer.
      const instruction = `⏰ 定时任务（无人在线：不要反问我，做完直接在回复里给结果和验证方式）\n\n${task.input.text}`;
      try {
        const started = await startTurn(surfaceId, instruction, `routine:${task.id}:${task.state.checkpoint.at}`);
        log("routines", started ? "routine started a turn" : "routine had nowhere to run", {
          surface: surfaceId,
          scheduled: formatWhen(task.state.checkpoint.at),
        });
        if (started && chatConversation !== undefined && before !== undefined) {
          const after = await spentSoFar(runtime, context, chatConversation).catch(() => undefined);
          if (after !== undefined && after.cost > before.cost) {
            await recordSpend(runtime as never, context, task.id, {
              cost: after.cost - before.cost,
              tokens: after.tokens - before.tokens,
              at: Date.now(),
            });
          }
        }
      } catch (error) {
        // A failed start must not fault the phase: the retry would fire the routine again, and a
        // routine that throws twice a day is worse than one that skips a beat.
        logError("routines", `could not start the routine turn in ${surfaceId}`, error);
      }
      const at = nextOccurrence(task.input.schedule, Date.now(), task.input.anchor);
      await runtime.sleepUntil(at, { phase: "wait" }, context);
    },
  },
  abort: async (task, runtime, context) => {
    log("routines", "routine cancelled", { task: task.id, text: task.input.text.slice(0, 60) });
    await runtime.commit(() => ({ status: "terminal", outcome: { status: "aborted" } }), context);
  },
});

const routine = defineTool({
  name: "routine",
  description:
    "建一个常驻的定时任务：到点自动在聊天里开一轮（跟收到一条消息一样），跑完把结果发到那个聊天。适合「每周一自审日志」「每天早上看有没有红灯」这类。schedule 支持 daily 09:00 / weekdays 09:00 / weekly mon 09:00 / every 6h。",
  parameters: obj({
    text: str({
      description:
        "到点要做的事。写成给自己下的指令：目标 + 边界 + 怎么算做完（例如「读最近 7 天的 logs 和 report 行，挑出 3 条最该改的流程问题，能直接改的直接改」）",
    }),
    schedule: str({ description: "daily HH:MM / weekdays HH:MM / weekly mon HH:MM / every Nm|Nh|Nd" }),
  }),
  replay: "safe",
  execute: async (args, api, context) => {
    const schedule = parseSchedule(args.schedule);
    if (typeof schedule === "string") return { output: [{ type: "text" as const, text: schedule }] };
    const anchor = api.now();
    const first = nextOccurrence(schedule, anchor, anchor);
    const surfaceId = surfaceForConversation(api.conversationId) ?? "";
    const taskId = await api.createTask(
      Routine,
      { schedule, text: args.text, surfaceId, anchor },
      // Background: the routine outlives this turn, so the conversation goes idle while it waits.
      { ownership: { kind: "conversation" }, background: true },
      context,
    );
    return {
      output: [
        {
          type: "text" as const,
          text: `建好了：${describe(schedule)}，下次 ${formatWhen(first)} 在聊天里开跑（task ${taskId}）`,
        },
      ],
    };
  },
});

const routines = defineTool({
  name: "routines",
  description: "列出常驻定时任务。传 cancel = 任务 id 取消它，或 cancel = all 全部取消。",
  parameters: obj({
    cancel: opt(str({ description: "任务 id，或 all" })),
  }),
  replay: "safe",
  execute: async (args, api, context) => {
    const harness = getHarness();
    if (harness === undefined) return { output: [{ type: "text" as const, text: "还没有可用的会话。" }] };
    const inspection = await harness.inspect(context);
    const live = inspection.tasks.filter((entry) => entry.record.kind === "app.routine");
    const idOf = (entry: (typeof live)[number]): string => String(entry.record.id);

    if (typeof args.cancel === "string" && args.cancel.length > 0) {
      const targets = args.cancel === "all" ? live : live.filter((entry) => idOf(entry) === args.cancel);
      if (targets.length === 0) {
        return { output: [{ type: "text" as const, text: `没有这个任务：${args.cancel}` }] };
      }
      // A routine that is running right now is running *inside this turn* — it drove the submission
      // this tool call belongs to — so awaiting its abort would wait for the answer being written.
      // Mark it and let its phase end once the turn does.
      const running: string[] = [];
      for (const entry of targets) {
        if (entry.state.kind === "running") {
          running.push(idOf(entry));
          void harness
            .abortTask(entry.record.id, context)
            .catch((error) => logError("routines", `could not cancel ${idOf(entry)}`, error));
          continue;
        }
        await harness.abortTask(entry.record.id, context);
      }
      const done = targets.length - running.length;
      const text =
        running.length === 0
          ? `取消了 ${done} 个定时任务`
          : `${done === 0 ? "" : `取消了 ${done} 个定时任务；`}${running.join("、")} 正在跑这一轮，跑完就停`;
      return { output: [{ type: "text" as const, text }] };
    }

    if (live.length === 0) return { output: [{ type: "text" as const, text: "现在没有定时任务。" }] };
    const lines = live.map((entry) => {
      const input = entry.record.input as RoutineInput;
      const state = entry.record.state;
      const until =
        state.status === "sleeping" && typeof state.until === "number" ? `下次 ${formatWhen(state.until)}` : "正在跑";
      return `· ${describe(input.schedule)} · ${until} · ${input.text.replace(/\s+/g, " ").slice(0, 40)}（task ${idOf(entry)}）`;
    });
    return { output: [{ type: "text" as const, text: lines.join("\n") }] };
  },
});

export const routineExtension = defineExtension({ name: "routines", tools: [routine, routines], tasks: [Routine] });
