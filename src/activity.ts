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

/**
 * The steps one task actually took, stage by stage, read from the store.
 *
 * A task's dialog had everything except the work: the brief it was given, the outcome if it
 * finished, and one line per child saying how far it got. "How far" is not "what happened" — when a
 * pipeline fails at 03:40 the only way to see which command went red was to open the child's chat
 * and scroll, and the child's chat is not in the rail.
 *
 * Entries carry `byTaskId`, so a task's own turn and each stage's transcript are a query away. Only
 * the blocks worth drawing are read (a tool call's name and first argument, a reply's text, a
 * result's verdict); a transcript is mostly model prose and reading all of it to draw forty rows
 * would make the dialog as slow as the run it describes.
 */
export interface TaskLogStep {
  id: number;
  /** "tool" for a call, "say" for a reply in words. */
  kind: "tool" | "say";
  at: number;
  tool?: string;
  /** One line of what the tool was told: the command, the path, the pattern. */
  label?: string;
  /** The whole of what it was told, pretty-printed and cut to a screenful. The row keeps the one-liner; this is what opens under it. */
  args?: string;
  /** What the call answered, cut to a screenful. Absent until the result arrives. */
  output?: string;
  /** "ok", "failed", or "running" — a call with no result yet is still going. */
  state: "ok" | "failed" | "running";
  /** For "say": the reply, cut to a paragraph. */
  text?: string;
  /** The provider's id for this call, which is how the task that came out of it is matched back. */
  call?: string;
}

export interface TaskLogStage {
  conversationId: number;
  /** True for the task's own turn, the stage that started the others. */
  parent: boolean;
  /** True for the one row that started the task: the call that created it. */
  start?: boolean;
  role: string | null;
  steps: TaskLogStep[];
  /** Steps left out because the stage was longer than a dialog row count. */
  more: number;
}

/** Steps a stage keeps: the tail is where a run ends, which is where the trouble is. */
const STAGE_STEPS = 40;
const TOOL_LABEL = 90;
const SAY_TEXT = 280;
/** What the panel opens under a step. A dialog row holds about this much; more is scrolling. */
const STEP_ARGS = 600;
const STEP_OUTPUT = 1000;

/** Arguments worth putting on a row, in the order they read best. */
const ARG_KEYS = ["command", "path", "pattern", "query", "url", "file", "id", "task", "name", "model", "text"];

function oneLine(text: string, limit: number): string {
  const flat = text.replace(/\s+/g, " ").trim();
  return flat.length <= limit ? flat : `${flat.slice(0, limit)}…`;
}

/**
 * A block of text as the dialog opens it: newlines kept, because a stack trace or a diff read on
 * one line is unreadable — and cut, because forty steps of full transcripts is a payload nobody
 * asked for. Both the arguments and the result go through here.
 */
function clip(text: string, limit: number): string {
  const whole = text.replace(/\r\n/g, "\n").trimEnd();
  return whole.length <= limit ? whole : `${whole.slice(0, limit)}…`;
}

/** A call's arguments, indented, as the panel shows them under the row. */
function argsText(args: unknown): string | undefined {
  if (args === undefined || args === null) return undefined;
  if (typeof args === "string") return args.trim() === "" ? undefined : clip(args, STEP_ARGS);
  try {
    const text = JSON.stringify(args, null, 2);
    return text === undefined || text === "{}" ? undefined : clip(text, STEP_ARGS);
  } catch {
    return undefined;
  }
}

/** What a tool call was told, as one line. A row says "bash · go test ./…", not a JSON blob. */
export function argumentLabel(args: unknown): string {
  if (typeof args !== "object" || args === null) return typeof args === "string" ? oneLine(args, TOOL_LABEL) : "";
  const record = args as Record<string, unknown>;
  for (const key of ARG_KEYS) {
    const value = record[key];
    if (typeof value === "string" && value.trim() !== "") return oneLine(value, TOOL_LABEL);
    if (typeof value === "number") return String(value);
  }
  for (const value of Object.values(record)) {
    if (typeof value === "string" && value.trim() !== "") return oneLine(value, TOOL_LABEL);
  }
  return "";
}

