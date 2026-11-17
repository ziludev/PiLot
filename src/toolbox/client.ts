import { spawn, execFile, type ChildProcess } from "node:child_process";
import { existsSync, mkdirSync, readdirSync, rmSync, statSync } from "node:fs";
import { connect, type Socket } from "node:net";
import { dirname, isAbsolute, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { log, logError } from "../log.ts";

/**
 * The client half of `toolbox-go/`: it starts the service, sends one call per
 * connection, and turns the streamed events into the callbacks a tool's
 * `execute()` already uses (`api.output`, `api.diagnostic`).
 *
 * Nothing here falls back to the in-process tool. A toolbox that is down is an
 * error the turn sees, not a silent second code path.
 */

/** One streamed event, as `toolbox-go/internal/proto` writes it. */
type ToolboxEvent = {
  type: "chunk" | "diagnostic" | "result" | "error";
  data?: string;
  skipped?: number;
  severity?: string;
  code?: string;
  message?: string;
  result?: unknown;
  spillPath?: string;
};

/** A call the tool itself refused: the agent turns it into the tool's own error. */
export type ToolboxFailure = {
  /** One of pi-durable's execution error codes: timeout, aborted, spawn_error. */
  code: string;
  message: string;
  /** Output saved to a file that outlives the failure, when there is one. */
  spillPath?: string;
};

/** A call's outcome: the tool's structured result, or the failure it decided on. */
type ToolboxOutcome =
  | { result: Record<string, unknown>; failure?: undefined }
  | { result?: undefined; failure: ToolboxFailure };

type ToolboxDiagnostic = { severity: string; code: string; message: string };

type ToolboxCallOptions = {
  /** The call's working directory, already resolved by the agent. */
  cwd: string;
  callId?: string;
  /** Text to append to the running output, and how much earlier output was dropped. */
  onChunk?: (text: string, skipped?: number) => void;
  onDiagnostic?: (diagnostic: ToolboxDiagnostic) => void;
  /** Aborting closes the socket; the service then kills whatever it started. */
  signal?: AbortSignal;
};

/**
 * The reference reports a call interrupted in flight as `aborted` — both its exec and
 * its file operations use that word — so the tools' model-facing text matches.
 */
export function abortedCall(): Error {
  return Object.assign(new Error("aborted"), { code: "aborted" });
}

const PING_TIMEOUT_MS = 5_000;
const PING_INTERVAL_MS = 50;

function repoRoot(): string {
  return resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
}

/** Newest modification time under `dir`, so a stale binary never serves a test run. */
function newestSource(dir: string): number {
  let newest = 0;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === "bin") continue;
      newest = Math.max(newest, newestSource(path));
      continue;
    }
    if (!entry.name.endsWith(".go") && entry.name !== "go.mod" && entry.name !== "go.sum") continue;
    newest = Math.max(newest, statSync(path).mtimeMs);
  }
  return newest;
}

/**
 * `toolbox-go` is Go, so the binary is built here rather than shipped. It rebuilds
 * when a source file is newer: a stale binary would silently answer with old
 * behaviour, which is exactly what the parity tests exist to catch.
 *
 * A missing `go` is fatal on purpose: these tools have no in-process implementation
 * to fall back to, so a machine without a toolchain needs a prebuilt binary.
 */
async function ensureBinary(bin: string): Promise<void> {
  const built = existsSync(bin) ? statSync(bin).mtimeMs : 0;
  if (built > newestSource(resolve(repoRoot(), "toolbox-go"))) return;
  const moduleDir = resolve(repoRoot(), "toolbox-go");
  await new Promise<void>((done, fail) => {
    execFile("go", ["build", "-o", bin, "."], { cwd: moduleDir }, (error, _stdout, stderr) => {
      if (error) {
        // A missing toolchain is the one failure the person reading the log can fix.
        if ((error as NodeJS.ErrnoException).code === "ENOENT") {
          fail(
            new Error(
              `toolbox: no Go toolchain to build ${bin}, and no prebuilt binary there. ` +
                "Install Go, or put a built toolbox at that path.",
            ),
          );
          return;
        }
        fail(new Error(`toolbox: go build failed: ${stderr || error.message}`));
        return;
      }
      log("toolbox", `built ${bin}`);
      done();
    });
  });
}

export class Toolbox {
  readonly socketPath: string;
  readonly #child: ChildProcess;
  #closed = false;

  private constructor(socketPath: string, child: ChildProcess) {
    this.socketPath = socketPath;
    this.#child = child;
  }

  get pid(): number | undefined {
    return this.#child.pid;
  }

