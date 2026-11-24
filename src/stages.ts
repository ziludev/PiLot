import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { classify, glab } from "./glab.ts";

import { log } from "./log.ts";

// The pipeline a chat's work walks through, derived from what git and GitLab
// already know: commits, the base branch, the MR, its review rounds. The bridge
// names no stage of its own — it reports facts, and the client draws them.
//
// Rounds matter because review bots are capped: the team limits a merge request to
// three rounds of machine review, so "how many rounds has this had" is the one
// number that decides whether merging is still allowed.

const run = promisify(execFile);

/** A stage is a fact plus the one thing that moves it forward. */
export type Stage = {
  key: string;
  label: string;
  state: "todo" | "active" | "done" | "blocked";
  /** What to do next, when something is. */
  nextAction?: string;
  /** What stops the next action, when something does. */
  blockedBy?: string;
  /** A short fact to show on the row: a sha, an MR id, a count. */
  detail?: string;
};

async function git(cwd: string, args: string[]): Promise<string> {
  const { stdout } = await run("git", args, { cwd, maxBuffer: 4 * 1024 * 1024 });
  return stdout;
}

/** Whether every commit of `branch` is already in `into`. */
async function mergedInto(cwd: string, branch: string, into: string): Promise<boolean> {
  try {
    await run("git", ["merge-base", "--is-ancestor", branch, into], { cwd });
    return true;
  } catch {
    return false;
  }
}

/** How far `branch` is ahead of and behind `base`, from the base's point of view. */
async function distance(cwd: string, base: string, branch: string): Promise<{ ahead: number; behind: number }> {
  try {
    const out = await git(cwd, ["rev-list", "--left-right", "--count", `${base}...${branch}`]);
    const [behind, ahead] = out.trim().split(/\s+/).map((n) => Number(n));
    return { ahead: ahead ?? 0, behind: behind ?? 0 };
  } catch {
    return { ahead: 0, behind: 0 };
  }
}

type RawNote = { system?: boolean; created_at?: string; author?: { username?: string } };
type RawDiscussion = { notes?: RawNote[] };

/**
 * Review rounds: the distinct commits a person (not the system) has commented on.
 * Counting heads rather than comments is what makes it a round: a bot that reviews
 * once per push produces one round per push, however many notes it leaves.
 */
async function reviewRounds(projectPath: string, iid: number): Promise<{ rounds: number; reviewers: string[] }> {
  const result = await glab(projectPath, ["api", `projects/:id/merge_requests/${iid}/discussions?per_page=100`]);
  if (classify(result) !== undefined) return { rounds: 0, reviewers: [] };
  let rows: Array<RawDiscussion & { position?: { head_sha?: string } }>;
  try {
    rows = JSON.parse(result.stdout) as Array<RawDiscussion & { position?: { head_sha?: string } }>;
  } catch {
    return { rounds: 0, reviewers: [] };
  }
  const heads = new Set<string>();
  const people = new Set<string>();
  for (const row of rows) {
    for (const note of row.notes ?? []) {
      if (note.system === true) continue;
      const who = note.author?.username;
      if (who !== undefined && who.length > 0) people.add(who);
      // A discussion carries the commit it was written on; without one it still
      // counts once, under the empty sha.
      heads.add(row.position?.head_sha ?? "");
    }
  }
  return { rounds: heads.size, reviewers: [...people] };
}

/** The open merge request from `branch` into `target`, if there is one. */
async function openRequest(projectPath: string, branch: string, target: string): Promise<number | undefined> {
  const query = `projects/:id/merge_requests?state=opened&per_page=20&source_branch=${encodeURIComponent(branch)}`;
  const result = await glab(projectPath, ["api", query]);
  if (classify(result) !== undefined) return undefined;
  let rows: Array<{ iid?: number; target_branch?: string; source_branch?: string }>;
  try {
    rows = JSON.parse(result.stdout) as Array<{ iid?: number; target_branch?: string; source_branch?: string }>;
  } catch {
    return undefined;
  }
  // One source branch can feed several targets: this team asks base for test and
  // feature/server for release, and the strip must not confuse the two.
  const hit = rows.find((row) => row.target_branch === target && row.source_branch === branch);
  return hit?.iid;
}

export type StageReport = {
  branch: string;
  /** The branch work is cut from, where the dev stage is measured. */
  origin: string;
  /** Where 提测 goes: a branch, not the project's default. */
  base: string;
  target: string;
  rounds: number;
  roundLimit: number;
  reviewers: string[];
  stages: Stage[];
};

