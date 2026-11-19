/**
 * What a background task's sub-agents are doing, read straight from the store.
 *
 * A task's detail card used to show its outcome, which by definition is null while it runs — so a
 * long build_feature looked dead for forty minutes. The work is in child conversations (designer,
 * implementer, tester…), each owned by the task through `conversations.owner_task_id`, and the store
 * is the only place that says which of them is alive and how far it got.
 *
 * Read-only, one connection per call: the bridge never writes here, and a stale read only costs a
 * slightly old step count.
 */
import { DatabaseSync } from "node:sqlite";

export interface SubAgentActivity {
  conversationId: number;
  /** The role line from the child's system prompt: "写技术设计文档", "按设计文档实现代码"… */
  role: string | null;
  /** Entries in the child conversation: model replies, tool results, the prompt itself. */
  steps: number;
  /** The last tool the child called, as the transcript names it. */
  tool: string | null;
  /** True while a task in that conversation can still make progress. */
  live: boolean;
  /** Wall clock (ms) the stage's first task started, null when it has not started. */
  startedAt: number | null;
  /** Wall clock (ms) the stage last settled, null while it is still running. */
  endedAt: number | null;
  /**
   * What the row is: a stage that has not begun, one in flight, one that
   * finished, or one that failed. The card draws the first two differently,
   * which is the only progress signal a forty-minute pipeline has.
   */
  state: "pending" | "live" | "done" | "failed";
}

/** Roles are written into the child's system prompt as "你的角色：<one line>"; nothing else names them. */
const roleLine = /你的角色：([^\n。]+)/;

function roleOf(instructions: unknown): string | null {
  if (typeof instructions !== "string") return null;
  const match = roleLine.exec(instructions);
  return match?.[1]?.trim() ?? null;
}

function systemRole(db: DatabaseSync, conversationId: number): string | null {
  const row = db
    .prepare(
      `select json_extract(record, '$.model[0].sections.instructions') as instructions
         from entries
        where conversation_id = ? and json_extract(record, '$.kind') = 'pi.system'
        order by id
        limit 1`,
    )
    .get(conversationId) as { instructions?: string } | undefined;
  return roleOf(row?.instructions);
}

function lastTool(db: DatabaseSync, conversationId: number): string | null {
  const row = db
    .prepare(
      `select json_extract(record, '$.model[0].toolName') as tool
         from entries
        where conversation_id = ?
          and json_extract(record, '$.model[0].toolName') is not null
        order by id desc
        limit 1`,
    )
    .get(conversationId) as { tool?: string } | undefined;
  return row?.tool ?? null;
}

/** A stage's clock: when it first started, when it last settled, whether it failed. */
function stageClock(
  db: DatabaseSync,
  conversationId: number,
): { startedAt: number | null; endedAt: number | null; failure: boolean } {
  const row = db
    .prepare(
      `select min(json_extract(record, '$.startedAt')) as startedAt,
              max(json_extract(record, '$.endedAt')) as endedAt,
              sum(case when json_extract(record, '$.state.outcome.status') in ('failed', 'faulted')
                       then 1 else 0 end) as failures
         from tasks
        where conversation_id = ?`,
    )
    .get(conversationId) as
    | { startedAt?: number | null; endedAt?: number | null; failures?: number | null }
    | undefined;
  const startedAt = row?.startedAt ?? null;
  const endedAt = row?.endedAt ?? null;
  return { startedAt, endedAt, failure: (row?.failures ?? 0) > 0 };
}

function isLive(db: DatabaseSync, conversationId: number): boolean {
  const row = db
    .prepare(
      `select count(*) as live
         from tasks
        where conversation_id = ?
          and status in ('pending', 'running', 'waiting', 'completing')`,
    )
    .get(conversationId) as { live?: number } | undefined;
  return (row?.live ?? 0) > 0;
}

/** Elapsed wall clock the way the cards write it: 42s, 6m 12s, 2h 10m. */
export function spanText(ms: number): string {
  const seconds = Math.max(0, Math.round(ms / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

/**
 * One stage, one line: the role, how long it has been at it, and whether it broke. A card that
 * shows only tool rounds cannot tell "still designing" from "already implementing".
 */
export function stageLine(agent: SubAgentActivity, now: number): string {
  const role = shortRole(agent.role);
  if (agent.state === "failed") return `✗ ${role} · 失败`;
  const span = spanText((agent.endedAt ?? now) - (agent.startedAt ?? now));
  return `${agent.state === "live" ? "▸" : "✓"} ${role} · ${span}`;
}

/**
 * A stage name short enough for a card line. The role is a sentence ("按设计文档实现代码，跑通构建"),
 * and a card that repeats the whole sentence spends a line saying what the state already says.
 */
export function shortRole(role: string | null, limit = 10): string {
  if (role === null || role.trim() === "") return "子会话";
  const head = (role.split(/[，。；、]/)[0] ?? role).trim();
  return head.length <= limit ? head : `${head.slice(0, limit)}…`;
}

/** One row per child conversation, newest last, from a list of conversation ids. */
function describe(db: DatabaseSync, children: Array<{ id: number }>): SubAgentActivity[] {
  const count = db.prepare(`select count(*) as steps from entries where conversation_id = ?`);
  return children.map((child) => {
    const steps = (count.get(child.id) as { steps?: number } | undefined)?.steps ?? 0;
    const live = isLive(db, child.id);
    const clock = stageClock(db, child.id);
    const state = live ? "live" : clock.failure ? "failed" : clock.startedAt === null ? "pending" : "done";
    return {
      conversationId: child.id,
      role: systemRole(db, child.id),
      steps,
      tool: lastTool(db, child.id),
      live,
      startedAt: clock.startedAt,
      endedAt: live ? null : clock.endedAt,
      state,
    };
  });
}

/**
 * One row per child conversation, newest last. A task with no children returns nothing, which is what
 * an ordinary chat task looks like.
 */
export function subAgentActivity(dbPath: string, taskId: number): SubAgentActivity[] {
  let db: DatabaseSync;
  try {
    db = new DatabaseSync(dbPath, { readOnly: true });
  } catch {
    // No store yet (or a locked file): the card falls back to whatever else it knows.
    return [];
  }
  try {
    const children = db
      .prepare(`select id from conversations where owner_task_id = ? order by id`)
      .all(taskId) as Array<{ id: number }>;
    return describe(db, children);
  } catch {
    // A schema this build does not know is not worth a 500 on the task endpoint.
    return [];
  } finally {
    db.close();
  }
}

/**
 * The stages of the turn now running in a chat conversation.
 *
 * A card belongs to a turn, but the children that do a pipeline's work hang off that turn's task,
 * one level below the conversation the card serves — and the card has no task id. The conversation
 * and "started after this turn began" are what it does have, so that is what this reads. Stages of
 * earlier turns in the same chat stay out of the current card.
 */
export function subAgentStagesSince(
  dbPath: string,
  conversationId: number,
  since: number,
): SubAgentActivity[] {
  let db: DatabaseSync;
  try {
    db = new DatabaseSync(dbPath, { readOnly: true });
  } catch {
    return [];
  }
  try {
    const children = db
      .prepare(
        `select distinct c.id as id
           from conversations c
           join tasks t on t.id = c.owner_task_id
          where t.conversation_id = ?
            and coalesce(json_extract(t.record, '$.startedAt'), 0) >= ?
          order by c.id`,
      )
      .all(conversationId, since) as Array<{ id: number }>;
    return describe(db, children);
  } catch {
    return [];
  } finally {
    db.close();
  }
}
