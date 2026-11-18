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
    const count = db.prepare(`select count(*) as steps from entries where conversation_id = ?`);
    return children.map((child) => {
      const steps = (count.get(child.id) as { steps?: number } | undefined)?.steps ?? 0;
      return {
        conversationId: child.id,
        role: systemRole(db, child.id),
        steps,
        tool: lastTool(db, child.id),
        live: isLive(db, child.id),
      };
    });
  } catch {
    // A schema this build does not know is not worth a 500 on the task endpoint.
    return [];
  } finally {
    db.close();
  }
}
