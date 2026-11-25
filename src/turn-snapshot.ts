/**
 * A copy of the workspace as it was when the turn started.
 *
 * `checkpoint.ts` covers the files the agent touches with `write` / `edit`. That is the minority:
 * most edits happen through bash — `sed -i`, `mv`, `rm`, `> file` — and those leave nothing behind.
 * One snapshot per turn covers all of it, so "put it back the way it was" works no matter which
 * tool did the damage.
 *
 * Files are copied, not hardlinked. A hardlink shares the inode, so a turn that does `echo x > f`
 * would rewrite the snapshot through the link and the copy would be worthless exactly when it is
 * needed. The workspace is small once caches and build output are excluded, so a real copy costs a
 * few hundred milliseconds and removes that whole class of surprise.
 *
 * Excluded on purpose: things nobody wants restored (`.git`, `node_modules`, build output, the
 * agent's own database) and secrets (`.ssh`, `.env*`, `.npmrc`) that should never be duplicated
 * into another directory. Anything over `MAX_BYTES` is skipped and counted.
 */
import { copyFile, mkdir, readdir, rm, stat } from "node:fs/promises";
import { dirname, join } from "node:path";
import { defineExtension, GenerationTask, hook } from "@earendil-works/pi-durable";
import { config } from "./config.ts";
import { log, logError } from "./log.ts";

const SKIP_DIRS = new Set([
  ".git",
  ".data",
  "node_modules",
  "backups",
  "dist",
  "build",
  "target",
  ".next",
  ".turbo",
  ".venv",
  "venv",
  "__pycache__",
  ".cache",
  ".ssh",
  ".aws",
  ".gnupg",
  ".kube",
]);

const SKIP_FILES = /^(\.env(\..*)?|\.npmrc|\.netrc|\.git-credentials|id_(rsa|ed25519|ecdsa))$/i;

const MAX_BYTES = 1024 * 1024;
const MAX_FILES = 20_000;

export interface Snapshot {
  dir: string;
  files: number;
  bytes: number;
  skipped: number;
  ms: number;
}

export function snapshotRoot(): string {
  return join(dirname(config.dbPath), "snapshots");
}

/** `<root>/<YYYY-MM-DD>/<HHMMSS>` — the day directory is what the pruner deletes. */
function snapshotDir(now: Date, root = snapshotRoot()): string {
  // Local time, not UTC: a snapshot taken at 01:00 Beijing time belongs with that day, and a
  // pruner that counts days in UTC would shave eight hours off the retention window.
  const day = now.toLocaleDateString("sv-SE");
  const time = now.toTimeString().slice(0, 8).replaceAll(":", "");
  return join(root, day, time);
}

export async function snapshotTree(
  source = config.workspace,
  now = new Date(),
  root = snapshotRoot(),
): Promise<Snapshot | undefined> {
  const started = Date.now();
  const dir = snapshotDir(now, root);
  const state = { files: 0, bytes: 0, skipped: 0 };
  try {
    await mkdir(dir, { recursive: true });
    await walk(source, dir, state);
  } catch (error) {
    logError("snapshot", "snapshot failed", { error: String(error) });
    return undefined;
  }
  return { dir, ...state, ms: Date.now() - started };
}

async function walk(source: string, target: string, state: { files: number; bytes: number; skipped: number }): Promise<void> {
  if (state.files >= MAX_FILES) return;
  const entries = await readdir(source, { withFileTypes: true });
  for (const entry of entries) {
    if (state.files >= MAX_FILES) return;
    // Symlinks are left out: copying one either duplicates a file outside the workspace or points
    // the snapshot back into it, and neither is what "restore" should mean.
    if (entry.isSymbolicLink()) continue;
    const from = join(source, entry.name);
    const to = join(target, entry.name);
    if (entry.isDirectory()) {
      if (SKIP_DIRS.has(entry.name)) continue;
      await mkdir(to, { recursive: true });
      await walk(from, to, state);
      continue;
    }
    if (!entry.isFile() || SKIP_FILES.test(entry.name)) continue;
    const info = await stat(from);
    if (info.size > MAX_BYTES) {
      state.skipped += 1;
      continue;
    }
    await copyFile(from, to);
    state.files += 1;
    state.bytes += info.size;
  }
}

/** Latest snapshot directory, newest first; `undefined` when none was taken yet. */
export async function latestSnapshot(root = snapshotRoot()): Promise<string | undefined> {
  const days = await safeList(root).catch(() => []);
  for (const day of days.sort().reverse()) {
    const times = await safeList(join(root, day)).catch(() => []);
    const newest = times.sort().reverse()[0];
    if (newest !== undefined) return join(root, day, newest);
  }
  return undefined;
}

async function safeList(path: string): Promise<string[]> {
  return (await readdir(path)).filter((name) => !name.startsWith("."));
}

/**
 * Snapshots are only useful for the minutes after a mistake: a day window is plenty, and a busy day
 * needs a cap of its own, because a full copy per turn times a hundred turns a day fills a laptop
 * long before the window closes. Whole day directories still go together, which keeps a mis-timed
 * restore from reaching back into months of history.
 */
export const KEEP_DAYS = 2;
export const KEEP_PER_DAY = 20;

export async function pruneSnapshots(
  now = new Date(),
  days = KEEP_DAYS,
  root = snapshotRoot(),
  perDay = KEEP_PER_DAY,
): Promise<void> {
  const cutoff = now.getTime() - days * 24 * 60 * 60 * 1000;
  for (const name of await safeList(root).catch(() => [])) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(name)) continue;
    const at = new Date(`${name}T00:00:00`).getTime();
    if (Number.isNaN(at) || at >= cutoff) {
      // Kept days still get trimmed: newest first is what a restore wants anyway.
      const times = (await safeList(join(root, name)).catch(() => [])).sort().reverse();
      for (const stale of times.slice(Math.max(perDay, 1))) {
        await rm(join(root, name, stale), { recursive: true, force: true });
      }
      continue;
    }
    await rm(join(root, name), { recursive: true, force: true });
  }
}

/**
 * Once per turn, not once per request: the snapshot is taken before the first provider request and
 * the flag is re-armed by `afterRun`. Fire-and-forget, because a turn should never wait on disk
 * before it can think.
 */
const turn = { taken: false };

export const snapshotExtension = defineExtension({
  name: "turn-snapshot",
  hooks: [
    hook(GenerationTask, {
      beforeRequest: () => {
        if (turn.taken) return undefined;
        turn.taken = true;
        void snapshotTree()
          .then(async (snapshot) => {
            if (snapshot === undefined) return;
            log("snapshot", "turn snapshot", {
              dir: snapshot.dir,
              files: snapshot.files,
              mb: Math.round(snapshot.bytes / 1024 / 1024),
              skipped: snapshot.skipped,
              ms: snapshot.ms,
            });
            await pruneSnapshots();
          })
          .catch((error: unknown) => logError("snapshot", "snapshot task failed", { error: String(error) }));
        return undefined;
      },
      afterRun: () => {
        turn.taken = false;
        return undefined;
      },
    }),
  ],
});
