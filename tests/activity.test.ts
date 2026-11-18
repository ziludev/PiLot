import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import test from "node:test";
import { subAgentActivity } from "../src/activity.ts";

/** The columns the reader touches, so a schema change here shows up as a failing test. */
function store(rows: {
  children: number[];
  entries: Record<number, Array<{ kind: string; toolName?: string; instructions?: string }>>;
  live: number[];
}): string {
  const dir = mkdtempSync(join(tmpdir(), "activity-"));
  const path = join(dir, "agent.sqlite");
  const db = new DatabaseSync(path);
  db.exec("create table conversations (id integer primary key, owner_task_id integer)");
  db.exec("create table entries (id integer primary key autoincrement, conversation_id integer, record text)");
  db.exec("create table tasks (id integer primary key autoincrement, conversation_id integer, status text)");
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
    db.prepare("insert into tasks (conversation_id, status) values (?, 'running')").run(id);
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
  });
  try {
    const agents = subAgentActivity(path, 42);
    assert.deepEqual(agents, [
      { conversationId: 60833, role: "写技术设计文档", steps: 2, tool: "bash", live: false },
      { conversationId: 63983, role: "为已实现的代码写测试用例", steps: 3, tool: "go test", live: true },
    ]);
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
