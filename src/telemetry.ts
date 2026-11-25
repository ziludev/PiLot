import { join, dirname } from "node:path";
import { createSqliteTelemetry, type SqliteTelemetry } from "@earendil-works/chord/telemetry-sqlite";
import { openNodeSqliteDatabase } from "@earendil-works/chord/sqlite/node";
import { log, logError } from "./log.ts";

/**
 * Spans for every durable task, model request, submission and tool call, in their own SQLite file
 * beside the agent's storage.
 *
 * Without this, "why did that turn take 11 minutes" could only be answered by reading entry
 * timestamps after the fact. `durable trace` reads the same file read-only, so the cost of a slow
 * turn can be located while the agent keeps running. `tracing.ts` is what puts a conversation's id
 * on those spans, which is what makes `durable trace --session` possible.
 */
function telemetryPath(dbPath: string): string {
  return join(dirname(dbPath), "telemetry.sqlite");
}

export async function openTelemetry(dbPath: string): Promise<SqliteTelemetry | undefined> {
  const path = telemetryPath(dbPath);
  try {
    const db = await openNodeSqliteDatabase(path);
    const telemetry = await createSqliteTelemetry(db, {});
    log("telemetry", "recording spans", { path });
    return telemetry;
  } catch (error) {
    // Observability must never be the reason the agent does not start.
    logError("telemetry", "could not open the span store; continuing without spans", error);
    return undefined;
  }
}

export interface TraceSummary {
  traceId: string;
  name: string;
  startedMs: number;
  ms: number;
  spans: number;
  errors: number;
}

type StoredTrace = Awaited<ReturnType<Awaited<ReturnType<typeof openStoreFor>>["getTrace"]>>;

async function openStoreFor(dbPath: string) {
  const { openTelemetryStore } = await import("@earendil-works/chord/telemetry-sqlite/node");
  return openTelemetryStore(telemetryPath(dbPath));
}

function summarize(trace: NonNullable<StoredTrace>): TraceSummary {
  const spans = [...trace.spans];
  const start = Math.min(...spans.map((span) => span.startedMs));
  const end = Math.max(...spans.map((span) => span.endedMs));
  return {
    traceId: trace.traceId,
    name: spans[0]?.name ?? "(empty)",
    startedMs: start,
    ms: end - start,
    spans: spans.length,
    errors: spans.filter((span) => span.status.status === "error").length,
  };
}

/** Reads recent traces for the CLI; read-only, so it works while the agent is running. */
export async function readTraces(dbPath: string, limit: number): Promise<TraceSummary[]> {
  const store = await openStoreFor(dbPath);
  try {
    const { traces } = await store.findTraces({ limit });
    return traces.map(summarize);
  } finally {
    await store.close();
  }
}

/**
 * Traces that belong to one conversation, newest first: matched on the `session.id` attribute the
 * generation hook stamps. A subagent's turn is found by its own id, and its spans carry
 * `parent.session.id` so the conversation that spawned it can be followed too.
 */
export async function readTracesForSession(
  dbPath: string,
  session: number,
  limit: number,
): Promise<TraceSummary[]> {
  const store = await openStoreFor(dbPath);
  try {
    const { traces } = await store.findTraces({ attributes: { "session.id": session }, limit });
    return traces.map(summarize);
  } finally {
    await store.close();
  }
}

/** One trace's spans, for the tree `durable trace --id` prints. */
export async function readTrace(dbPath: string, traceId: string): Promise<StoredTrace> {
  const store = await openStoreFor(dbPath);
  try {
    return await store.getTrace(traceId);
  } finally {
    await store.close();
  }
}
