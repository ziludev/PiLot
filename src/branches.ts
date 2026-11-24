import { execFile } from "node:child_process";
import { basename } from "node:path";
import { promisify } from "node:util";
import { log, logError } from "./log.ts";

const run = promisify(execFile);

async function git(dir: string, args: string[]): Promise<string> {
  const { stdout } = await run("git", ["-C", dir, ...args], { timeout: 30_000, maxBuffer: 16 * 1024 * 1024 });
  return stdout;
}

/** The checkout a branch is checked out in, when a conversation owns it. */
export type BranchOwner = { conversationId: number; dir: string };

export type BranchRow = {
  name: string;
  current: boolean;
  base: boolean;
  upstream?: string;
  track?: string;
  /** Commits on the branch that the base does not have. */
  ahead: number;
  /** Commits on the base that the branch does not have. */
  behind: number;
  owner?: BranchOwner;
};

export type BranchList = { base: string; current: string; branches: BranchRow[] };

/**
 * Which branch each checkout holds, keyed by branch name.
 *
 * A branch checked out in a worktree cannot be checked out again, and deleting it
 * would take that checkout with it — so the list has to say who owns what, not
 * just which names exist.
 */
export async function worktreeOwners(projectPath: string): Promise<Map<string, BranchOwner>> {
  const owners = new Map<string, BranchOwner>();
  const raw = await git(projectPath, ["worktree", "list", "--porcelain"]).catch(() => "");
  let dir = "";
  for (const line of raw.split("\n")) {
    if (line.startsWith("worktree ")) {
      dir = line.slice("worktree ".length).trim();
      continue;
    }
    if (!line.startsWith("branch ")) continue;
    const ref = line.slice("branch ".length).trim();
    const name = ref.replace(/^refs\/heads\//, "");
    const match = /^conv-(\d+)$/.exec(basename(dir));
    // The project's own checkout has no conversation; it is not an owner a reader
    // can act on, and marking it would make every branch look taken.
    if (match !== null) owners.set(name, { conversationId: Number(match[1]), dir });
  }
  return owners;
}

/**
 * Lists local branches with their distance from the base, so a reader can tell a
 * branch that is done from one that would drop work.
 */
export async function listBranches(
  projectPath: string,
  options: { base?: string; current?: string; limit?: number } = {},
): Promise<BranchList> {
  const base = (options.base ?? "").trim() || (await git(projectPath, ["rev-parse", "--abbrev-ref", "HEAD"]).catch(() => "")).trim();
  const current = (options.current ?? "").trim();
  const owners = await worktreeOwners(projectPath);
  const raw = await git(projectPath, [
    "for-each-ref",
    "--format=%(refname:short)\t%(upstream:short)\t%(upstream:track)",
    "refs/heads",
  ]).catch(() => "");

  const names: string[] = [];
  const meta = new Map<string, { upstream?: string; track?: string }>();
  for (const line of raw.split("\n")) {
    if (line.trim().length === 0) continue;
    const [name, upstream, track] = line.split("\t");
    if (name === undefined || name.length === 0) continue;
    names.push(name);
    meta.set(name, {
      upstream: upstream === undefined || upstream === "" ? undefined : upstream,
      track: track === undefined || track === "" ? undefined : track.replace(/^\[|\]$/g, ""),
    });
  }

  const limit = Math.max(1, options.limit ?? 200);
  const rows: BranchRow[] = [];
  for (const name of names.slice(0, limit)) {
    const counts = await git(projectPath, ["rev-list", "--left-right", "--count", `${base}...${name}`]).catch(() => "");
    const [behindRaw, aheadRaw] = counts.trim().split(/\s+/);
    const entry = meta.get(name) ?? {};
    rows.push({
      name,
      current: name === current,
      base: name === base,
      upstream: entry.upstream,
      track: entry.track,
      ahead: Number(aheadRaw ?? 0) || 0,
      behind: Number(behindRaw ?? 0) || 0,
      owner: owners.get(name),
    });
  }
  // Base first, then the chat's own branch, then the rest by name: the two the
  // reader cares about should not be found by scrolling.
  rows.sort((left, right) => {
    if (left.base !== right.base) return left.base ? -1 : 1;
    if (left.current !== right.current) return left.current ? -1 : 1;
    return left.name.localeCompare(right.name);
  });
  return { base, current, branches: rows };
}

export type BranchResult =
  | { status: "created"; name: string; from: string }
  | { status: "deleted"; name: string }
  | { status: "exists"; name: string }
  | { status: "absent"; name: string }
  | { status: "invalid"; name: string; message: string }
  | { status: "in-use"; name: string; conversationId: number }
  | { status: "protected"; name: string }
  | { status: "unmerged"; name: string; commits: number }
  | { status: "error"; name: string; message: string };

/**
 * Whether git would accept this as a branch name at all. Asking git is the only
 * answer that matches what the next command will do; a regex would drift.
 */
export async function validBranchName(projectPath: string, name: string): Promise<boolean> {
  if (name.trim().length === 0) return false;
  try {
    await run("git", ["-C", projectPath, "check-ref-format", "--branch", name], { timeout: 10_000 });
    return true;
  } catch {
    return false;
  }
}

/** Creates a branch at a point without moving any checkout. */
export async function createBranch(
  projectPath: string,
  name: string,
  options: { from?: string; base?: string } = {},
): Promise<BranchResult> {
  const clean = name.trim();
  if (!(await validBranchName(projectPath, clean))) {
    return { status: "invalid", name: clean, message: "not a branch name" };
  }
  const exists = await git(projectPath, ["rev-parse", "--verify", "--quiet", `refs/heads/${clean}`]).catch(() => "");
  if (exists.trim().length > 0) return { status: "exists", name: clean };

  const from = (options.from ?? "").trim() || (options.base ?? "").trim() || "HEAD";
  try {
    await git(projectPath, ["branch", clean, from]);
    log("branches", "created", { name: clean, from });
    return { status: "created", name: clean, from };
  } catch (error) {
    logError("branches", `could not create ${clean}`, error);
    return { status: "error", name: clean, message: (error as Error).message };
  }
}

/**
 * Deletes a branch that holds nothing of its own. The base branch, the branch the
 * project is on, and any branch a conversation has checked out are refused: those
 * are checkouts, and deleting them would take a working tree with it.
 */
export async function deleteBranch(
  projectPath: string,
  name: string,
  options: { base?: string; current?: string } = {},
): Promise<BranchResult> {
  const clean = name.trim();
  const base = (options.base ?? "").trim();
  const current = (options.current ?? "").trim() || (await git(projectPath, ["rev-parse", "--abbrev-ref", "HEAD"]).catch(() => "")).trim();
  if (clean.length === 0) return { status: "invalid", name: clean, message: "empty" };
  if (clean === base || clean === current) return { status: "protected", name: clean };

  const owners = await worktreeOwners(projectPath);
  const owner = owners.get(clean);
  if (owner !== undefined) return { status: "in-use", name: clean, conversationId: owner.conversationId };

  const exists = await git(projectPath, ["rev-parse", "--verify", "--quiet", `refs/heads/${clean}`]).catch(() => "");
  if (exists.trim().length === 0) return { status: "absent", name: clean };

  if (base.length > 0) {
    const ahead = Number((await git(projectPath, ["rev-list", "--count", `${base}..${clean}`]).catch(() => "0")).trim()) || 0;
    if (ahead > 0) return { status: "unmerged", name: clean, commits: ahead };
  }
  try {
    await git(projectPath, ["branch", "-D", clean]);
    log("branches", "deleted", { name: clean });
    return { status: "deleted", name: clean };
  } catch (error) {
    logError("branches", `could not delete ${clean}`, error);
    return { status: "error", name: clean, message: (error as Error).message };
  }
}
