import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { bridgeVersion, healthReport } from "../src/health.ts";

/**
 * /api/health is what tells the desktop client whether the agent behind the
 * bridge is alive. The client reads a failed request as "the agent is gone", so
 * this report has to come back even when the harness behind it does not: a
 * health check that throws is a false alarm about a bridge that is fine.
 */
type Runtime = Parameters<typeof healthReport>[0];

function runtimeWith(parts: {
  submissions?: Array<{ status: string }>;
  tasks?: Array<{ state: { kind: string } }>;
  chats?: Record<string, number>;
  fail?: boolean;
}): Runtime {
  const harness = {
    inspect: async () => {
      if (parts.fail === true) throw new Error("harness is down");
      return {
        submissions: parts.submissions ?? [],
        tasks: parts.tasks ?? [],
      };
    },
    snapshot: async () => {
      if (parts.fail === true) throw new Error("harness is down");
      return { chats: parts.chats ?? {} };
    },
  };
  return { harness } as unknown as Runtime;
}

const context = {} as Parameters<typeof healthReport>[1];

test("the report counts the queue, the running turns and the chats", async () => {
  const started = 1_700_000_000_000;
  const report = await healthReport(
    runtimeWith({
      submissions: [{ status: "queued" }, { status: "placed" }, { status: "queued" }],
      tasks: [
        { state: { kind: "running" } },
        { state: { kind: "sleeping" } },
        { state: { kind: "running" } },
        { state: { kind: "done" } },
      ],
      // Two surfaces on one conversation: a topic and the chat it grew from.
      chats: { oc_a: 7, oc_b: 7, oc_c: 9 },
    }),
    context,
    started,
    started + 11_520_000,
  );

  assert.equal(report.queueDepth, 3);
  assert.equal(report.runningTurns, 2);
  assert.equal(report.conversations, 2);
  assert.equal(report.pid, process.pid);
  assert.equal(report.startedAt, new Date(started).toISOString());
  assert.equal(report.uptimeMs, 11_520_000);
  assert.equal(report.version, bridgeVersion());
});

test("the version is the one this bridge was built from", () => {
  const pkg = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8")) as { version: string };
  assert.equal(bridgeVersion(), pkg.version);
});

test("a broken harness comes back as zeroes, not as an error", async () => {
  const report = await healthReport(runtimeWith({ fail: true }), context, Date.now());
  assert.equal(report.queueDepth, 0);
  assert.equal(report.runningTurns, 0);
  assert.equal(report.conversations, 0);
  assert.equal(report.uptimeMs, 0);
});