/**
 * What a call's arguments say, looked up by the call id the live snapshot carries.
 *
 * The live document names the tools of a round and nothing else, so a turn that ran everything
 * through `bash` draws five identical lines — and most turns here are exactly that, because
 * searching the web, reading a repo and checking git all go through `bash`. The arguments are in
 * the transcript one row above the slot; the first line of them is what makes those lines differ.
 *
 * Read-only and capped: a handful of rows per call id, one connection per snapshot that brings a
 * call id we have not seen.
 */
/** How far back an argument lookup looks. A call id belongs to the round that is running now. */
const HINT_WINDOW = 200;

export function callHints(dbPath: string, conversationId: number, callIds: string[]): Record<string, string> {
  const out: Record<string, string> = {};
  if (callIds.length === 0) return out;
  const db = new DatabaseSync(dbPath, { readOnly: true });
  try {
    // The window is the point. A bare `like` over the conversation is a full scan of every row:
    // the main chat here is 32k entries and 120 MB, measured at 90 ms per call id, so a twenty-call
    // round paid almost two seconds to re-read text it had already written moments earlier. A call
    // id is always from this turn, so only the tail of the transcript can hold it.
    const find = db.prepare(
      `select record from (
         select id, record from entries
          where conversation_id = ?
          order by id desc
          limit ${HINT_WINDOW}
       ) where record like ?
       order by id desc
       limit 5`,
    );
    for (const callId of callIds) {
      const rows = find.all(conversationId, `%"${callId}"%`) as { record: string }[];
      for (const row of rows) {
        let record: { model?: { role?: string; content?: Block[] }[] };
        try {
          record = JSON.parse(row.record);
        } catch {
          continue;
        }
        const block = (record.model ?? [])
          .flatMap((message) => (Array.isArray(message.content) ? message.content : []))
          .find((part) => part.type === "toolCall" && part.id === callId);
        if (block === undefined) continue;
        const hint = argumentLabel(block.arguments);
        if (hint.length > 0) out[callId] = hint;
        break;
      }
    }
  } finally {
    db.close();
  }
  return out;
}

interface Block {
  type?: string;
  text?: string;
  name?: string;
  id?: string;
  arguments?: unknown;
}

/** One row of a transcript, reduced to the part the dialog draws. */
interface RawRow {
  id: number;
  kind: string;
  at: number;
  content: Block[];
  /** On a tool result: the call it answers, and whether it broke. */
  callId: string | null;
  isError: boolean;
}

/**
 * One conversation's rows for one task, or for every task the conversation has run.
 *
 * A stage is a conversation plus the tasks it ran: filtering on the conversation alone would report
 * every earlier turn of it (the parent chat has years of them), and filtering on the task alone
 * would cross conversations when ids collide.
 */
