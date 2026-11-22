import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { logError } from "./log.ts";

const run = promisify(execFile);

/**
 * A watch turns a routine from "say something every hour" into "say something when it changes".
 * The whole point is that an unchanged reading costs one command and nothing else: no turn, no
 * tokens, no card. So the reading has to be a string two runs can be compared with, and it has to
 * be readable by the turn that eventually wakes, because "it changed" without "from what to what"
 * only moves the question to the person.
 */

/** Enough to recognise the change and explain it; a reading that needs more is a report, not a signal. */
const MAX_READING = 3_000;
const TIMEOUT_MS = 60_000;
const MAX_BUFFER = 4 * 1024 * 1024;

export type WatchLook = {
  /** The comparable form: `ok\n<output>` or `<status>\n<what it printed>`. */
  reading: string;
  /** One line for the tool's answer, so a person can see what the command currently says. */
  summary: string;
  /** The command did not run to completion. A routine must not be built on one of these. */
  broke: boolean;
};

/** Strip what a terminal renders but a comparison should not see: colour codes and carriage returns. */
export function plainWatchText(raw: string): string {
  return raw
    .replace(/\u001b\[[0-9;]*m/g, "")
    .replace(/\r\n?/g, "\n")
    .trim();
}

/** Cut a long reading from the middle: the head says what it is about, the tail where it went. */
export function clampReading(text: string, limit = MAX_READING): string {
  if (text.length <= limit) return text;
  const keep = Math.floor((limit - 40) / 2);
  const dropped = text.length - keep * 2;
  return `${text.slice(0, keep)}\n…[${dropped} 字符略]…\n${text.slice(-keep)}`;
}

/** A reading in one line: its status, and how much it printed. */
export function summarizeWatch(reading: string): string {
  const [status = "", ...body] = reading.split("\n");
  const lines = body.filter((line) => line.trim() !== "").length;
  return status === "ok" ? `${lines} 行` : `${status} · ${lines} 行输出`;
}

/**
 * Run the command and fold whatever happened into a reading. A failure is a reading like any other:
 * two identical failures stay quiet, a failure after successes wakes the routine — which is exactly
 * the "CI went red" case, and the case where a broken command must not be mistaken for calm.
 */
export async function readWatch(cmd: string, cwd: string): Promise<WatchLook> {
  try {
    const { stdout } = await run("/bin/sh", ["-c", cmd], {
      cwd,
      timeout: TIMEOUT_MS,
      maxBuffer: MAX_BUFFER,
    });
    const reading = `ok\n${clampReading(plainWatchText(stdout))}`;
    return { reading, summary: summarizeWatch(reading), broke: false };
  } catch (error) {
    const failed = error as {
      code?: number | string;
      signal?: string;
      killed?: boolean;
      stdout?: string;
      stderr?: string;
    };
    // A timeout has no exit code worth printing; `killed` is the only thing that says why.
    const status = failed.killed === true ? `timeout ${TIMEOUT_MS / 1000}s` : `exit ${String(failed.code ?? "?")}`;
    const printed = plainWatchText(`${failed.stderr ?? ""}\n${failed.stdout ?? ""}`);
    const reading = `${status}\n${clampReading(printed === "" ? "（没有输出）" : printed)}`;
    if (failed.code === undefined) {
      logError("watch", `watch command did not finish: ${cmd}`, error);
    }
    return { reading, summary: summarizeWatch(reading), broke: true };
  }
}

/**
 * What the turn is told about the change. The baseline case is named, because a first reading that
 * happens to differ from an empty one is not a change anybody made.
 */
export function watchChangeNote(cmd: string, before: string, after: string, baseline: boolean): string {
  const from = baseline ? "（刚建任务时的样子）" : "";
  return [
    `—— 下面这条命令的输出来得跟上一次不一样了（这就是这一轮要说的事）：`,
    `$ ${cmd}`,
    `上次${from}：`,
    clampReading(before, 1_200),
    `这次：`,
    clampReading(after, 1_200),
  ].join("\n");
}
