import type { Harness } from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";
import { listApprovals } from "./approvals.ts";
import { recent, type AuditEvent } from "./audit.ts";

/**
 * `/ketchup`: what happened since the owner last looked, and everything waiting on them.
 *
 * The card answers a question the owner keeps asking in words ("怎么样了"), so the shapes here
 * are pinned by a test: a run in flight, an approval waiting, an audit tail, and a quiet window
 * that says so instead of showing an empty list.
 */

export type KetchupTask = {
  title: string;
  /** The harness task state kind: running, sleeping, waiting. */
  state: string;
  until?: number;
};

export type KetchupApproval = { tool: string; summary: string; waitedMs: number };

export type KetchupInput = {
  now: number;
  hours: number;
  tasks: KetchupTask[];
  approvals: KetchupApproval[];
  events: AuditEvent[];
};

const clock = (at: number): string =>
  new Date(at).toLocaleTimeString("zh-CN", { timeZone: "Asia/Shanghai", hour12: false, hour: "2-digit", minute: "2-digit" });

/** Minutes when under an hour, hours after, because "等待 3 小时" is not news. */
export function elapsed(ms: number): string {
  const minutes = Math.floor(ms / 60_000);
  if (minutes < 1) return "刚刚";
  if (minutes < 60) return `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  return hours < 24 ? `${hours} 小时` : `${Math.floor(hours / 24)} 天`;
}

export function formatKetchup(input: KetchupInput): string {
  const { now, hours, tasks, approvals, events } = input;
  const nothing = tasks.length === 0 && approvals.length === 0 && events.length === 0;
  if (nothing) return `**最近 ${hours} 小时没有动静。**`;

  const lines: string[] = [`**最近 ${hours} 小时**`];

  const running = tasks.filter((task) => task.state === "running");
  const waiting = tasks.filter((task) => task.state !== "running");
  if (running.length > 0) {
    lines.push("", `**在跑 ${running.length} 件**`);
    for (const task of running) lines.push(`· ${task.title} · ${task.state}`);
  }
  if (waiting.length > 0) {
    lines.push("", `**排在后面 ${waiting.length} 件**`);
    for (const task of waiting) {
      const when = task.until === undefined ? task.state : `${task.state} · ${clock(task.until)} 起来`;
      lines.push(`· ${task.title} · ${when}`);
    }
  }
  if (approvals.length > 0) {
    lines.push("", `**等你决定 ${approvals.length} 件**`);
    for (const approval of approvals) {
      lines.push(`· \`${approval.tool}\` · ${approval.summary} · 已等 ${elapsed(approval.waitedMs)}`);
    }
  }
  if (events.length > 0) {
    lines.push("", `**做过 ${events.length} 条**`);
    for (const event of events) {
      const detail = event.detail === undefined ? "" : ` · ${event.detail.slice(0, 60)}`;
      const tool = event.tool === undefined ? "" : ` \`${event.tool}\``;
      lines.push(`· ${clock(event.at)} ${event.kind}${tool}${detail}`);
    }
  }
  return lines.join("\n");
}

/** Newest last, capped, and only inside the window: a digest, not a log dump. */
export function withinWindow(events: readonly AuditEvent[], now: number, hours: number, limit = 6): AuditEvent[] {
  const since = now - hours * 3_600_000;
  return events.filter((event) => event.at >= since).slice(-limit);
}

export async function collectKetchup(
  harness: Harness,
  context: Context,
  options: { now?: number; hours?: number } = {},
): Promise<KetchupInput> {
  const now = options.now ?? Date.now();
  const hours = options.hours ?? 12;

  const inspection = await harness.inspect(context).catch(() => ({ tasks: [] as never[] }));
  const tasks: KetchupTask[] = inspection.tasks.flatMap((task) => {
    const state = task.state as unknown as { kind?: string; status?: string; until?: number };
    const input = task.record.input as { text?: string; spec?: string } | undefined;
    const kind = state.kind ?? state.status ?? "?";
    if (kind === "terminal") return [];
    const title = input?.text ?? input?.spec?.replace(/\s+/g, " ").slice(0, 50) ?? task.record.kind;
    return [{ title, state: kind, until: state.until }];
  });

  const approvals: KetchupApproval[] = listApprovals().map((approval) => ({
    tool: approval.tool,
    summary: approval.summary,
    waitedMs: now - approval.createdAt,
  }));

  const events = withinWindow(await recent(harness, context, 100), now, hours);
  return { now, hours, tasks, approvals, events };
}
