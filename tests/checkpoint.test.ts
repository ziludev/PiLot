import assert from "node:assert/strict";
import { test } from "node:test";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { KEEP_DAYS, backupFile, prune } from "../src/checkpoint.ts";

/**
 * An agent that edits a file it was not asked to touch needs one thing: the bytes that were there
 * before. These tests use a temporary root, since the real one lives next to the agent database.
 */

const workspace = (): string => mkdtempSync(join(tmpdir(), "ckpt-ws-"));
const root = (): string => mkdtempSync(join(tmpdir(), "ckpt-root-"));

test("a copy is taken before the write, with the original content", () => {
  const ws = workspace();
  const file = join(ws, "notes.md");
  writeFileSync(file, "original");
  const saved = backupFile(file, root());
  assert.ok(saved !== undefined);
  assert.equal(readFileSync(saved, "utf8"), "original");
});

test("two backups of one file land apart", () => {
  const ws = workspace();
  const file = join(ws, "a.ts");
  writeFileSync(file, "one");
  const first = backupFile(file, root());
  writeFileSync(file, "two");
  const second = backupFile(file, root());
  assert.ok(first !== undefined && second !== undefined);
  assert.notEqual(first, second);
  assert.equal(readFileSync(first, "utf8"), "one");
  assert.equal(readFileSync(second, "utf8"), "two");
});

test("nothing is copied for a file that does not exist yet", () => {
  assert.equal(backupFile(join(workspace(), "new.ts"), root()), undefined);
});

test("an empty file is not worth a copy", () => {
  const ws = workspace();
  const file = join(ws, "empty");
  writeFileSync(file, "");
  assert.equal(backupFile(file, root()), undefined);
});

test("week-old copies are pruned, today's are kept", () => {
  const dir = root();
  const old = new Date(Date.now() - (KEEP_DAYS + 1) * 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
  const today = new Date().toISOString().slice(0, 10);
  mkdirSync(join(dir, old), { recursive: true });
  mkdirSync(join(dir, today), { recursive: true });
  mkdirSync(join(dir, "not-a-day"), { recursive: true });
  prune(dir);
  const left = readdirSync(dir).sort();
  assert.equal(left.includes(old), false);
  assert.equal(left.includes(today), true);
  assert.equal(left.includes("not-a-day"), true, "only its own day directories are touched");
  assert.equal(existsSync(join(dir, today)), true);
});
