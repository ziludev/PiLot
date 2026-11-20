import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import test from "node:test";
import { argumentLabel, taskLog } from "../src/activity.ts";

/**
 * The task dialog's step list, read from the store. What these tests pin: a call is paired with its
 * result (the verdict is why the row exists at all), a neighbouring turn's work stays out, and a
 * stage longer than a dialog keeps its tail — a run ends where the trouble is.
 */

type Row = Record<string, unknown>;

/** An assistant row with one content block, the way the store keeps a call or a reply. */
function assistant(content: Row[], at = 1000): Row {
  return { kind: "pi.assistant", model: [{ role: "assistant", timestamp: at, content }] };
}

function call(id: string, tool: string, args: Row): Row {
  return assistant([{ type: "toolCall", id, name: tool, arguments: args }]);
}

function result(id: string, isError: boolean): Row {
  return {
    kind: "pi.tool-result",
    model: [{ role: "toolResult", toolCallId: id, isError, timestamp: 1001, content: [{ type: "text", text: isError ? "exit 1" : "ok" }] }],
  };
}

function say(text: string): Row {
  return assistant([{ type: "thinking", thinking: "not drawn" }, { type: "text", text }]);
}

/**
 * A store with one parent task, its child conversations, and the rows each conversation kept for
 * the task that produced them. `byTaskId` is on the row, so the reader can pick a stage's work out
 * of a conversation that has run many turns.
 */
class Store {
  readonly path: string;
  private readonly dir: string;
  private readonly db: DatabaseSync;

  constructor() {
    this.dir = mkdtempSync(join(tmpdir(), "task-log-"));
    this.path = join(this.dir, "agent.sqlite");
    this.db = new DatabaseSync(this.path);
    this.db.exec("create table conversations (id integer primary key, owner_task_id integer)");
    this.db.exec("create table entries (id integer primary key autoincrement, conversation_id integer, record text)");
    this.db.exec("create table tasks (id integer primary key autoincrement, conversation_id integer, status text, record text)");
  }

  /** A task in a conversation, returning the id the store gave it. */
  task(conversationId: number, record: Row = {}): number {
    const info = this.db
      .prepare("insert into tasks (conversation_id, status, record) values (?, 'terminal', ?)")
      .run(conversationId, JSON.stringify(record));
    return Number(info.lastInsertRowid);
  }

  /** Rows for a conversation, tagged with the task that produced them. */
  rows(conversationId: number, taskId: number, rows: Row[]): this {
    for (const row of rows) {
      this.db
        .prepare("insert into entries (conversation_id, record) values (?, ?)")
        .run(conversationId, JSON.stringify({ ...row, byTaskId: taskId }));
    }
    return this;
  }

  /**
   * A child conversation owned by a task, plus the task it ran there. A stage is read as
   * "this conversation's tasks", so the fixture has to give the conversation one.
   */
  child(conversationId: number, ownerTaskId: number): number {
    this.db.prepare("insert into conversations (id, owner_task_id) values (?, ?)").run(conversationId, ownerTaskId);
    return this.task(conversationId);
  }

  close(): void {
    this.db.close();
    rmSync(this.dir, { recursive: true, force: true });
  }
}

test("a stage reads its calls in order, paired with their results", () => {
  const store = new Store();
  const parent = store.task(1);
  store.rows(2, store.child(2, parent), [
    call("c1", "bash", { command: "go test ./...", description: "run the tests" }),
    result("c1", true),
    say("The build is red in internal/app."),
    call("c2", "read", { path: "/tmp/x.go" }),
    result("c2", false),
  ]);

  const stages = taskLog(store.path, parent, 1);
  store.close();

  assert.equal(stages.length, 1, "one child conversation, one stage");
  const [stage] = stages;
  assert.equal(stage?.conversationId, 2);
  assert.equal(stage?.parent, false);
  assert.equal(stage?.more, 0);
  assert.deepEqual(
    (stage?.steps ?? []).map((step) => [step.kind, step.tool ?? null, step.label ?? step.text ?? null, step.state]),
    [
      ["tool", "bash", "go test ./...", "failed"],
      ["say", null, "The build is red in internal/app.", "ok"],
      ["tool", "read", "/tmp/x.go", "ok"],
    ],
  );
});

test("a call with no result yet is still running, and thinking is not a step", () => {
  const store = new Store();
  const parent = store.task(1);
  store.rows(2, store.child(2, parent), [call("c1", "bash", { command: "sleep 30" })]);

  const stages = taskLog(store.path, parent, 1);
  store.close();

  const steps = stages[0]?.steps ?? [];
  assert.equal(steps.length, 1);
  assert.equal(steps[0]?.state, "running");
});

