/**
 * A copy of the file as it was before the agent changed it.
 *
 * `git` covers what is committed and, with `git status`, what is tracked. It does not cover the
 * notes, configs and one-off scripts under the workspace that were never added to a repository —
 * and those are exactly what an agent is careless with. So every `write` / `edit` lands a copy
 * first: cheap (one file, skipped above 2 MB), no dependencies, and `cp` is the whole undo story.
 *
 * Kept for a week and pruned as copies are made, so the directory cannot grow without bound.
 */
import { copyFileSync, existsSync, mkdirSync, readdirSync, rmSync, statSync } from "node:fs";
import { createHash } from "node:crypto";
import { basename, dirname, isAbsolute, join, resolve } from "node:path";
import { config } from "./config.ts";

export const KEEP_DAYS = 7;
const MAX_BYTES = 2 * 1024 * 1024;

export function checkpointRoot(): string {
  return join(dirname(config.dbPath), "checkpoints");
}

/** Copies `path` aside and returns where it went; undefined when there is nothing to copy. */
export function backupFile(path: string, root = checkpointRoot(), now = new Date()): string | undefined {
  const absolute = isAbsolute(path) ? path : resolve(config.workspace, path);
  if (!existsSync(absolute)) return undefined;
  const stat = statSync(absolute);
  if (!stat.isFile() || stat.size === 0 || stat.size > MAX_BYTES) return undefined;

  const day = now.toISOString().slice(0, 10);
  const dir = join(root, day);
  mkdirSync(dir, { recursive: true });
  // Time first so a day's copies sort in the order they were taken; the hash keeps two files with
  // the same basename apart, and the basename keeps the directory readable by hand.
  const stamp = now.toISOString().slice(11, 19).replaceAll(":", "");
  const digest = createHash("sha1").update(absolute).digest("hex").slice(0, 6);
  const target = join(dir, `${stamp}-${digest}-${basename(absolute)}`);
  copyFileSync(absolute, target);
  prune(root, now);
  return target;
}

/** Drops day directories older than `KEEP_DAYS`; only touches its own `YYYY-MM-DD` names. */
export function prune(root: string, now = new Date()): void {
  if (!existsSync(root)) return;
  const cutoff = now.getTime() - KEEP_DAYS * 24 * 60 * 60 * 1000;
  for (const name of readdirSync(root)) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(name)) continue;
    const at = Date.parse(`${name}T00:00:00Z`);
    if (Number.isNaN(at) || at >= cutoff) continue;
    rmSync(join(root, name), { recursive: true, force: true });
  }
}