function transcript(db: DatabaseSync, conversationId: number, taskId: number | "any"): RawRow[] {
  const where = taskId === "any"
    ? `json_extract(record, '$.byTaskId') in (select id from tasks where conversation_id = ?)`
    : `json_extract(record, '$.byTaskId') = ?`;
  const rows = db
    .prepare(
      `select id,
              json_extract(record, '$.kind') as kind,
              json_extract(record, '$.model[0].timestamp') as at,
              json_extract(record, '$.model[0].content') as content,
              json_extract(record, '$.model[0].toolCallId') as callId,
              json_extract(record, '$.model[0].isError') as isError
         from entries
        where conversation_id = ?
          and ${where}
        order by id`,
    )
    .all(conversationId, taskId === "any" ? conversationId : taskId) as Array<{
    id: number;
    kind?: string;
    at?: number | null;
    content?: string | null;
    callId?: string | null;
    isError?: number | null;
  }>;
  const out: RawRow[] = [];
  for (const row of rows) {
    if (row.content === null || row.content === undefined) continue;
    let content: Block[];
    try {
      const parsed: unknown = JSON.parse(row.content);
      if (!Array.isArray(parsed)) continue;
      content = parsed as Block[];
    } catch {
      continue;
    }
    out.push({
      id: row.id,
      kind: row.kind ?? "",
      at: row.at ?? 0,
      content,
      callId: row.callId ?? null,
      isError: row.isError === 1,
    });
  }
  // What happened in Feishu — it waited for a yes, it ended with a verdict — was written down as an
  // `app.*` entry, which has no model rows and so fell out of the query above. An approval is
  // decided outside the task's own commit scope, so the task id it belongs to rides in `data`.
  const events = db
    .prepare(
      `select id,
              json_extract(record, '$.kind') as kind,
              json_extract(record, '$.data.at') as at,
              json_extract(record, '$.data.text') as text
         from entries
        where conversation_id = ?
          and json_extract(record, '$.data.taskId') ${taskId === "any" ? "is not null" : "= ?"}
        order by id`,
    )
    .all(...(taskId === "any" ? [conversationId] : [conversationId, taskId])) as Array<{
    id: number;
    kind?: string | null;
    at?: number | null;
    text?: string | null;
  }>;
  for (const event of events) {
    const text = typeof event.text === "string" ? event.text : "";
    if (text.trim() === "") continue;
    out.push({
      id: event.id,
      kind: event.kind ?? "",
      at: event.at ?? 0,
      content: [{ type: "text", text }],
      callId: null,
      isError: false,
    });
  }
  // Two passes into one timeline: the dialog reads them in the order they happened, and `id` is the
  // only clock both kinds of entry share.
  out.sort((a, b) => a.id - b.id);
  return out;
}

/** The rows a stage gets: calls paired with their results, replies in words, thinking dropped. */
export function stepsFromTranscript(rows: RawRow[]): TaskLogStep[] {
  const steps: TaskLogStep[] = [];
  const byCall = new Map<string, TaskLogStep>();
  for (const row of rows) {
    // A result row carries its verdict on the row, not in a block, and it carries nothing else worth
    // drawing: the tool already has a step, this just closes it.
    if (row.kind === "pi.tool-result" && row.callId !== null) {
      const step = byCall.get(row.callId);
      if (step !== undefined) {
        step.state = row.isError ? "failed" : "ok";
        const text = row.content
          .map((block) => (typeof block.text === "string" ? block.text : ""))
          .filter((part) => part !== "")
          .join("\n");
        if (text.trim() !== "") step.output = clip(text, STEP_OUTPUT);
      }
      continue;
    }
    for (const block of row.content) {
      if (row.kind === "pi.assistant" && block.type === "toolCall" && typeof block.name === "string") {
        const args = argsText(block.arguments);
        const step: TaskLogStep = {
          id: row.id,
          kind: "tool",
          at: row.at,
          tool: block.name,
          label: argumentLabel(block.arguments),
          state: "running",
          call: typeof block.id === "string" ? block.id : undefined,
        };
        // Only when there is something to open: a step with no arguments should
        // not carry the key at all, or every row would look expandable.
        if (args !== undefined) step.args = args;
        steps.push(step);
        if (typeof block.id === "string") byCall.set(block.id, step);
      } else if (row.kind.startsWith("app.") && block.type === "text" && typeof block.text === "string" && block.text.trim() !== "") {
        // Our own event line: an approval, a verdict. It reads like a sentence the turn said.
        steps.push({ id: row.id, kind: "say", at: row.at, state: "ok", text: oneLine(block.text, SAY_TEXT) });
      } else if (row.kind === "pi.assistant" && block.type === "text" && typeof block.text === "string" && block.text.trim() !== "") {
        steps.push({ id: row.id, kind: "say", at: row.at, state: "ok", text: oneLine(block.text, SAY_TEXT) });
      }
    }
  }
  return steps;
}

