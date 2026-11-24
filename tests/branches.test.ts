import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { test } from "node:test";
import { createBranch, deleteBranch, listBranches, worktreeOwners } from "../src/branches.ts";

const run = promisify(execFile);

async function git(dir: string, args: string[]): Promise<string> {
  const { stdout } = await run("git", ["-C", dir, ...args]);
  return stdout;
}

/** A repo on main with one commit, so branches have something to point at. */
async function scratchRepo(): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), "agent-branches-"));
  await run("git", ["init", "-q", "-b", "main"], { cwd: dir });
  await run("git", ["config", "user.email", "test@local"], { cwd: dir });
  await run("git", ["config", "user.name", "test"], { cwd: dir });
  await writeFile(join(dir, "a.txt"), "a\n");
  await run("git", ["add", "."], { cwd: dir });
  await run("git", ["commit", "-qm", "init"], { cwd: dir });
  return dir;
}

test("listBranches marks the base, the current branch, and how far each has moved", async () => {
  const dir = await scratchRepo();
  await git(dir, ["branch", "agent/conv-4"]);
  await git(dir, ["checkout", "-q", "agent/conv-4"]);
  await writeFile(join(dir, "b.txt"), "b\n");
  await git(dir, ["add", "."]);
  await git(dir, ["commit", "-qm", "work"]);

  const listing = await listBranches(dir, { base: "main", current: "agent/conv-4" });
  assert.equal(listing.base, "main");
  assert.equal(listing.current, "agent/conv-4");
  // Base first: the branch everything is measured against is the one a reader
  // should not have to hunt for.
  assert.equal(listing.branches[0]?.name, "main");
  assert.equal(listing.branches[0]?.base, true);

  const mine = listing.branches.find((row) => row.name === "agent/conv-4");
  assert.equal(mine?.current, true);
  assert.equal(mine?.ahead, 1);
  assert.equal(mine?.behind, 0);
});

test("a branch a conversation has checked out says which conversation owns it", async () => {
  const dir = await scratchRepo();
  const worktree = join(dir, ".worktrees", "conv-7");
  await run("git", ["-C", dir, "worktree", "add", "-q", "-b", "agent/conv-7", worktree]);

  const owners = await worktreeOwners(dir);
  assert.equal(owners.get("agent/conv-7")?.conversationId, 7);
  // The project's own checkout is not a conversation and must not look like one.
  assert.equal(owners.has("main"), false);

  const listing = await listBranches(dir, { base: "main", current: "main" });
  assert.equal(listing.branches.find((row) => row.name === "agent/conv-7")?.owner?.conversationId, 7);
});

test("createBranch makes a branch without moving the checkout, and refuses a taken name", async () => {
  const dir = await scratchRepo();
  const made = await createBranch(dir, "feature/x", { base: "main" });
  assert.deepEqual(made, { status: "created", name: "feature/x", from: "main" });
  assert.equal((await git(dir, ["rev-parse", "--abbrev-ref", "HEAD"])).trim(), "main");

  assert.deepEqual(await createBranch(dir, "feature/x", { base: "main" }), { status: "exists", name: "feature/x" });
  const bad = await createBranch(dir, "not a branch", { base: "main" });
  assert.equal(bad.status, "invalid");
});

test("deleteBranch refuses the base, a checkout, and a branch with its own commits", async () => {
  const dir = await scratchRepo();
  await git(dir, ["branch", "ahead-of-main"]);
  await git(dir, ["checkout", "-q", "ahead-of-main"]);
  await writeFile(join(dir, "c.txt"), "c\n");
  await git(dir, ["add", "."]);
  await git(dir, ["commit", "-qm", "own work"]);
  await git(dir, ["checkout", "-q", "main"]);

  assert.deepEqual(await deleteBranch(dir, "main", { base: "main", current: "main" }), {
    status: "protected",
    name: "main",
  });
  const unmerged = await deleteBranch(dir, "ahead-of-main", { base: "main" });
  assert.equal(unmerged.status, "unmerged");
  assert.equal(unmerged.status === "unmerged" ? unmerged.commits : 0, 1);

  await git(dir, ["branch", "gone"]);
  assert.deepEqual(await deleteBranch(dir, "gone", { base: "main" }), { status: "deleted", name: "gone" });
  assert.deepEqual(await deleteBranch(dir, "gone", { base: "main" }), { status: "absent", name: "gone" });
});

test("a branch a conversation is working in cannot be deleted from under it", async () => {
  const dir = await scratchRepo();
  const worktree = join(dir, ".worktrees", "conv-3");
  await run("git", ["-C", dir, "worktree", "add", "-q", "-b", "agent/conv-3", worktree]);

  const result = await deleteBranch(dir, "agent/conv-3", { base: "main" });
  assert.deepEqual(result, { status: "in-use", name: "agent/conv-3", conversationId: 3 });
});
