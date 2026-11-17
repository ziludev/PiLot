import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { after, before, test } from "node:test";
import { createBashTool as createInProcessBash } from "@earendil-works/pi-durable/tools";
import { NodeExecutionEnv } from "@earendil-works/pi-durable/env/node";
import type { ToolExecutionApi, ToolExecutionResult } from "@earendil-works/pi-durable";
import { Toolbox } from "../src/toolbox/client.ts";
import { createBashTool as createToolboxBash } from "../src/toolbox/tools/bash.ts";
import { DEFAULT_MAX_BYTES, DEFAULT_MAX_LINES } from "../src/toolbox/limits.ts";

/**
 * The parity suite: one command, two implementations, the same model-visible result.
 *
 * The reference is pi-durable's own `bash` tool driven through a real
 * `NodeExecutionEnv`; the candidate is the same tool shape served by the Go
 * toolbox. Anything the model could notice — output text, truncation, exit code,
 * spill file, diagnostics, error text — is compared field by field.
 */

const root = resolve(import.meta.dirname, "..");
const bin = join(root, "toolbox-go", "bin", "toolbox");
const dir = mkdtempSync(join(tmpdir(), "tbx-parity-"));
const work = join(dir, "work");
const socketPath = join(dir, "sock");
let toolbox: Toolbox;

