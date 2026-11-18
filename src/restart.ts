/**
 * Restart notes.
 *
 * The agent goes away and comes back looking identical from the inside: `agentctl restart` waits for
 * an idle window, a queued restart may sit for twenty minutes, and launchd also restarts us after a
 * crash. Alex never got told any of it — the only trace was a line in logs/restart-later.log — so the
 * switch now leaves a note next to the database before it cuts us, and the next boot reads it.
 *
 * Two kinds of news, one card each:
 *   planned — the note says who asked, why, and when the request was queued (so the wait is visible);
 *   crash   — no note, but the boot marker from the previous process was never cleared.
 */
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

export interface RestartNote {
  /** `queued` (restart-later), `manual` (agentctl restart) or `planned` (the queued job firing). */
  kind: string;
  reason?: string;
  /** When the restart was first asked for. A queued note keeps this across the fire. */
  queuedAt?: number;
  /** When the switch actually cut the process. */
  at?: number;
}

export interface BootMark {
  pid: number;
  startedAt: number;
}

const noteFile = "restart-pending.json";
const bootFile = "boot.json";

function notePath(dir: string): string {
  return join(dir, noteFile);
}

function bootPath(dir: string): string {
  return join(dir, bootFile);
}

/** Read and remove the note: it describes exactly one restart, so it must not fire twice. */
export function takeRestartNote(dir: string): RestartNote | undefined {
  const path = notePath(dir);
  if (!existsSync(path)) return undefined;
  let note: RestartNote | undefined;
  try {
    note = JSON.parse(readFileSync(path, "utf8")) as RestartNote;
  } catch {
    // A half-written note (power loss between printf and the bootout) is not worth a card.
    note = undefined;
  } finally {
    rmSync(path, { force: true });
  }
  return note;
}

/** The previous process's marker, if it never got to remove it. */
export function readBootMark(dir: string): BootMark | undefined {
  const path = bootPath(dir);
  if (!existsSync(path)) return undefined;
  try {
    const mark = JSON.parse(readFileSync(path, "utf8")) as BootMark;
    return typeof mark?.pid === "number" ? mark : undefined;
  } catch {
    return undefined;
  }
}

export function markBoot(dir: string, pid: number, now = Date.now()): void {
  mkdirSync(dir, { recursive: true });
  const mark: BootMark = { pid, startedAt: now };
  writeFileSync(bootPath(dir), `${JSON.stringify(mark)}\n`);
}

/** Called on every graceful exit; a marker left behind therefore means the process was killed. */
export function clearBoot(dir: string): void {
  rmSync(bootPath(dir), { force: true });
}

function clock(at: number): string {
  const d = new Date(at);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

function span(ms: number): string {
  const seconds = Math.max(0, Math.round(ms / 1000));
  if (seconds < 90) return `${seconds} 秒`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 90) return `${minutes} 分钟`;
  return `${Math.round(minutes / 60)} 小时`;
}

const kindText: Record<string, string> = {
  queued: "我排队重启，等手上的回合跑完再加载新代码",
  planned: "排队的那次重启落地了",
  manual: "手动重启（agentctl restart）",
};

/** What the new process tells the owner. Pure, so the wording can be tested without a switch. */
export function formatRestartNotice(note: RestartNote, now: number, pid: number): string {
  const firedAt = note.at ?? now;
  const reason = note.reason?.trim() || kindText[note.kind] || "重启";
  const waited = note.queuedAt === undefined ? undefined : firedAt - note.queuedAt;
  const lines = [`- 原因：${reason}`, `- 重启于 ${clock(firedAt)}，新进程 pid ${pid}`];
  if (waited !== undefined && waited > 60_000) {
    lines.push(`- 从排队到落地等了 ${span(waited)}（等空档，没有打断正在跑的回合）`);
  }
  return `🔄 已重启\n\n${lines.join("\n")}`;
}

export function formatCrashNotice(mark: BootMark, now: number, pid: number): string {
  const ran = now - mark.startedAt;
  return [
    "⚠️ 上次没正常退出",
    "",
    `- 上一个进程（pid ${mark.pid}）${clock(mark.startedAt)} 启动，跑了 ${span(ran)} 就没了`,
    `- 没有重启记录，按崩溃处理；现在是 pid ${pid}，任务会接着跑`,
  ].join("\n");
}

/**
 * Boot-time news, or undefined when the process came back the boring way (launchd start, first run).
 * Reads the note first: a planned restart is not also a crash even though neither process exited by
 * itself.
 */
export function bootNews(dir: string, now: number, pid: number): string | undefined {
  const note = takeRestartNote(dir);
  if (note !== undefined) return formatRestartNotice(note, now, pid);
  const previous = readBootMark(dir);
  if (previous === undefined) return undefined;
  return formatCrashNotice(previous, now, pid);
}
