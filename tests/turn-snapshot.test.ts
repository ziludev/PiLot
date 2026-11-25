import assert from "node:assert/strict";
import { test } from "node:test";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { latestSnapshot, pruneSnapshots, snapshotTree } from "../src/turn-snapshot.ts";

/**
 * The turn snapshot is only worth anything if it is a real copy: a hardlink would be rewritten
 * through by `echo x > file` and the very turn that broke the file would break its backup too.
 */

const dir = (prefix: string): string => mkdtempSync(join(tmpdir(), prefix));

function fixture(): { source: string; root: string } {
  const source = dir("snap-src-");
  const root = dir("snap-root-");
  writeFileSync(join(source, "notes.md"), "before");
  mkdirSync(join(source, "src/deep"), { recursive: true });
  writeFileSync(join(source, "src/deep/a.ts"), "export const a = 1;\n");
  return { source, root };
}

test("a turn snapshot copies the workspace", async () => {
  const { source, root } = fixture();
  const snapshot = await snapshotTree(source, new Date("2026-11-21T10:20:30Z"), root);
  assert.ok(snapshot !== undefined);
  assert.equal(snapshot.files, 2);
  assert.equal(readFileSync(join(snapshot.dir, "src/deep/a.ts"), "utf8"), "export const a = 1;\n");
});

test("the snapshot does not change when the original is rewritten in place", async () => {
  const { source, root } = fixture();
  const snapshot = await snapshotTree(source, new Date("2026-11-21T10:20:30Z"), root);
  assert.ok(snapshot !== undefined);
  writeFileSync(join(source, "notes.md"), "after, and longer than before");
  assert.equal(readFileSync(join(snapshot.dir, "notes.md"), "utf8"), "before");
});

test("caches, repositories and the agent's own data stay out", async () => {
  const { source, root } = fixture();
  for (const name of ["node_modules", ".git", ".data", "backups"]) {
    mkdirSync(join(source, name), { recursive: true });
    writeFileSync(join(source, name, "x.txt"), "no");
  }
  const snapshot = await snapshotTree(source, new Date("2026-11-21T10:20:30Z"), root);
  assert.ok(snapshot !== undefined);
  assert.equal(snapshot.files, 2);
  assert.equal(existsSync(join(snapshot.dir, "node_modules")), false);
  assert.equal(existsSync(join(snapshot.dir, ".git")), false);
});

test("secrets are never duplicated into the snapshot", async () => {
  const { source, root } = fixture();
  writeFileSync(join(source, ".env"), "TOKEN=1");
  writeFileSync(join(source, ".npmrc"), "//registry:_authToken=1");
  const snapshot = await snapshotTree(source, new Date("2026-11-21T10:20:30Z"), root);
  assert.ok(snapshot !== undefined);
  assert.equal(snapshot.files, 2);
  assert.equal(existsSync(join(snapshot.dir, ".env")), false);
  assert.equal(existsSync(join(snapshot.dir, ".npmrc")), false);
});

test("oversized files are counted, not copied", async () => {
  const { source, root } = fixture();
  writeFileSync(join(source, "big.bin"), Buffer.alloc(1024 * 1024 + 1));
  const snapshot = await snapshotTree(source, new Date("2026-11-21T10:20:30Z"), root);
  assert.ok(snapshot !== undefined);
  assert.equal(snapshot.files, 2);
  assert.equal(snapshot.skipped, 1);
  assert.equal(existsSync(join(snapshot.dir, "big.bin")), false);
});

test("symlinks are left out rather than followed", async () => {
  const { source, root } = fixture();
  symlinkSync(join(source, "notes.md"), join(source, "link.md"));
  const snapshot = await snapshotTree(source, new Date("2026-11-21T10:20:30Z"), root);
  assert.ok(snapshot !== undefined);
  assert.equal(snapshot.files, 2);
  assert.equal(existsSync(join(snapshot.dir, "link.md")), false);
});

test("the newest snapshot is the one reported", async () => {
  const { source, root } = fixture();
  await snapshotTree(source, new Date("2026-11-20T09:00:00Z"), root);
  const newest = await snapshotTree(source, new Date("2026-11-21T11:00:00Z"), root);
  assert.ok(newest !== undefined);
  assert.equal(await latestSnapshot(root), newest.dir);
});

test("snapshots older than a week are dropped", async () => {
  const { source, root } = fixture();
  await snapshotTree(source, new Date("2026-11-01T09:00:00Z"), root);
  const recent = await snapshotTree(source, new Date("2026-11-20T09:00:00Z"), root);
  assert.ok(recent !== undefined);
  await pruneSnapshots(new Date("2026-11-21T09:00:00Z"), 7, root);
  assert.equal(existsSync(join(root, "2026-11-01")), false);
  assert.equal(existsSync(join(root, "2026-11-20")), true);
});

test("a day window does not matter if one day alone fills the disk", async () => {
  const { source, root } = fixture();
  for (const time of ["09:00:00", "10:00:00", "11:00:00", "12:00:00"]) {
    await snapshotTree(source, new Date(`2026-11-20T${time}`), root);
  }
  await pruneSnapshots(new Date("2026-11-21T09:00:00Z"), 7, root, 2);
  assert.equal(existsSync(join(root, "2026-11-20/110000")), true);
  assert.equal(existsSync(join(root, "2026-11-20/120000")), true);
  assert.equal(existsSync(join(root, "2026-11-20/100000")), false);
  assert.equal(existsSync(join(root, "2026-11-20/090000")), false);
});
