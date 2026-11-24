import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { adoptTarget, listStrays } from "../src/stray.ts";

const run = promisify(execFile);

async function git(dir: string, args: string[]): Promise<string> {
  const { stdout } = await run("git", ["-C", dir, ...args]);
  return stdout;
}

/** A repo on main, plus one stray checkout on agent/conv-42 with a commit main lacks. */
async function fixture(): Promise<{ root: string; stray: string; clean: string }> {
  const root = await mkdtemp(join(tmpdir(), "stray-"));
  await git(root, ["init", "-q", "-b", "main"]);
  await git(root, ["config", "user.email", "t@example.com"]);
  await git(root, ["config", "user.name", "t"]);
  await writeFile(join(root, "README.md"), "hi\n");
  await git(root, ["add", "."]);
  await git(root, ["commit", "-qm", "first"]);

  await mkdir(join(root, ".worktrees"));
  const stray = join(root, ".worktrees", "conv-42");
  await git(root, ["worktree", "add", "-q", "-b", "agent/conv-42", stray, "main"]);
  await writeFile(join(stray, "work.txt"), "one\n");
  await git(stray, ["add", "."]);
  await git(stray, ["commit", "-qm", "work"]);

  const clean = join(root, ".worktrees", "conv-43");
  await git(root, ["worktree", "add", "-q", "-b", "agent/conv-43", clean, "main"]);
  return { root, stray, clean };
}

test("listStrays reads a checkout's branch, distance and dirt", async () => {
  const { root, stray, clean } = await fixture();
  await writeFile(join(clean, "dirty.txt"), "uncommitted\n");
  const rows = await listStrays(root, "main", new Set([7]));
  assert.equal(rows.length, 2);
  const one = rows.find((row) => row.dir === stray);
  const two = rows.find((row) => row.dir === clean);
  assert.equal(one?.branch, "agent/conv-42");
  assert.equal(one?.conversationId, 42);
  assert.equal(one?.ahead, 1);
  assert.equal(one?.behind, 0);
  assert.equal(one?.dirty, false);
  assert.equal(one?.merged, false);
  assert.equal(two?.branch, "agent/conv-43");
  assert.equal(two?.ahead, 0);
  assert.equal(two?.dirty, true);
  assert.equal(two?.merged, false, "an uncommitted file means nothing was merged");
});

test("listStrays marks who still owns a checkout", async () => {
  const { root } = await fixture();
  const rows = await listStrays(root, "main", new Set([42]));
  assert.equal(rows.find((row) => row.conversationId === 42)?.occupied, true);
  assert.equal(rows.find((row) => row.conversationId === 43)?.occupied, false);
});

test("adoptTarget refuses what is not this project's worktree", async () => {
  const { root, stray } = await fixture();
  const outside = await mkdtemp(join(tmpdir(), "outside-"));
  assert.deepEqual(await adoptTarget(root, join(outside, "conv-1")), { error: "that directory is gone" });
  assert.deepEqual(await adoptTarget(root, root), { error: "not a worktree of this project" });
  assert.deepEqual(await adoptTarget(root, join(root, ".worktrees")), { error: "not a worktree of this project" });
  const target = await adoptTarget(root, stray);
  assert.equal("branch" in target && target.branch, "agent/conv-42");

  // A directory that exists inside .worktrees but is not a checkout is refused too.
  const fake = join(root, ".worktrees", "conv-99");
  await mkdir(fake);
  assert.deepEqual(await adoptTarget(root, fake), { error: "that directory is not a checkout" });
});

test("adoptTarget refuses a detached head", async () => {
  const { root, stray } = await fixture();
  const head = (await git(stray, ["rev-parse", "HEAD"])).trim();
  await git(stray, ["checkout", "-q", head]);
  assert.deepEqual(await adoptTarget(root, stray), { error: "that checkout is on a detached head" });
});