  static async start(options: { bin: string; socketPath: string }): Promise<Toolbox> {
    // The configured binary is relative to the checkout, not to wherever the agent
    // happens to run, so a call from a worktree finds the same build.
    const bin = isAbsolute(options.bin) ? options.bin : resolve(repoRoot(), options.bin);
    await ensureBinary(bin);
    mkdirSync(dirname(options.socketPath), { recursive: true });
    // A socket left by a killed agent would make the new service fail to bind.
    rmSync(options.socketPath, { force: true });

    const child = spawn(bin, ["-socket", options.socketPath], {
      stdio: ["ignore", "pipe", "pipe"],
    });
    const toolbox = new Toolbox(options.socketPath, child);

    // The service writes progress to stdout and failures to stderr, so the agent's
    // log keeps the same split.
    child.stdout?.setEncoding("utf8");
    child.stdout?.on("data", (line: string) => {
      for (const text of line.trimEnd().split("\n")) if (text !== "") log("toolbox", text);
    });
    child.stderr?.setEncoding("utf8");
    child.stderr?.on("data", (line: string) => {
      for (const text of line.trimEnd().split("\n")) if (text !== "") logError("toolbox", text);
    });
    child.on("exit", (code, signal) => {
      // A service that dies while the agent still needs it is a real failure, and
      // the turn should see it rather than a silent in-process substitute.
      if (!toolbox.#closed) logError("toolbox", `service exited code=${code} signal=${signal}`);
    });

    const exited = new Promise<never>((_done, fail) => {
      child.once("exit", (code) => fail(new Error(`toolbox: service exited before it answered (code=${code})`)));
    });
    await Promise.race([toolbox.#awaitReady(), exited]).catch(async (error: Error) => {
      await toolbox.close();
      throw error;
    });
    return toolbox;
  }

  /** Polls until the service answers, so a slow build or a stale socket is a timeout, not a hang. */
  async #awaitReady(): Promise<void> {
    const deadline = Date.now() + PING_TIMEOUT_MS;
    for (;;) {
      try {
        const outcome = await this.call("ping", {}, { cwd: process.cwd() });
        if (outcome.failure !== undefined) throw new Error(outcome.failure.message);
        return;
      } catch (error) {
        if (Date.now() >= deadline) throw new Error(`toolbox: not ready after ${PING_TIMEOUT_MS}ms: ${(error as Error).message}`);
        await new Promise((done) => setTimeout(done, PING_INTERVAL_MS));
      }
    }
  }

  /**
   * Runs one call. Transport and protocol problems throw; a failure the tool decided
   * on comes back as `failure`, because only the tool knows how to report it.
   */
  async call(tool: string, args: unknown, options: ToolboxCallOptions): Promise<ToolboxOutcome> {
    if (this.#closed) throw new Error("toolbox: already closed");
    const socket = await this.#connect(options.signal);
    const request = JSON.stringify({ tool, args, cwd: options.cwd, callId: options.callId });

    return await new Promise<ToolboxOutcome>((done, fail) => {
      let buffer = "";
      const onAbort = () => {
        socket.destroy();
        fail(abortedCall());
      };
      options.signal?.addEventListener("abort", onAbort, { once: true });

      const finish = (error: Error | undefined, outcome?: ToolboxOutcome) => {
        options.signal?.removeEventListener("abort", onAbort);
        socket.destroy();
        if (error) fail(error);
        else if (outcome) done(outcome);
        else fail(new Error("toolbox: call ended without an outcome"));
      };

      socket.setEncoding("utf8");
      socket.on("data", (data: string) => {
        buffer += data;
        for (;;) {
          const newline = buffer.indexOf("\n");
          if (newline === -1) return;
          const line = buffer.slice(0, newline);
          buffer = buffer.slice(newline + 1);
          if (line.trim() === "") continue;
          let event: ToolboxEvent;
          try {
            event = JSON.parse(line) as ToolboxEvent;
          } catch {
            finish(new Error(`toolbox: unreadable event: ${line.slice(0, 200)}`));
            return;
          }
          const ended = this.#handle(event, options, finish);
          if (ended) return;
        }
      });
      socket.on("error", (error) => finish(error));
      socket.on("close", () => finish(new Error("toolbox: connection closed before the call finished")));
      socket.write(`${request}\n`);
    });
  }

  /** Applies one event; returns true when the call has ended. */
  #handle(
    event: ToolboxEvent,
    options: ToolboxCallOptions,
    finish: (error?: Error, outcome?: ToolboxOutcome) => void,
  ): boolean {
    switch (event.type) {
      case "chunk":
        options.onChunk?.(event.data ?? "", event.skipped);
        return false;
      case "diagnostic":
        options.onDiagnostic?.({
          severity: event.severity ?? "info",
          code: event.code ?? "toolbox",
          message: event.message ?? "",
        });
        return false;
      case "result":
        finish(undefined, { result: (event.result ?? {}) as Record<string, unknown> });
        return true;
      case "error": {
        // A failure the tool decided on carries a code the agent maps onto its own
        // error handling; anything else is the toolbox itself misbehaving.
        if (event.code === undefined) {
          finish(new Error(event.message ?? "toolbox: call failed"));
          return true;
        }
        finish(undefined, {
          failure: { code: event.code, message: event.message ?? "", spillPath: event.spillPath },
        });
        return true;
      }
      default:
        finish(new Error(`toolbox: unexpected event ${String((event as { type?: string }).type)}`));
        return true;
    }
  }

  #connect(signal?: AbortSignal): Promise<Socket> {
    return new Promise<Socket>((done, fail) => {
      const socket = connect(this.socketPath);
      const onAbort = () => {
        socket.destroy();
        fail(abortedCall());
      };
      signal?.addEventListener("abort", onAbort, { once: true });
      socket.once("connect", () => {
        signal?.removeEventListener("abort", onAbort);
        done(socket);
      });
      socket.once("error", (error) => {
        signal?.removeEventListener("abort", onAbort);
        fail(error);
      });
    });
  }

  /** Stops the service and waits for it, so no bash child outlives the agent. */
  async close(): Promise<void> {
    if (this.#closed) return;
    this.#closed = true;
    if (this.#child.exitCode !== null || this.#child.signalCode !== null) return;
    const exited = new Promise<void>((done) => this.#child.once("exit", () => done()));
    this.#child.kill("SIGTERM");
    await Promise.race([exited, new Promise<void>((done) => setTimeout(done, 2_000))]);
    if (this.#child.exitCode === null && this.#child.signalCode === null) this.#child.kill("SIGKILL");
  }
}