/**
 * Derives the four stages of this team's pipeline: develop, hand to test, ask for
 * release, ship. Every fact comes from git or GitLab; nothing is remembered, so a
 * strip can never claim a state the repository does not have.
 */
export async function deriveStages(
  projectPath: string,
  worktree: string,
  options: { origin?: string; base: string; branch: string; target: string; roundLimit: number },
): Promise<StageReport> {
  const base = options.base.trim().length > 0 ? options.base.trim() : "release/test";
  // 开发 is measured from where the branch was cut, not from where it is going:
  // a chat on release/test's child has no commits over master, and would look
  // stalled if the two were confused.
  const origin = (options.origin ?? "").trim().length > 0 ? (options.origin ?? "").trim() : base;
  const branch = options.branch.trim();
  const target = options.target.trim().length > 0 ? options.target.trim() : "feature/server";
  const limit = options.roundLimit > 0 ? options.roundLimit : 3;
  const stages: Stage[] = [];

  const { ahead } = await distance(projectPath, origin, branch);
  const { behind } = await distance(projectPath, base, branch);
  // In the test branch means merged, not "has nothing to merge": a branch that
  // has not started yet is not a branch that already passed 提测.
  const inBase = await mergedInto(projectPath, branch, base);
  const devDone = ahead > 0 || inBase;

  stages.push({
    key: "dev",
    label: "开发",
    state: devDone ? "done" : "active",
    detail: ahead > 0 ? `${ahead} 个提交` : branch,
    nextAction: devDone ? undefined : "在这个分支上改代码并提交",
  });

  const baseRequest = await openRequest(projectPath, branch, base).catch(() => undefined);
  stages.push({
    key: "test",
    label: `提测到 ${base}`,
    state: inBase ? "done" : devDone ? "active" : "todo",
    detail: inBase ? undefined : baseRequest !== undefined ? `!${baseRequest}` : devDone ? `${behind} 个提交落后` : undefined,
    nextAction: inBase
      ? undefined
      : !devDone
        ? "开发完成后合到 " + base
        : baseRequest !== undefined
          ? "等 MR 合入"
          : `合到 ${base} 并推送`,
  });

  // Release is where the cap lives: a merge request that has used its rounds cannot
  // merge again, and the strip says so before the button is pressed.
  const releaseMr = await openRequest(projectPath, branch, target).catch(() => undefined);
  let rounds = 0;
  let reviewers: string[] = [];
  if (inBase) {
    const merged = await glab(projectPath, [
      "api",
      `projects/:id/merge_requests?state=merged&source_branch=${encodeURIComponent(branch)}&per_page=1`,
    ]);
    const anyMerged = classify(merged) === undefined && merged.stdout.includes('"iid"');
    stages.push({
      key: "release",
      label: `发布到 ${target}`,
      state: anyMerged ? "done" : "todo",
      nextAction: anyMerged ? undefined : `从 ${base} 建 MR 到 ${target}`,
    });
    stages.push({
      key: "ship",
      label: "上线",
      state: anyMerged ? "active" : "todo",
      nextAction: anyMerged ? "等发布" : undefined,
    });
  } else {
    if (releaseMr !== undefined) {
      const counted = await reviewRounds(projectPath, releaseMr).catch(() => ({ rounds: 0, reviewers: [] as string[] }));
      rounds = counted.rounds;
      reviewers = counted.reviewers;
    }
    const blocked = releaseMr !== undefined && rounds >= limit;
    stages.push({
      key: "release",
      label: `发布到 ${target}`,
      state: blocked ? "blocked" : releaseMr !== undefined ? "active" : "todo",
      detail: releaseMr !== undefined ? `!${releaseMr} · 第 ${Math.min(rounds + 1, limit)}/${limit} 轮` : undefined,
      blockedBy: blocked ? `机器评审已评 ${rounds} 轮（上限 ${limit}）` : undefined,
      nextAction:
        releaseMr === undefined
          ? `等提测通过后从 ${base} 建 MR 到 ${target}`
          : blocked
            ? "人工 review 或改需求"
            : "看评审意见并推修复",
    });
    stages.push({
      key: "ship",
      label: "上线",
      state: "todo",
      nextAction: `等 ${target} 合入后发布`,
    });
  }

  log("stages", "derived", { branch, origin, base, target, rounds, limit });
  return { branch, origin, base, target, rounds, roundLimit: limit, reviewers, stages };
}
