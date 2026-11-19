import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type { Context } from "@earendil-works/chord/context";
import { getFeishuStatus } from "./feishu.ts";
import { ChatsToken, type AgentRuntime } from "./harness.ts";

/**
 * What the bridge says about itself, in the shape the desktop client watches it.
 *
 * The client shows this next to the workspace path, so that a bridge which dies
 * and comes back is something the user can see rather than guess at.
 *
 * Everything here is read defensively. A health check that throws is worse than
 * no health check: the client would report a dead agent while the agent is
 * fine, so a number that cannot be read comes back as zero.
 */
export type Health = {
  pid: number;
  /** ISO, so the client's clock is all it needs to work out an uptime. */
  startedAt: string;
  uptimeMs: number;
  version: string;
  /** Submissions waiting for a run, the same count `scripts/agentctl` prints. */
  queueDepth: number;
  runningTurns: number;
  conversations: number;
  /**
   * Whether the bot can still hear and speak on Feishu, and how long it has been deaf. It lives in
   * the gateway now, so this is the bridge's read of that process, refreshed every 30s — the one
   * field that tells a silent bot apart from a quiet chat.
   */
  feishu: ReturnType<typeof getFeishuStatus>;
};

const here = dirname(fileURLToPath(import.meta.url));

/** The version the client shows in the tooltip: the one this bridge was built from. */
export function bridgeVersion(): string {
  try {
    const pkg = JSON.parse(readFileSync(join(here, "..", "package.json"), "utf8")) as { version?: string };
    return pkg.version ?? "unknown";
  } catch {
    // Cosmetic: an unreadable package.json should not make the bridge unhealthy.
    return "unknown";
  }
}

export async function healthReport(
  runtime: AgentRuntime,
  context: Context,
  startedAt: number,
  now: number = Date.now(),
): Promise<Health> {
  const inspection = await runtime.harness.inspect(context).catch(() => undefined);
  // `submissions` is "queued and placed submissions" — the queue itself, so the
  // depth needs no SQLite read of its own.
  const queueDepth = inspection?.submissions.length ?? 0;
  const runningTurns = inspection?.tasks.filter((task) => task.state.kind === "running").length ?? 0;
  const chats = (await runtime.harness.snapshot(ChatsToken, context).catch(() => undefined)) as
    | { chats: Record<string, number> }
    | undefined;
  return {
    pid: process.pid,
    startedAt: new Date(startedAt).toISOString(),
    uptimeMs: Math.max(0, now - startedAt),
    version: bridgeVersion(),
    queueDepth,
    runningTurns,
    // One surface can be counted twice only if two chats share a conversation,
    // which is how a thread and its parent look: count the conversations.
    conversations: new Set(Object.values(chats?.chats ?? {})).size,
    feishu: getFeishuStatus(),
  };
}
