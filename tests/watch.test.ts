import assert from "node:assert/strict";
import { test } from "node:test";
import { tmpdir } from "node:os";
import { clampReading, plainWatchText, readWatch, summarizeWatch, watchChangeNote } from "../src/watch.ts";

/**
 * A watch is only as good as its reading. The tests that matter are the ones that decide whether a
 * round stays quiet: two runs of an unchanged command must compare equal even when the terminal
 * would have printed them with colour and carriage returns, and a failure must compare *different*
 * from a success — a CI going red is the case the whole feature exists for.
 */

test("an unchanged command reads the same twice, whatever its trailing whitespace", async () => {
  const first = await readWatch("printf 'open: none\\n'", tmpdir());
  const second = await readWatch("printf 'open: none\\n\\n'", tmpdir());
  assert.equal(first.broke, false);
  assert.equal(first.reading, "ok\nopen: none");
  assert.equal(second.reading, first.reading);
});

test("colour codes and carriage returns do not count as a change", async () => {
  const plain = await readWatch("printf 'a\\nb\\n'", tmpdir());
  const coloured = await readWatch("printf '\\033[31ma\\033[0m\\r\\n\\033[32mb\\033[0m\\r\\n'", tmpdir());
  assert.equal(coloured.reading, plain.reading);
});

test("a non-zero exit is a reading, not a success", async () => {
  const look = await readWatch("printf 'boom' >&2; exit 3", tmpdir());
  assert.equal(look.broke, true);
  assert.match(look.reading, /^exit 3\nboom$/);
  assert.match(look.summary, /exit 3/);
});

test("a command that does not exist is reported as such", async () => {
  const look = await readWatch("definitely-not-a-real-command-xyz", tmpdir());
  assert.equal(look.broke, true);
  assert.match(look.reading, /^exit 127/);
});

test("a long reading keeps its head and its tail and says what it dropped", () => {
  const long = `${"a".repeat(5000)}MIDDLE${"z".repeat(5000)}`;
  const clamped = clampReading(long, 400);
  assert.ok(clamped.length <= 420, `expected a clamped length, got ${clamped.length}`);
  assert.match(clamped, /字符略/);
  assert.ok(clamped.startsWith("aaaa"));
  assert.ok(clamped.endsWith("zzzz"));
});

test("the summary counts printed lines, not the status line", async () => {
  const look = await readWatch("printf 'one\\ntwo\\nthree\\n'", tmpdir());
  assert.equal(look.summary, "3 行");
});

test("the change note names a baseline as a baseline", () => {
  const note = watchChangeNote("glab mr view 12", "ok\nno comments", "ok\n1 new comment", true);
  assert.match(note, /glab mr view 12/);
  assert.match(note, /刚建任务时的样子/);
  assert.match(note, /1 new comment/);
});

test("plainWatchText leaves ordinary text alone", () => {
  assert.equal(plainWatchText("  hi\r\nthere  \n"), "hi\nthere");
});