test("a stage that ran longer than a dialog keeps its tail and says how many it left out", () => {
  const store = new Store();
  const parent = store.task(1);
  const rows: Row[] = [];
  for (let i = 0; i < 60; i += 1) rows.push(call(`c${i}`, "bash", { command: `step ${i}` }));
  store.rows(2, store.child(2, parent), rows);

  const stages = taskLog(store.path, parent, 1);
  store.close();

  const stage = stages[0];
  assert.equal(stage?.steps.length, 40, "a stage is capped at forty rows");
  assert.equal(stage?.more, 20);
  assert.equal(stage?.steps.at(-1)?.label, "step 59", "and the tail is what survives");
});

test("a task with no children shows its own turn, and only that turn", () => {
  const store = new Store();
  // Two turns in the same conversation: the second is the task under test. Reading the
  // conversation instead of the task would mix them, and the parent chat has years of turns.
  const earlier = store.task(9);
  const turn = store.task(9);
  store.rows(9, earlier, [call("old", "bash", { command: "an earlier turn" })]);
  store.rows(9, turn, [call("new", "bash", { command: "this turn" })]);

  const stages = taskLog(store.path, turn, 9);
  store.close();

  assert.equal(stages.length, 1);
  const [stage] = stages;
  assert.equal(stage?.parent, true);
  assert.deepEqual(stage?.steps.map((step) => step.label), ["this turn"]);
});

test("a task with nothing recorded says nothing rather than guessing", () => {
  const store = new Store();
  const task = store.task(9);

  const stages = taskLog(store.path, task, 9);
  store.close();

  assert.deepEqual(stages, []);
});

test("a missing store is an empty answer, not a throw", () => {
  assert.deepEqual(taskLog(join(tmpdir(), "no-such-store", "agent.sqlite"), 1, 1), []);
});

test("the call that started the task comes first, with the task's own verdict", () => {
  const store = new Store();
  // The turn above made the call; the task it started points back at that turn
  // with `owner` and at the call with `callId`. That is the only way to say
  // "this ran because the agent picked delegate", which the task's own rows
  // cannot say — they start after the decision.
  const turn = store.task(9);
  const task = store.task(9, {
    conversationId: 9,
    input: { callId: "c1" },
    owner: turn,
    state: { status: "terminal", outcome: { status: "completed" } },
  });
  store.child(2, task);
  store.rows(9, turn, [call("c1", "delegate", { task: "write the tests for the drawer" })]);
  store.rows(2, store.task(2), [call("inner", "bash", { command: "go test ./..." })]);

  const stages = taskLog(store.path, task, 9);
  store.close();

  assert.equal(stages.length, 2, "the start row and one stage");
  const start = stages[0];
  assert.equal(start?.start, true);
  assert.equal(start?.steps[0]?.tool, "delegate");
  assert.equal(start?.steps[0]?.label, "write the tests for the drawer");
  assert.equal(start?.steps[0]?.state, "ok", "a finished task's call is not still running");
});

test("a task nobody called — a routine — has no start row", () => {
  const store = new Store();
  const task = store.task(9, { conversationId: 9, state: { status: "terminal", outcome: { status: "completed" } } });
  store.rows(9, task, [call("c1", "bash", { command: "git log" })]);

  const stages = taskLog(store.path, task, 9);
  store.close();

  assert.equal(stages.length, 1);
  assert.equal(stages[0]?.start, undefined);
});

test("an approval and a verdict show up in the task that hit them", () => {
  const store = new Store();
  const turn = store.task(9);
  store.child(2, turn);
  store.rows(9, turn, [
    call("c1", "bash", { command: "rm -rf build" }),
    result("c1", false),
  ]);
  // Written by the bridge, not by the model: an approval is answered outside the task's own commit
  // scope, so the task it belongs to rides in `data` and the row has no model to read.
  store.rows(9, turn, [
    { kind: "app.approval", data: { taskId: turn, text: "bash：rm -rf build → 已批准", at: 1002 } },
    { kind: "app.done", data: { taskId: turn, text: "目标达成：删干净了", at: 1003 } },
  ]);

  const stages = taskLog(store.path, turn, 9);
  store.close();

  const labels = (stages[0]?.steps ?? []).map((step) => [step.kind, step.label ?? step.text]);
  assert.deepEqual(labels, [
    ["tool", "rm -rf build"],
    ["say", "bash：rm -rf build → 已批准"],
    ["say", "目标达成：删干净了"],
  ]);
});
