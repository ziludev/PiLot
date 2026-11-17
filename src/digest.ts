import { defineTask, type Harness } from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";
import { OwnerChat } from "./access.ts";
import { AuditDoc, recent, type AuditEvent } from "./audit.ts";
import { notify } from "./channel.ts";
import { config } from "./config.ts";
import { elapsed } from "./ketchup.ts";
import { log } from "./log.ts";
import { settle } from "./settle.ts";
import { recentVerdicts, VerdictDoc, type Verdict } from "./verdicts.ts";

/**
 * The morning read: what ran overnight, what did not land, and what nobody would notice by
 * scrolling. Built from data the rest of the system already keeps (verdicts + audit), so a
 * digest can never disagree with the cards it summarises.
 */

type DigestInput = {
  now: number;
  hours: number;
  verdicts: readonly Verdict[];
  audit: readonly AuditEvent[];
};

const clock = (at: number): string =>
  new Date(at).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });

const REASON_LABEL: Record<string, string> = {
  turn_limit: "到了步数上限",
  aborted: "被叫停",
  budget: "撞到预算",
};

function reasonText(reason: string): string {
  return REASON_LABEL[reason] ?? reason;
}

/** One line per turn that needs a human eye: unmet, or ended by the system rather than a claim. */
function shortfalls(verdicts: readonly Verdict[]): string[] {
  return verdicts
    .filter((verdict) => !verdict.met || verdict.reason !== undefined)
    .slice(-5)
    .map((verdict) => {
      const why = verdict.why.length > 0 ? verdict.why : "没说明";
      const suffix = verdict.reason === undefined ? "未达成" : reasonText(verdict.reason);
      return `· ${clock(verdict.at)} ${why}（${suffix}）`;
    });
}

export function formatDigest({ now, hours, verdicts, audit }: DigestInput): string {
  if (verdicts.length === 0 && audit.length === 0) return `近 ${hours} 小时没有动静。`;
  const met = verdicts.filter((verdict) => verdict.met).length;
  // Feishu's card markdown has no bullet-list syntax — "- item" renders as a literal hyphen. Emoji
  // lead each section instead, which survives both the card and the flat text a quote preview gets.
  const lines: string[] = [`📊 近 ${hours} 小时`];

  if (verdicts.length > 0) {
    const undeclared = verdicts.length - met;
    lines.push(`🛠 干活 ${verdicts.length} 轮：${met} 轮报了达成，${undeclared} 轮没达成或没声明`);
    const starts = shortfalls(verdicts);
    if (starts.length > 0) lines.push(...starts);
  }

  const approvals = audit.filter((event) => event.kind === "approve");
  if (approvals.length > 0) {
    lines.push(`✅ 审批 ${approvals.length} 次：${approvals.map((event) => event.detail ?? event.tool ?? "?").slice(-3).join("、")}`);
  }

  const tail = audit.slice(-4);
  if (tail.length > 0) {
    lines.push(`📋 审计 ${audit.length} 条：`);
    lines.push(...tail.map((event) => `· ${clock(event.at)} ${event.kind}${event.tool === undefined ? "" : ` ${event.tool}`}${event.detail === undefined ? "" : ` — ${event.detail}`}`));
  }

  const latest = verdicts.at(-1);
  if (latest !== undefined) lines.push(`⏱ 最近一轮：${clock(latest.at)} 起，${elapsed(now - latest.at)}前`);
  return lines.join("\n");
}

export async function collectDigest(
  harness: Harness,
  context: Context,
  options: { now?: number; hours?: number } = {},
): Promise<string> {
  const now = options.now ?? Date.now();
  const hours = options.hours ?? 12;
  const since = now - hours * 3_600_000;
  const [verdicts, audit] = await Promise.all([
    recentVerdicts(harness, context, since),
    recent(harness, context, 40),
  ]);
  return formatDigest({
    now,
    hours,
    verdicts,
    audit: audit.filter((event) => event.at >= since),
  });
}

/** "08:30" in Asia/Shanghai, next occurrence strictly after `now`. Pure, so a test can pin the clock. */
export function nextDigestAt(now: number, at: string): number | undefined {
  const match = /^(\d{1,2}):(\d{2})$/.exec(at.trim());
  if (match === null) return undefined;
  const hour = Number(match[1]);
  const minute = Number(match[2]);
  if (hour > 23 || minute > 59) return undefined;
  // Shanghai has no DST, so a fixed offset is exact and needs no timezone table.
  const day = new Date(now).toLocaleDateString("sv-SE", { timeZone: "Asia/Shanghai" });
  const pad = (value: number): string => String(value).padStart(2, "0");
  const today = new Date(`${day}T${pad(hour)}:${pad(minute)}:00+08:00`).getTime();
  return today > now ? today : today + 86_400_000;
}

const DIGEST_HOURS = 12;

/**
 * One message a day, at `AGENT_DIGEST_AT`, and only when there is something to say: a quiet
 * night stays quiet. `wait` exists so a restart never doubles as a send — the first phase only
 * ever computes the next deadline.
 */
export const DailyDigest = defineTask<null, { phase: "wait" } | { phase: "send" }, null>({
  name: "app.digest",
  version: 1,
  initial: () => ({ phase: "wait" }),
  phases: {
    wait: async (task, runtime, context) => {
      const next = nextDigestAt(Date.now(), config.digestAt);
      if (next === undefined) {
        await settle("app.digest", () =>
          runtime.commit(() => ({ status: "terminal", outcome: { status: "completed", result: null } }), context),
        );
        return;
      }
      await runtime.sleepUntil(next, { phase: "send" }, context);
    },
    send: async (task, runtime, context) => {
      const now = Date.now();
      const since = now - DIGEST_HOURS * 3_600_000;
      // Read in one transaction read, talk to the network outside it.
      let body = "";
      let chat: string | undefined;
      await runtime.commit(async (tx) => {
        const verdicts = ((await tx.doc(VerdictDoc)).verdicts ?? []).filter((entry) => entry.at >= since);
        const audit = ((await tx.doc(AuditDoc)).events ?? []).filter((entry) => entry.at >= since);
        if (verdicts.length > 0 || audit.length > 0) {
          body = formatDigest({ now, hours: DIGEST_HOURS, verdicts, audit });
          chat = (await tx.doc(OwnerChat)).chatId;
        }
        return undefined;
      }, context);
      if (body.length > 0 && chat !== undefined) {
        try {
          await notify(chat, body);
          log("digest", "morning digest sent", { chat, hours: DIGEST_HOURS });
        } catch (error) {
          // A digest that cannot be delivered must not fault the task: a fault retries the
          // phase, and a phase that keeps throwing loses tomorrow's digest too.
          log("digest", "could not deliver the digest", { error: String(error) });
        }
      }
      const next = nextDigestAt(now + 60_000, config.digestAt) ?? now + 86_400_000;
      await runtime.sleepUntil(next, { phase: "send" }, context);
    },
  },
  abort: (_task, runtime, context) =>
    settle("app.digest", () => runtime.commit(() => ({ status: "terminal", outcome: { status: "aborted" } }), context)),
});
