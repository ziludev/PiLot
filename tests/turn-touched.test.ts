import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { test } from "node:test";
import { markTree, touchedSince } from "../src/changes.ts";
import { touchedLine } from "../src/feishu.ts";

const run = promisify(execFile);

/** The finished card names these files: a wrong list is a claim about work that did not happen. */
async function scratchRepo(): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), "agent-touched-"));
  await run("git", ["init", "-q", "-b", "main"], { cwd: dir });
  await run("git", ["config", "user.email", "test@local"], { cwd: dir });
  await run("git", ["config", "user.name", "test"], { cwd: dir });
  await writeFile(join(dir, "kept.txt"), "one\n");
  await run("git", ["add", "."], { cwd: dir });
  await run("git", ["commit", "-qm", "init"], { cwd: dir });
  return dir;
}

test("a directory that is not a repo has no mark, so the card just omits the line", async () => {
  const dir = await mkdtemp(join(tmpdir(), "agent-plain-"));
  assert.equal(await markTree(dir), undefined);
});

test("a turn that only talked claims nothing", async () => {
  const dir = await scratchRepo();
  const mark = await markTree(dir);
  assert.notEqual(mark, undefined);
  assert.deepEqual(await touchedSince(dir, mark!), []);
});

test("touchedSince names what this turn wrote, not what was already dirty", async () => {
  const dir = await scratchRepo();
  // Left behind by someone earlier: it is in the tree before the turn starts.
  await writeFile(join(dir, "stale.txt"), "half an edit\n");
  const mark = await markTree(dir);
  assert.notEqual(mark, undefined);

  await writeFile(join(dir, "kept.txt"), "one\ntwo\n");
  await writeFile(join(dir, "fresh.txt"), "hello\n");

  const touched = await touchedSince(dir, mark!);
  assert.deepEqual(touched.sort(), ["fresh.txt", "kept.txt"]);
});

test("a turn that commits its own work is still listed", async () => {
  const dir = await scratchRepo();
  const mark = await markTree(dir);
  await writeFile(join(dir, "kept.txt"), "one\ntwo\n");
  await run("git", ["commit", "-qam", "work"], { cwd: dir });
  assert.deepEqual(await touchedSince(dir, mark!), ["kept.txt"]);
});

test("the line under the cost stays three names wide", async () => {
  assert.equal(touchedLine([]), "");
  assert.equal(touchedLine(["src/a.ts", "src/b.ts"]), "📝 改了 2 个文件：`src/a.ts` · `src/b.ts`");
  assert.match(touchedLine(["a", "b", "c", "d", "e"]), /^📝 改了 5 个文件：`a` · `b` · `c` …还有 2 个$/);
});
