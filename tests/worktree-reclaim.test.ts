import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { test } from "node:test";
import { ensureWorktree, reclaimWorktrees, worktreePaths } from "../src/worktree.ts";

const run = promisify(execFile);

/** A repo with one commit on main: what every `ensureWorktree` here branches from. */
async function scratchRepo(): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), "agent-reclaim-"));
  await run("git", ["init", "-q", "-b", "main"], { cwd: dir });
  await run("git", ["config", "user.email", "test@local"], { cwd: dir });
  await run("git", ["config", "user.name", "test"], { cwd: dir });
  await writeFile(join(dir, "kept.txt"), "one\n");
  await run("git", ["add", "."], { cwd: dir });
  await run("git", ["commit", "-qm", "init"], { cwd: dir });
  return dir;
}

async function gone(repo: string, conversationId: number): Promise<boolean> {
  const list = await run("git", ["-C", repo, "worktree", "list", "--porcelain"]);
  return !list.stdout.includes(worktreePaths(repo, conversationId).dir);
}

/**
 * The sweep exists because nothing deletes a checkout on purpose: storage gets reset or
 * migrated and `.worktrees` outlives the conversations that owned it. What it must never
 * do is take work with it, so the only thing it removes is a branch the base already has.
 */
test("reclaimWorktrees drops a checkout whose conversation is gone", async () => {
  const repo = await scratchRepo();
  const target = await ensureWorktree(repo, 5);
  assert.equal(target !== undefined, true);

  const result = await reclaimWorktrees(repo, new Set<number>());
  assert.deepEqual(
    result.removed.map((item) => item.conversationId),
    [5],
  );
  assert.equal(existsSync(worktreePaths(repo, 5).dir), false);
  // The registration goes too, or `git worktree list` keeps pointing at a dead path.
  assert.equal(await gone(repo, 5), true);
  const branches = await run("git", ["-C", repo, "branch", "--list", worktreePaths(repo, 5).branch]);
  assert.equal(branches.stdout.trim(), "");
});

test("reclaimWorktrees keeps an orphan whose commits are not merged", async () => {
  const repo = await scratchRepo();
  const target = await ensureWorktree(repo, 6);
  assert.equal(target !== undefined, true);
  // The only copy of this commit is the worktree about to be considered orphaned.
  await writeFile(join(target!.dir, "work.txt"), "unsaved idea\n");
  await run("git", ["add", "."], { cwd: target!.dir });
  await run("git", ["commit", "-qm", "wip"], { cwd: target!.dir });

  const result = await reclaimWorktrees(repo, new Set<number>());
  assert.deepEqual(result.removed, []);
  assert.equal(result.kept[0]?.conversationId, 6);
  assert.equal(result.kept[0]?.reason, "unmerged");
  assert.equal(existsSync(target!.dir), true);
});

test("reclaimWorktrees keeps an orphan with uncommitted files", async () => {
  const repo = await scratchRepo();
  const target = await ensureWorktree(repo, 7);
  assert.equal(target !== undefined, true);
  await writeFile(join(target!.dir, "notes.txt"), "still typing\n");

  const result = await reclaimWorktrees(repo, new Set<number>());
  assert.deepEqual(result.removed, []);
  assert.equal(result.kept[0]?.reason, "dirty");
  assert.equal(existsSync(target!.dir), true);
});

test("reclaimWorktrees leaves live conversations alone", async () => {
  const repo = await scratchRepo();
  const target = await ensureWorktree(repo, 8);
  assert.equal(target !== undefined, true);

  const result = await reclaimWorktrees(repo, new Set([8]));
  assert.deepEqual(result.removed, []);
  assert.deepEqual(result.kept, []);
  assert.equal(existsSync(target!.dir), true);
});

test("reclaimWorktrees does nothing when the base branch has no name", async () => {
  const repo = await scratchRepo();
  const target = await ensureWorktree(repo, 9);
  assert.equal(target !== undefined, true);
  // Detached HEAD: "HEAD..agent/conv-9" is not a range, so nothing can be called merged.
  await run("git", ["checkout", "-q", "--detach"], { cwd: repo });

  const result = await reclaimWorktrees(repo, new Set<number>());
  assert.equal(result.skipped, "no-base");
  assert.deepEqual(result.removed, []);
  assert.equal(existsSync(target!.dir), true);
});
