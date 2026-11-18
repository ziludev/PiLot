import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import test from "node:test";
import { lastStops } from "../src/turn-status.ts";

/**
 * A store holding just the columns the reader touches: entries of a conversation in the order they
 * were committed, with the stopReason on the assistant message as the engine writes it.
 */
function store(
  rows: Record<number, Array<{ kind: string; stopReason?: string; messages?: number }>>,
): { path: string; dir: string } {
  const dir = mkdtempSync(join(tmpdir(), "turn-status-"));
  const path = join(dir, "agent.sqlite");
  const db = new DatabaseSync(path);
  db.exec("create table entries (id integer primary key autoincrement, conversation_id integer, record text)");
  for (const [conversationId, entries] of Object.entries(rows)) {
    for (const entry of entries) {
      const model = Array.from({ length: entry.messages ?? 1 }, () => ({ role: "assistant" }));
      if (entry.stopReason !== undefined) {
        (model[model.length - 1] as { stopReason?: string }).stopReason = entry.stopReason;
      }
      db.prepare("insert into entries (conversation_id, record) values (?, ?)").run(
        Number(conversationId),
        JSON.stringify({ kind: entry.kind, model }),
      );
    }
  }
  db.close();
  return { path, dir };
}

function marked(rows: Record<number, Array<{ kind: string; stopReason?: string; messages?: number }>>) {
  const { path, dir } = store(rows);
  try {
    return lastStops(path, Object.keys(rows).map(Number));
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

test("a turn that errored is the one marked", () => {
  const stops = marked({
    1: [{ kind: "pi.user" }, { kind: "pi.assistant", stopReason: "error" }],
    2: [{ kind: "pi.user" }, { kind: "pi.assistant", stopReason: "stop" }],
    3: [{ kind: "pi.user" }, { kind: "pi.assistant", stopReason: "aborted" }],
  });
  assert.equal(stops.get(1), "error");
  assert.equal(stops.has(2), false);
  assert.equal(stops.get(3), "aborted");
});

test("a chat still calling tools has not finished, so it says nothing", () => {
  const stops = marked({
    1: [
      { kind: "pi.assistant", stopReason: "toolUse" },
      { kind: "pi.toolResult" },
    ],
  });
  assert.equal(stops.size, 0);
});

test("the newest reply decides, not the first bad one", () => {
  const stops = marked({
    1: [
      { kind: "pi.assistant", stopReason: "error" },
      { kind: "pi.user" },
      { kind: "pi.assistant", stopReason: "stop" },
    ],
  });
  assert.equal(stops.has(1), false);
});

test("the stop reason can sit a few entries back", () => {
  // The engine commits tool results after the assistant message that asked for them, and the reason
  // lives on that earlier message.
  const stops = marked({
    1: [
      { kind: "pi.assistant", stopReason: "error" },
      { kind: "pi.toolResult" },
      { kind: "pi.toolResult" },
    ],
  });
  assert.equal(stops.get(1), "error");
});

test("a store with nothing in it marks nothing", () => {
  assert.equal(marked({}).size, 0);
});

test("a missing store is not an error", () => {
  assert.equal(lastStops(join(tmpdir(), "turn-status-does-not-exist", "agent.sqlite"), [1]).size, 0);
});
