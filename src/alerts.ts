/**
 * Which failures get a message in the chat, and what that message says.
 *
 * The harness swallows failures on purpose: a hook that throws, a task that ends faulted, the
 * storage closing after a platform reset. They arrive at `onReport` and, logged only, are found by
 * noticing that nothing happened — the worst way to learn the assistant is broken. Reports a person
 * can act on become one line to the home chat; the rest stay in the log where they belong.
 */
import type { Report } from "@earendil-works/pi-durable";

/** A report of this kind means something is broken in a way a restart or a fix will not fix itself. */
const ALERT_KINDS = new Set<Report["kind"]>([
  "storage_failed",
  "task_faulted",
  "task_interrupted",
  "migration_failed",
  "extension_error",
  "host_error",
]);

/** A flapping hook can report the same failure dozens of times in a minute; one message is enough. */
const WINDOW_MS = 10 * 60_000;
const seen = new Map<string, number>();

function reason(report: Report): string | undefined {
  return "reason" in report ? (report.reason as string | undefined) : undefined;
}

function where(report: Report): string {
  if ("task" in report && report.task !== undefined) return report.task.kind;
  if ("extension" in report && report.extension !== undefined) return report.extension;
  if ("callback" in report && report.callback !== undefined) return `host:${report.callback}`;
  return "";
}

function detail(report: Report): string {
  const error = report.error;
  const message =
    error instanceof Error ? error.message : typeof error === "string" ? error : error === undefined ? "" : String(error);
  return message.replace(/\s+/g, " ").slice(0, 300);
}

/** The line to send, or `undefined` when this report is noise (a task's own report, a listener). */
export function alertText(report: Report): string | undefined {
  if (!ALERT_KINDS.has(report.kind)) return undefined;
  const subject = where(report);
  const why = reason(report);
  const head = `${report.kind}${subject === "" ? "" : ` · ${subject}`}${why === undefined ? "" : ` · ${why}`}`;
  const what = detail(report);
  return `🔴 ${head}${what === "" ? "" : `\n${what}`}`;
}

/** True the first time a given failure shows up in the window; `now` is a parameter so tests need no clock. */
export function isFresh(key: string, now: number = Date.now()): boolean {
  for (const [old, at] of seen) if (now - at > WINDOW_MS) seen.delete(old);
  const last = seen.get(key);
  if (last !== undefined && now - last <= WINDOW_MS) return false;
  seen.set(key, now);
  return true;
}

/** Tests only: the window is process state, and one test's failure must not silence another's. */
export function forgetAlerts(): void {
  seen.clear();
}

/**
 * Tell the owner about a failure that keeps the assistant from working. Never throws: it runs inside
 * `onReport`, where a throw would be reported back into the same path.
 */
export async function alertReport(
  report: Report,
  send: (surfaceId: string, text: string) => Promise<void>,
  home: string | undefined,
): Promise<boolean> {
  const text = alertText(report);
  if (text === undefined || home === undefined) return false;
  const key = `${report.kind}:${where(report)}:${reason(report) ?? ""}`;
  if (!isFresh(key)) return false;
  await send(home, text).catch(() => undefined);
  return true;
}