/**
 * The call that started a task: a skill, a delegation, a whole pipeline.
 *
 * It is not in the task's own rows — it belongs to the turn above, which is where the task's `owner`
 * points and where its `callId` comes from. Leaving it out made a pipeline look like it began with
 * `bash`: the one row that says "this ran because the agent picked the 写测试 skill" was the only row
 * missing.
 */
function startStage(db: DatabaseSync, taskId: number): TaskLogStage | null {
  const row = db.prepare(`select conversation_id as conversationId, record from tasks where id = ?`).get(taskId) as
    | { conversationId: number | null; record: string | null }
    | undefined;
  if (row === undefined || row.record === null || row.conversationId === null) return null;
  let record: {
    input?: { callId?: unknown };
    owner?: unknown;
    state?: { status?: string; outcome?: { status?: string } };
  };
  try {
    record = JSON.parse(row.record) as typeof record;
  } catch {
    return null;
  }
  const callId = record.input?.callId;
  const owner = record.owner;
  if (typeof callId !== "string" || typeof owner !== "number") return null;
  // The owner's rows in the same conversation hold the call; the conversation id alone would scan
  // years of turns for one id.
  const steps = stepsFromTranscript(
    transcript(db, row.conversationId, owner),
  );
  const step = steps.find((candidate) => candidate.call === callId);
  if (step === undefined) return null;
  // The call's verdict is the task's: a delegation's result row is written in the parent turn
  // minutes later, and the task's own outcome already says whether it worked. Reading "running"
  // for a task that finished an hour ago would be the dialog lying about the one row it added.
  if (record.state?.status === "terminal") {
    step.state = record.state.outcome?.status === "completed" ? "ok" : "failed";
  }
  return { conversationId: row.conversationId, parent: true, start: true, role: null, steps: [step], more: 0 };
}

/** One stage, trimmed to the tail a dialog can hold. */
function stageSteps(
  db: DatabaseSync,
  conversationId: number,
  taskId: number | "any",
): { steps: TaskLogStep[]; more: number } {
  const steps = stepsFromTranscript(transcript(db, conversationId, taskId));
  if (steps.length <= STAGE_STEPS) return { steps, more: 0 };
  return { steps: steps.slice(-STAGE_STEPS), more: steps.length - STAGE_STEPS };
}

/**
 * Every stage of a task's run: what each child did, or — for a task with no children, which is every
 * ordinary turn — what the task itself did.
 *
 * The parent stage is left out once children exist: a pipeline's own record is the one tool call
 * that started it, and the dialog already shows that as the task's input. The children are where
 * the work is.
 */
export function taskLog(dbPath: string, taskId: number, conversationId: number | null): TaskLogStage[] {
  let db: DatabaseSync;
  try {
    db = new DatabaseSync(dbPath, { readOnly: true });
  } catch {
    return [];
  }
  try {
    const stages: TaskLogStage[] = [];
    const started = startStage(db, taskId);
    if (started !== null) stages.push(started);
    const children = db
      .prepare(`select id from conversations where owner_task_id = ? order by id`)
      .all(taskId) as Array<{ id: number }>;
    for (const child of children) {
      const { steps, more } = stageSteps(db, child.id, "any");
      if (steps.length === 0) continue;
      stages.push({ conversationId: child.id, parent: false, role: systemRole(db, child.id), steps, more });
    }
    if (stages.length === 0 && conversationId !== null) {
      const { steps, more } = stageSteps(db, conversationId, taskId);
      if (steps.length > 0) {
        stages.push({ conversationId, parent: true, role: null, steps, more });
      }
    }
    return stages;
  } catch {
    return [];
  } finally {
    db.close();
  }
}
