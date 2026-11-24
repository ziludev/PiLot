import { existsSync } from "node:fs";
import { readdir, realpath } from "node:fs/promises";
import { basename, join, sep } from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { log } from "./log.ts";

const run = promisify(execFile);

/** One checkout sitting in .worktrees/ — a session's work, whether or not that session still exists. */
export type StrayRow = {
  dir: string;
  /** The conversation whose number is in the directory name, when the name has one. */
  conversationId?: number;
  branch: string;
  /** Commits on the branch that the base does not have, and the other way round. */
  ahead: number;
  behind: number;
  /** Anything uncommitted: adopting it is safe only if the person meant this state. */
  dirty: boolean;
  /** True when the work is already on the base: the checkout is pure leftovers. */
  merged: boolean;
  /** Whether a conversation with that number is still in the store. */
  occupied: boolean;
};

async function git(dir: string, args: string[]): Promise<string> {
  const { stdout } = await run("git", ["-C", dir, ...args], { timeout: 30_000, maxBuffer: 8 * 1024 * 1024 });
  return stdout;
}

/** Every checkout under the project's .worktrees, one row each, newest number last. */
export async function listStrays(
  projectPath: string,
  base: string,
  known: Set<number> = new Set(),
): Promise<StrayRow[]> {
  const root = join(projectPath, ".worktrees");
  if (!existsSync(root)) return [];
  const names = await readdir(root).catch(() => [] as string[]);
  const rows: StrayRow[] = [];
  for (const name of names.sort()) {
    const match = /^conv-(\d+)$/.exec(name);
    const dir = join(root, name);
    if (!existsSync(join(dir, ".git"))) continue;
    const branch = (await git(dir, ["rev-parse", "--abbrev-ref", "HEAD"]).catch(() => "")).trim();
    if (branch.length === 0 || branch === "HEAD") continue;
    const counts = (await git(dir, ["rev-list", "--left-right", "--count", `${base}...${branch}`]).catch(() => ""))
      .trim()
      .split(/\s+/);
    const behind = Number(counts[0] ?? 0) || 0;
    const ahead = Number(counts[1] ?? 0) || 0;
    const status = (await git(dir, ["status", "--porcelain"]).catch(() => "")).trim();
    // A branch missing from the base (fresh repo, or base renamed) counts as ahead:
    // the work is not on the base either way, and pretending otherwise would let a
    // later cleanup delete it.
    const merged = ahead === 0 && status.length === 0;
    rows.push({
      dir,
      conversationId: match !== null ? Number(match[1]) : undefined,
      branch,
      ahead,
      behind,
      dirty: status.length > 0,
      merged,
      occupied: match !== null && known.has(Number(match[1])),
    });
  }
  return rows;
}

export type AdoptTarget = { dir: string; branch: string };
export type AdoptCheck = AdoptTarget | { error: string };

/**
 * Checks a directory can be adopted before anything is created: inside this
 * project's .worktrees, a real checkout, and on a branch rather than a detached
 * head. The path comes from a request body, so it is checked by resolved path and
 * not by string prefix.
 */
export async function adoptTarget(projectPath: string, dir: string): Promise<AdoptCheck> {
  if (typeof dir !== "string" || dir.trim().length === 0) return { error: "dir required" };
  const root = await realpath(join(projectPath, ".worktrees")).catch(() => "");
  if (root.length === 0) return { error: "this project has no .worktrees" };
  const real = await realpath(dir).catch(() => "");
  if (real.length === 0) return { error: "that directory is gone" };
  if (!real.startsWith(root + sep) || real === root) return { error: "not a worktree of this project" };
  if (basename(real) === "") return { error: "not a worktree of this project" };
  if (!existsSync(join(real, ".git"))) return { error: "that directory is not a checkout" };
  const branch = (await git(real, ["rev-parse", "--abbrev-ref", "HEAD"]).catch(() => "")).trim();
  if (branch.length === 0 || branch === "HEAD") return { error: "that checkout is on a detached head" };
  log("worktree", "adopt target checked", { dir: real, branch });
  return { dir: real, branch };
}
