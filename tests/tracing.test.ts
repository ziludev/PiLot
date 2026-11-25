import assert from "node:assert/strict";
import { test } from "node:test";
import { createRegistry, defineExtension, type ToolRegistration } from "@earendil-works/pi-durable";
import { instrumentRegistry, linkChildSession, parentSessionOf } from "../src/tracing.ts";

/**
 * The trace store could say how long a turn took and nothing about what it did: tool calls were not
 * spans at all. These pin the two halves of the fix — the call becomes a child span, and it names
 * the conversation, which is the key `durable trace --session` queries.
 */


/**
 * The span recorder from the test, not from the framework: `pi-telemetry` does not export one from
 * a stable path, and what this file asserts is the shape of the spans, not the adapter's.
 */
interface Recorded {
  id: string;
  parentId: string | undefined;
  name: string;
  attributes: Record<string, unknown>;
  status: { status: string; error?: { message?: string } };
}

class Recorder {
  readonly spans: Recorded[] = [];
  private current: string | undefined;
  private next = 0;

  async startSpan<T>(options: { name: string; attributes?: Record<string, unknown> }, run: (span: unknown) => T | Promise<T>): Promise<T> {
    const record: Recorded = {
      id: String((this.next += 1)),
      parentId: this.current,
      name: options.name,
      attributes: { ...options.attributes },
      status: { status: "ok" },
    };
    this.spans.push(record);
    const parent = this.current;
    this.current = record.id;
    const span = {
      setAttributes: (attributes: Record<string, unknown>) => Object.assign(record.attributes, attributes),
      setStatus: (status: Recorded["status"]) => {
        record.status = status;
      },
      addEvent: () => undefined,
      addLink: () => undefined,
      startSpan: this.startSpan.bind(this),
    };
    try {
      return await run(span);
    } finally {
      this.current = parent;
    }
  }
}

function tool(name: string, execute: ToolRegistration["execute"]): ToolRegistration {
  return {
    name,
    description: `${name} in a test`,
    parameters: { type: "object", properties: {} },
    execute,
  } as ToolRegistration;
}

/** The tool as the agent would see it: installed through `registry.install`, which is wrapped. */
function installed(definition: ToolRegistration): ToolRegistration {
  const registry = createRegistry();
  instrumentRegistry(registry);
  registry.install(defineExtension({ name: "fixture", tools: [definition] }));
  const found = registry.snapshot().tools().find((entry) => entry.tool.name === definition.name);
  assert.ok(found !== undefined, "the fixture tool should be registered");
  return found.tool;
}

function api(): never {
  return { conversationId: 67, taskId: 1084, callId: "call-1" } as never;
}

test("a tool call is a child span naming its conversation and result", async () => {
  const memory = new Recorder();
  const call = installed(tool("read", async () => ({ output: [{ type: "text", text: "ok" }] })));

  const result = await memory.startSpan({ name: "turn" }, async (span) => {
    const context = { telemetry: span, abortSignal: undefined } as never;
    return call.execute({}, api(), context);
  });
  assert.deepEqual(result, { output: [{ type: "text", text: "ok" }] });

  const spans = memory.spans;
  const root = spans.find((span) => span.name === "turn");
  const callSpan = spans.find((span) => span.name === "pi.tool.call");
  assert.ok(root !== undefined && callSpan !== undefined);
  assert.equal(callSpan.parentId, root.id, "the call nests under the span the task was in");
  assert.equal(callSpan.attributes["tool.name"], "read");
  assert.equal(callSpan.attributes["session.id"], 67);
  assert.equal(callSpan.attributes["tracing.task.id"], 1084);
  assert.equal(callSpan.attributes["tool.result"], "ok");
  assert.equal(callSpan.status.status, "ok");
});

test("a failing call is marked on the span and still throws unchanged", async () => {
  const memory = new Recorder();
  const boom = new Error("toolbox is gone");
  const call = installed(
    tool("bash", async () => {
      throw boom;
    }),
  );

  await memory.startSpan({ name: "turn" }, async (span) => {
    const context = { telemetry: span, abortSignal: undefined } as never;
    await assert.rejects(() => call.execute({}, api(), context), (error: unknown) => error === boom);
  });

  const callSpan = memory.spans.find((span) => span.name === "pi.tool.call");
  assert.ok(callSpan !== undefined);
  assert.equal(callSpan.status.status, "error");
  assert.match(JSON.stringify(callSpan.status), /toolbox is gone/);
});

test("with no span store the tool still runs, and is not wrapped twice", async () => {
  let ran = 0;
  const call = installed(
    tool("bash", async () => {
      ran += 1;
      return { output: [] };
    }),
  );
  await call.execute({}, api(), {} as never);
  assert.equal(ran, 1);
});

test("a subagent's conversation remembers which conversation asked for it", () => {
  linkChildSession(101, 67);
  assert.equal(parentSessionOf(101), 67);
  // A conversation is never its own parent: that link would loop the trace.
  linkChildSession(67, 67);
  assert.equal(parentSessionOf(67), undefined);
});
