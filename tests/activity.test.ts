import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import test from "node:test";
import { shortRole, spanText, stageLine, subAgentActivity, subAgentStagesSince, type SubAgentActivity } from "../src/activity.ts";

/** When the fixture's in-flight stage started, in wall clock ms. */
const LIVE_START = 3_600_000;

/** The columns the reader touches, so a schema change here shows up as a failing test. */
function store(rows: {
  children: number[];
  entries: Record<number, Array<{ kind: string; toolName?: string; instructions?: string }>>;
  live: number[];
  /** Settled tasks per conversation: the stage's clock and how it ended. */
  settled?: Record<number, { startedAt: number; endedAt: number; outcome?: string }>;
}): string {
  const dir = mkdtempSync(join(tmpdir(), "activity-"));
  const path = join(dir, "agent.sqlite");
  const db = new DatabaseSync(path);
  db.exec("create table conversations (id integer primary key, owner_task_id integer)");
  db.exec("create table entries (id integer primary key autoincrement, conversation_id integer, record text)");
  db.exec("create table tasks (id integer primary key autoincrement, conversation_id integer, status text, record text)");
  for (const id of rows.children) {
    db.prepare("insert into conversations (id, owner_task_id) values (?, 42)").run(id);
  }
  for (const [id, entries] of Object.entries(rows.entries)) {
    for (const entry of entries) {
      const model = entry.kind === "pi.system"
        ? { sections: { instructions: entry.instructions } }
        : entry.toolName === undefined ? { role: "assistant" } : { role: "toolResult", toolName: entry.toolName };
      db.prepare("insert into entries (conversation_id, record) values (?, ?)").run(
        Number(id),
        JSON.stringify({ kind: entry.kind, model: [model] }),
      );
    }
  }
  for (const id of rows.live) {
    // A running task already carries the instant it started, which is what the
    // stage's elapsed time counts from.
    const record = { startedAt: LIVE_START, endedAt: null, state: { status: "running" } };
    db.prepare("insert into tasks (conversation_id, status, record) values (?, 'running', ?)").run(
      id,
      JSON.stringify(record),
    );
  }
  for (const [id, settled] of Object.entries(rows.settled ?? {})) {
    const record = {
      startedAt: settled.startedAt,
      endedAt: settled.endedAt,
      state: { status: "terminal", outcome: { status: settled.outcome ?? "completed" } },
    };
    db.prepare("insert into tasks (conversation_id, status, record) values (?, 'terminal', ?)").run(
      Number(id),
      JSON.stringify(record),
    );
  }
  db.close();
  return path;
}

test("a running pipeline reports its children, their role and last tool", () => {
  const path = store({
    children: [60833, 63983],
    entries: {
      60833: [
        { kind: "pi.system", instructions: "你的角色：写技术设计文档。" },
        { kind: "pi.tool-result", toolName: "bash" },
      ],
      63983: [
        { kind: "pi.system", instructions: "你的角色：为已实现的代码写测试用例。\n更多要求" },
        { kind: "pi.tool-result", toolName: "bash" },
        { kind: "pi.tool-result", toolName: "go test" },
      ],
    },
    live: [63983],
    settled: { 60833: { startedAt: 1_000, endedAt: 241_000 } },
  });
  try {
    const agents = subAgentActivity(path, 42);
    assert.deepEqual(agents, [
      {
        conversationId: 60833,
        role: "写技术设计文档",
        steps: 2,
        tool: "bash",
        live: false,
        startedAt: 1_000,
        endedAt: 241_000,
        state: "done",
      },
      {
        conversationId: 63983,
        role: "为已实现的代码写测试用例",
        steps: 3,
        tool: "go test",
        live: true,
        startedAt: LIVE_START,
        endedAt: null,
        state: "live",
      },
    ]);
  } finally {
    rmSync(join(path, ".."), { recursive: true, force: true });
  }
});

test("a stage reads pending before it starts and failed after it breaks", () => {
  // The dialog draws these three states differently, so the bridge has to tell
  // them apart: nothing run yet, run and broke, run and finished.
  const path = store({
    children: [1, 2, 3],
    entries: {
      1: [{ kind: "pi.system", instructions: "你的角色：设计。" }],
      2: [{ kind: "pi.system", instructions: "你的角色：实现。" }],
      3: [{ kind: "pi.system", instructions: "你的角色：测试。" }],
    },
    live: [],
    settled: {
      2: { startedAt: 5_000, endedAt: 65_000, outcome: "failed" },
      3: { startedAt: 65_000, endedAt: 125_000 },
    },
  });
  try {
    const agents = subAgentActivity(path, 42);
    assert.deepEqual(
      agents.map((agent) => [agent.conversationId, agent.state, agent.startedAt, agent.endedAt]),
      [
        [1, "pending", null, null],
        [2, "failed", 5_000, 65_000],
        [3, "done", 65_000, 125_000],
      ],
    );
  } finally {
    rmSync(join(path, ".."), { recursive: true, force: true });
  }
});

