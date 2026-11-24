import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmodSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { deriveStages } from "../src/stages.ts";

// The strip is only ever as honest as its inputs, so the inputs here are a real
// repository plus a stub glab: git answers commits, the stub answers reviews.
function repo(): string {
  const dir = mkdtempSync(join(tmpdir(), "pilot-stages-"));
  const git = (...args: string[]) => execFileSync("git", args, { cwd: dir });
  git("init", "-q", "-b", "release/test");
  git("config", "user.email", "t@example.com");
  git("config", "user.name", "t");
  writeFileSync(join(dir, "a.txt"), "one\n");
  git("add", ".");
  git("commit", "-qm", "first");
  git("checkout", "-q", "-b", "feat/work");
  writeFileSync(join(dir, "b.txt"), "two\n");
  git("add", ".");
  git("commit", "-qm", "second");
  return dir;
}

/** A stub glab: lists one open request and answers discussions with N rounds. */
function stub(rounds: number): string {
  const dir = mkdtempSync(join(tmpdir(), "pilot-glabs-"));
  const path = join(dir, "glab");
  const discussions = Array.from({ length: rounds }, (_, i) => ({
    notes: [{ system: false, author: { username: "ocr" } }],
    position: { head_sha: `sha${i}` },
  }));
  writeFileSync(
    path,
    `#!/bin/sh
case "$2" in
  *merge_requests\\?state=opened*) echo '[{"iid":3165,"source_branch":"feat/work","target_branch":"feature/server"}]' ;;
  *discussions*) echo '${JSON.stringify(discussions)}' ;;
  *state=merged*) echo '[]' ;;
  *) echo '[]' ;;
esac
`,
  );
  chmodSync(path, 0o755);
  return path;
}

test("a merge request that used its rounds is blocked, not merely active", async () => {
  const dir = repo();
  process.env["PILOT_GLAB"] = stub(3);
  const report = await deriveStages(dir, dir, {
    base: "release/test",
    branch: "feat/work",
    target: "feature/server",
    roundLimit: 3,
  });
  const release = report.stages.find((s) => s.key === "release");
  assert.equal(report.rounds, 3);
  assert.equal(release?.state, "blocked");
  assert.match(release?.blockedBy ?? "", /3 轮/);
  assert.equal(report.stages.find((s) => s.key === "dev")?.state, "done");
  assert.equal(report.stages.find((s) => s.key === "test")?.state, "active");
});

test("the same request with rounds left offers the next round", async () => {
  const dir = repo();
  process.env["PILOT_GLAB"] = stub(1);
  const report = await deriveStages(dir, dir, {
    base: "release/test",
    branch: "feat/work",
    target: "feature/server",
    roundLimit: 3,
  });
  const release = report.stages.find((s) => s.key === "release");
  assert.equal(release?.state, "active");
  assert.match(release?.detail ?? "", /!3165/);
  assert.match(release?.detail ?? "", /2\/3/);
  assert.equal(release?.blockedBy, undefined);
});

test("a branch that is already in the base skips straight to release", async () => {
  const dir = repo();
  execFileSync("git", ["checkout", "-q", "release/test"], { cwd: dir });
  execFileSync("git", ["merge", "-q", "--ff-only", "feat/work"], { cwd: dir });
  execFileSync("git", ["checkout", "-q", "feat/work"], { cwd: dir });
  process.env["PILOT_GLAB"] = stub(0);
  const report = await deriveStages(dir, dir, {
    base: "release/test",
    branch: "feat/work",
    target: "feature/server",
    roundLimit: 3,
  });
  assert.equal(report.stages.find((s) => s.key === "test")?.state, "done");
  assert.equal(report.stages.find((s) => s.key === "release")?.state, "todo");
  assert.equal(report.stages.find((s) => s.key === "ship")?.state, "todo");
});

test("without a usable glab the strip still reports what git knows", async () => {
  const dir = repo();
  process.env["PILOT_GLAB"] = "/nonexistent/glab";
  const report = await deriveStages(dir, dir, {
    base: "release/test",
    branch: "feat/work",
    target: "feature/server",
    roundLimit: 3,
  });
  assert.equal(report.stages.length, 4);
  assert.equal(report.rounds, 0);
});

test("the merge refusal only fires at the cap, and says what to do instead", async () => {
  const { reviewCapRefusal } = await import("../src/workflow.ts");
  assert.equal(reviewCapRefusal({ rounds: 2, roundLimit: 3 }), undefined);
  const said = reviewCapRefusal({ rounds: 3, roundLimit: 3 });
  assert.match(said ?? "", /3 轮/);
  assert.match(said ?? "", /人工 review/);
});
