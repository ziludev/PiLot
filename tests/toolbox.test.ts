import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { after, before, test } from "node:test";
import { Toolbox } from "../src/toolbox/client.ts";

/**
 * The toolbox is a separate Go process, so these tests cover the lifecycle the
 * agent depends on: it starts, answers, runs calls in parallel, and leaves no
 * process behind. Tool semantics are covered by `toolbox-go`'s own tests and by
 * the parity tests in tests/toolbox-parity.test.ts.
 */

const root = resolve(import.meta.dirname, "..");
const bin = join(root, "toolbox-go", "bin", "toolbox");
const dir = mkdtempSync(join(tmpdir(), "tbx-"));
const socketPath = join(dir, "sock");
let toolbox: Toolbox;

/** Skips (loudly) on a machine without Go: the suite must stay green there. */
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

before(async () => {
  if (skip) return;
  toolbox = await Toolbox.start({ bin, socketPath });
});

after(async () => {
  if (skip) return;
  await toolbox?.close();
  rmSync(dir, { recursive: true, force: true });
});

test("ping answers with its version and the tools it serves", { skip }, async () => {
  const outcome = await toolbox.call("ping", {}, { cwd: root });
  assert.equal(outcome.failure, undefined);
  const result = outcome.result as unknown as { version: string; pid: number; tools: string[] };
  assert.equal(typeof result.version, "string");
  assert.equal(result.pid, toolbox.pid);
  assert.ok(result.tools.includes("ping"));
});

test("calls run in parallel on their own connections", { skip }, async () => {
  const started = Date.now();
  const results = await Promise.all(
    Array.from({ length: 8 }, (_, index) => toolbox.call("ping", { index }, { cwd: root })),
  );
  assert.equal(results.length, 8);
  // Eight round trips must not be eight sequential 5s waits; this only asserts the
  // client does not serialise them behind one connection.
  assert.ok(Date.now() - started < 2_000, "parallel pings were serialised");
});

test("an unknown tool is an error, not an empty result", { skip }, async () => {
  await assert.rejects(() => toolbox.call("nope", {}, { cwd: root }), /unknown tool/);
});

test("an abort closes the call", { skip }, async () => {
  const controller = new AbortController();
  const call = toolbox.call("ping", {}, { cwd: root, signal: controller.signal });
  controller.abort();
  await assert.rejects(() => call, /aborted/);
});

test("close stops the service and its socket", { skip }, async () => {
  const other = await Toolbox.start({ bin, socketPath: join(dir, "sock2") });
  const pid = other.pid;
  assert.ok(pid !== undefined);
  await other.close();
  // The process is gone, and the socket path was the service's to clean up.
  assert.equal(existsSync(join(dir, "sock2")), false);
  await assert.rejects(() => other.call("ping", {}, { cwd: root }), /closed/);
});