function haveGo(): boolean {
  if (existsSync(bin)) return true;
  try {
    execFileSync("go", ["version"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

const skip = haveGo() ? false : "no go toolchain and no prebuilt toolbox-go/bin/toolbox";

/** What one run looked like, in the terms the model and the transcript see. */
type Seen = {
  text: string;
  truncated: boolean;
  output: string | undefined;
  exitCode: number | undefined;
  spill: { path: string; content: string } | undefined;
  diagnostics: { severity: string; code: string; message: string }[];
  isError: boolean;
  error: string | undefined;
};

/**
 * The tail the harness would keep. Both runs go through this same helper, so the
 * comparison answers "does the Go path stream the same text", not "is the harness
 * truncation correct" — the harness is shared code either way.
 */
function retainTail(text: string): { text: string; truncated: boolean } {
  const lines = text.split("\n");
  let kept = text;
  let truncated = false;
  if (lines.length > DEFAULT_MAX_LINES) {
    kept = lines.slice(lines.length - DEFAULT_MAX_LINES).join("\n");
    truncated = true;
  }
  if (Buffer.byteLength(kept, "utf8") > DEFAULT_MAX_BYTES) {
    kept = kept.slice(kept.length - DEFAULT_MAX_BYTES);
    truncated = true;
  }
  return { text: kept, truncated };
}

/** The api a tool's `execute()` sees, reduced to what these tools touch. */
function fakeApi(cwd: string, seen: Seen): ToolExecutionApi {
  const chunks: string[] = [];
  const env = new NodeExecutionEnv({ cwd });
  return {
    env,
    output: (chunk: string) => {
      chunks.push(chunk);
    },
    outputWindow: undefined,
    retainedOutput: () => {
      const retained = retainTail(chunks.join(""));
      seen.text = retained.text;
      seen.truncated = retained.truncated;
      return retained;
    },
    diagnostic: (diagnostic: { severity: string; code: string; message: string }) => {
      seen.diagnostics.push(diagnostic);
    },
  } as unknown as ToolExecutionApi;
}

async function run(tool: { execute: Function }, command: string, timeout?: number): Promise<Seen> {
  const seen: Seen = {
    text: "",
    truncated: false,
    output: undefined,
    exitCode: undefined,
    spill: undefined,
    diagnostics: [],
    isError: false,
    error: undefined,
  };
  const api = fakeApi(work, seen);
  try {
    const result = (await tool.execute(
      { command, ...(timeout === undefined ? {} : { timeout }) },
      api,
      {},
    )) as ToolExecutionResult;
    const structured = result.structuredOutput as {
      output: string;
      truncated: boolean;
      fullOutputPath?: string;
      exitCode: number;
    };
    seen.diagnostics.push(...(result.diagnostics ?? []));
    seen.output = structured.output;
    seen.truncated = structured.truncated;
    seen.exitCode = structured.exitCode;
    seen.isError = result.isError === true;
    if (structured.fullOutputPath !== undefined) {
      seen.spill = { path: structured.fullOutputPath, content: readFileSync(structured.fullOutputPath, "utf8") };
    }
  } catch (error) {
    seen.error = (error as Error).message;
  }
  return seen;
}

/** Diagnostics name the spill file, whose random name differs by design. */
function redact(seen: Seen): Seen {
  return {
    ...seen,
    spill: seen.spill === undefined ? undefined : { path: "<spill>", content: seen.spill.content },
    diagnostics: seen.diagnostics.map((diagnostic) => ({ ...diagnostic, message: diagnostic.message.replace(/\/\S*pi-output-\S*\.log/g, "<spill>") })),
  };
}

before(async () => {
  if (skip) return;
  const { mkdirSync } = await import("node:fs");
  mkdirSync(work, { recursive: true });
  toolbox = await Toolbox.start({ bin, socketPath });
});

after(async () => {
  if (skip) return;
  await toolbox?.close();
  rmSync(dir, { recursive: true, force: true });
});

/** Runs both paths and asserts they agree, returning the shared outcome. */
async function parity(command: string, timeout?: number): Promise<Seen> {
  const reference = await run(createInProcessBash(), command, timeout);
  const candidate = await run(createToolboxBash(toolbox), command, timeout);
  assert.deepEqual(redact(candidate), redact(reference), `paths disagree for: ${command}`);
  for (const seen of [reference, candidate]) if (seen.spill !== undefined) rmSync(seen.spill.path, { force: true });
  return reference;
}

test("parity: plain output and exit code", { skip }, async () => {
  const seen = await parity(`echo hello`);
  assert.equal(seen.text, "hello\n");
  assert.equal(seen.exitCode, 0);
  assert.equal(seen.isError, false);
});

test("parity: both streams reach the model", { skip }, async () => {
  // Which stream lands first is the child's business, so this one compares the lines
  // as a set instead of byte for byte.
  const command = `echo out; echo err >&2`;
  const reference = await run(createInProcessBash(), command);
  const candidate = await run(createToolboxBash(toolbox), command);
  const lines = (seen: Seen) => seen.text.split("\n").filter(Boolean).sort();
  assert.deepEqual(lines(candidate), lines(reference));
  assert.deepEqual(lines(reference), ["err", "out"]);
  assert.equal(candidate.exitCode, reference.exitCode);
});

/**
 * What one run streamed, before any retention: every call the tool made to `api.output` and the
 * second argument it passed. The reference bash tool hands its window to `env.exec`, but
 * `NodeExecutionEnv` never reads `options.window`, so nothing is skipped on either path — this
 * test pins that down rather than assuming it.
 */
type Streamed = { text: string; calls: number; bytes: number; skipped: unknown[] };

async function stream(tool: ReturnType<typeof createInProcessBash>, command: string): Promise<Streamed> {
  const parts: string[] = [];
  const skipped: unknown[] = [];
  const api = {
    env: new NodeExecutionEnv({ cwd: work }),
    output: (chunk: string, skip?: unknown) => {
      parts.push(chunk);
      skipped.push(skip);
    },
    // A window the tool would pass down; the env ignores it, so output must still arrive whole.
    outputWindow: { maxBytes: DEFAULT_MAX_BYTES, maxLines: DEFAULT_MAX_LINES, minIntervalMs: 0, bytesPerSecond: 1 },
    retainedOutput: () => ({ text: parts.join(""), truncated: false }),
    diagnostic: () => {},
    details: undefined,
  } as unknown as ToolExecutionApi;
  await tool.execute({ command }, api, {});
  const text = parts.join("");
  return { text, calls: parts.length, bytes: Buffer.byteLength(text, "utf8"), skipped };
}

test("parity: a long stream arrives whole on both paths", { skip }, async () => {
  const command = `seq 1 40000`;
  const reference = await stream(createInProcessBash(), command);
  const candidate = await stream(createToolboxBash(toolbox), command);
  assert.equal(reference.text, candidate.text, "the streamed text is the same");
  assert.ok(reference.bytes > 200 * 1024, `the output is large enough to matter: ${reference.bytes}`);
  assert.deepEqual(candidate.skipped.filter((value) => value !== undefined), [], "the Go path skips nothing");
  assert.deepEqual(reference.skipped.filter((value) => value !== undefined), [], "neither does the reference");
});

test("parity: a nonzero exit is an error result with the code", { skip }, async () => {
  const seen = await parity(`exit 7`);
  assert.equal(seen.exitCode, 7);
  assert.equal(seen.isError, true);
  assert.deepEqual(seen.diagnostics, [{ severity: "error", code: "exit_code", message: "Command exited with code 7" }]);
});

test("parity: large output is truncated and spilled identically", { skip }, async () => {
  const seen = await parity(`seq 1 3000`);
  assert.equal(seen.truncated, true);
  assert.ok(seen.output?.endsWith("3000\n"), "the retained tail ends at the last line");
  assert.ok(seen.spill !== undefined, "a 3000-line output is spilled");
  assert.ok(seen.spill.content.startsWith("1\n") && seen.spill.content.endsWith("3000\n"), "the spill is complete");
  assert.deepEqual(seen.diagnostics, [{ severity: "info", code: "full_output", message: `Full output: ${seen.spill.path}` }]);
});

test("parity: output under the threshold is not spilled", { skip }, async () => {
  const seen = await parity(`seq 1 100`);
  assert.equal(seen.truncated, false);
  assert.equal(seen.spill, undefined);
});

test("parity: a timeout reads the same", { skip }, async () => {
  const seen = await parity(`sleep 5`, 0.2);
  assert.equal(seen.error, "Command timed out after 0.2 seconds");
});

test("parity: multibyte output survives the stream", { skip }, async () => {
  const seen = await parity(`printf '中文\\n'`);
  assert.equal(seen.text, "中文\n");
});

test("parity: malformed UTF-8 is replaced the way TextDecoder does it", { skip }, async () => {
  // One replacement per malformed subpart, not per byte: the acceptance contract names this case.
  const cases: Array<[string, string]> = [
    [`printf '\\357\\273A'`, "\uFFFDA"],
    [`printf 'A\\360\\237\\222B'`, "A\uFFFDB"],
    [`printf 'A\\342\\202'`, "A\uFFFD"],
    [`printf '\\377\\376'`, "\uFFFD\uFFFD"],
  ];
  for (const [command, want] of cases) {
    const seen = await parity(command);
    assert.equal(seen.text, want, command);
  }
});

test("parity: a leading byte-order mark is dropped and an inner one kept", { skip }, async () => {
  assert.equal((await parity(`printf '\\357\\273\\277hello'`)).text, "hello");
  assert.equal((await parity(`printf 'a\\357\\273\\277b'`)).text, "a\uFEFFb");
});

test("parity: a missing working directory reads the same", { skip }, async () => {
  const missing = join(dir, "gone");
  const reference = await runIn(createInProcessBash(), missing);
  const candidate = await runIn(createToolboxBash(toolbox), missing);
  assert.equal(candidate.error, reference.error);
  assert.match(reference.error ?? "", /Working directory does not exist/);
});

/** Same as `run`, but in a directory that does not exist. */
async function runIn(tool: { execute: Function }, cwd: string): Promise<Seen> {
  const seen: Seen = {
    text: "",
    truncated: false,
    output: undefined,
    exitCode: undefined,
    spill: undefined,
    diagnostics: [],
    isError: false,
    error: undefined,
  };
  try {
    await tool.execute({ command: "true" }, fakeApi(cwd, seen), {});
  } catch (error) {
    seen.error = (error as Error).message;
  }
  return seen;
}