test("an ordinary task with no children reports nothing", () => {
  const path = store({ children: [], entries: {}, live: [] });
  try {
    assert.deepEqual(subAgentActivity(path, 42), []);
  } finally {
    rmSync(join(path, ".."), { recursive: true, force: true });
  }
});

test("a missing store is not an error, just no progress to show", () => {
  assert.deepEqual(subAgentActivity(join(tmpdir(), "no-such-dir", "agent.sqlite"), 42), []);
});

test("a child whose prompt never names a role keeps its id", () => {
  const path = store({
    children: [7],
    entries: { 7: [{ kind: "pi.system", instructions: "Generic prompt without a role line." }] },
    live: [],
  });
  try {
    const [agent] = subAgentActivity(path, 42);
    assert.equal(agent.role, null);
    assert.equal(agent.tool, null);
    assert.equal(agent.steps, 1);
  } finally {
    rmSync(join(path, ".."), { recursive: true, force: true });
  }
});

test("a stage name is the role up to its first stop, not the whole sentence", () => {
  // "按设计文档实现代码，跑通构建" on a card line is a sentence where a name belongs.
  assert.equal(shortRole("按设计文档实现代码，跑通构建"), "按设计文档实现代码");
  assert.equal(shortRole("把需求拆成可执行的步骤，标出依赖、风险和验收标准"), "把需求拆成可执行的步…");
  assert.equal(shortRole(null), "子会话");
  assert.equal(shortRole("   "), "子会话");
});

test("the card sees the stages of this turn, not every stage this chat ever ran", () => {
  // The card is per turn and has no task id, so it reads by conversation plus "started after this
  // turn began". A pipeline from an earlier turn shares the chat and must stay out of it.
  const dir = mkdtempSync(join(tmpdir(), "activity-since-"));
  const path = join(dir, "agent.sqlite");
  const db = new DatabaseSync(path);
  db.exec("create table conversations (id integer primary key, owner_task_id integer)");
  db.exec("create table entries (id integer primary key autoincrement, conversation_id integer, record text)");
  db.exec("create table tasks (id integer primary key autoincrement, conversation_id integer, status text, record text)");
  const chat = 7;
  const insertTask = (id: number, startedAt: number): void => {
    const record = { startedAt, endedAt: null, state: { status: "running" } };
    db.prepare("insert into tasks (id, conversation_id, status, record) values (?, ?, 'running', ?)").run(
      id,
      chat,
      JSON.stringify(record),
    );
  };
  insertTask(42, 20_000); // this turn
  insertTask(41, 1_000); // an earlier turn in the same chat
  for (const [id, owner] of [[100, 42], [101, 42], [90, 41]] as const) {
    db.prepare("insert into conversations (id, owner_task_id) values (?, ?)").run(id, owner);
    db.prepare("insert into entries (conversation_id, record) values (?, ?)").run(
      id,
      JSON.stringify({ kind: "pi.system", model: [{ sections: { instructions: "你的角色：写技术设计文档。" } }] }),
    );
  }
  db.close();
  try {
    const stages = subAgentStagesSince(path, chat, 10_000);
    assert.deepEqual(stages.map((stage) => stage.conversationId), [100, 101]);
    assert.equal(stages[0].role, "写技术设计文档");
    assert.deepEqual(subAgentStagesSince(path, chat, 30_000), []);
    assert.deepEqual(subAgentStagesSince(join(tmpdir(), "no-such-dir", "agent.sqlite"), chat, 0), []);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a stage line says which role, how long, and whether it broke", () => {
  const base = { conversationId: 1, steps: 3, tool: null, role: "按设计文档实现代码，跑通构建" };
  const now = 100 * 60_000;
  const live = { ...base, live: true, startedAt: now - 138_000, endedAt: null, state: "live" } as SubAgentActivity;
  assert.equal(stageLine(live, now), "▸ 按设计文档实现代码 · 2m 18s");
  const done = { ...base, live: false, startedAt: now - 62_000, endedAt: now - 60_000, state: "done" } as SubAgentActivity;
  assert.equal(stageLine(done, now), "✓ 按设计文档实现代码 · 2s");
  const failed = { ...base, live: false, startedAt: now - 9_000, endedAt: now, state: "failed" } as SubAgentActivity;
  assert.equal(stageLine(failed, now), "✗ 按设计文档实现代码 · 失败");
  assert.equal(spanText(-5), "0s");
  assert.equal(spanText(3_900_000), "1h 5m");
});
