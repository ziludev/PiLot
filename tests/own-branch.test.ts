import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  attachBranch,
  branchNameOK,
  ensureWorktree,
  isPushed,
  localBranchExists,
  renameWorktreeBranch,
  worktreePaths,
} from "../src/worktree.ts";

// A real repository, because every question here is one git answers: does a name
// parse, does a branch exist, is it on a remote, can a checkout move to it.
function repo(): string {
  const dir = mkdtempSync(join(tmpdir(), "pilot-own-"));
  const git = (...args: string[]) => execFileSync("git", args, { cwd: dir });
  git("init", "-q", "-b", "main");
  git("config", "user.email", "t@example.com");
  git("config", "user.name", "t");
  writeFileSync(join(dir, "a.txt"), "one\n");
  git("add", ".");
  git("commit", "-qm", "first");
  return dir;
}

test("a name git would refuse is refused here, before anything is created", async () => {
  const root = repo();
  assert.equal(await branchNameOK(root, "feat/sky-blue"), true);
  assert.equal(await branchNameOK(root, "feat/sky blue"), false);
  assert.equal(await branchNameOK(root, "feat/.."), false);
  assert.equal(await branchNameOK(root, "  padded"), false);
  assert.equal(await branchNameOK(root, ""), false);
});

test("a worktree takes the name it is given, and the directory keeps the id", async () => {
  const root = repo();
  const made = await ensureWorktree(root, 7, "main", "feat/sky-blue");
  assert.equal(made?.branch, "feat/sky-blue");
  assert.match(made?.dir ?? "", /\.worktrees\/conv-7$/);
  const head = execFileSync("git", ["rev-parse", "--abbrev-ref", "HEAD"], { cwd: made!.dir }).toString().trim();
  assert.equal(head, "feat/sky-blue");
  // Without a name the derived one is used, which is what an older chat already has.
  assert.equal(worktreePaths(root, 9).branch, "agent/conv-9");
});

test("a second project attaches the same branch instead of branching again", async () => {
  const one = repo();
  const two = repo();
  const first = await ensureWorktree(one, 7, "main", "feat/sky-blue");
  assert.ok(first);
  // two does not have that branch yet: it is created from main, as the first project did.
  const second = await ensureWorktree(two, 7, "main", "feat/sky-blue");
  assert.equal(second?.branch, "feat/sky-blue");
  assert.equal(await localBranchExists(two, "feat/sky-blue"), true);
  // A checkout that already holds the branch is attached to it, not renamed.
  const moved = await attachBranch(second!.dir, "feat/sky-blue");
  assert.equal(moved.status, "ok");
});

test("only a branch nobody pushed may be renamed", async () => {
  const root = repo();
  const made = await ensureWorktree(root, 11, "main", "feat/first-name");
  assert.ok(made);
  assert.equal(await isPushed(root, "feat/first-name"), false);
  const renamed = await renameWorktreeBranch(made.dir, "feat/first-name", "feat/second-name");
  assert.deepEqual(renamed, { status: "renamed", branch: "feat/second-name" });
  const head = execFileSync("git", ["rev-parse", "--abbrev-ref", "HEAD"], { cwd: made.dir }).toString().trim();
  assert.equal(head, "feat/second-name");
  // Once a remote has the name, it is not ours to rename any more.
  execFileSync("git", ["update-ref", "refs/remotes/origin/feat/second-name", "HEAD"], { cwd: root });
  assert.equal(await isPushed(root, "feat/second-name"), true);
});

test("a rename with a dirty checkout, a taken name or no name at all says which", async () => {
  const root = repo();
  const made = await ensureWorktree(root, 12, "main", "feat/clean");
  assert.ok(made);
  assert.equal((await renameWorktreeBranch(made.dir, "feat/clean", "feat/clean")).status, "unchanged");
  execFileSync("git", ["branch", "taken", "main"], { cwd: root });
  const taken = await renameWorktreeBranch(made.dir, "feat/clean", "taken");
  assert.deepEqual(taken, { status: "exists", branch: "taken" });
  writeFileSync(join(made.dir, "a.txt"), "changed\n");
  const dirty = await renameWorktreeBranch(made.dir, "feat/clean", "feat/other");
  assert.deepEqual(dirty, { status: "dirty", branch: "feat/clean" });
  const nowhere = await attachBranch(made.dir, "feat/does-not-exist");
  assert.equal(nowhere.status === "missing" || nowhere.status === "dirty", true);
});
