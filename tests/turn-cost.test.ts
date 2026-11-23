import assert from "node:assert/strict";
import { test } from "node:test";
import { forgetSlices, noteSlice, parseSliceRead } from "../src/toolbox/tools/bash-read.ts";
import { batchingHint, forgetReads, noteReadCall, READ_RUN } from "../src/batching.ts";
import { shapeOf, turnRow } from "../src/turn-log.ts";

/**
 * Three ways a turn gets long: reading one file in five slices, asking one question per request,
 * and having no record of either. Each is tested here against the number it exists to move —
 * calls per turn — rather than against its own implementation.
 */

test("a plain slice read is recognised, with the range it asked for", () => {
  assert.deepEqual(parseSliceRead("head -20 src/main.ts"), { path: "src/main.ts", limit: 20 });
  assert.deepEqual(parseSliceRead("head -n 40 src/main.ts"), { path: "src/main.ts", limit: 40 });
  assert.deepEqual(parseSliceRead("head src/main.ts"), { path: "src/main.ts", limit: 10 });
  assert.deepEqual(parseSliceRead("sed -n '40,120p' src/main.ts"), {
    path: "src/main.ts",
    offset: 40,
    limit: 81,
  });
  assert.deepEqual(parseSliceRead("sed -n '900,$p' src/main.ts"), { path: "src/main.ts", offset: 900 });
  assert.deepEqual(parseSliceRead("cat 'my notes.md'"), { path: "my notes.md" });
});

test("anything the shell does beyond reading one file is left to the shell", () => {
  // Each of these produces something that is not the file: a filtered list, a count, a header.
  for (const command of [
    "head -20 f | grep error",
    "cat f > out.txt",
    "grep -rn foo src/",
    "sed -n '1,5p' f | wc -l",
    "ls src/",
    "cat f; cat g",
    "cat $(echo f)",
    "head -20 f && echo done",
    "sed -i s/a/b/ f",
    "tail -n 20 f",
    "head -20",
  ]) {
    assert.equal(parseSliceRead(command), undefined, command);
  }
});

test("the second read of one file is the one that widens", () => {
  forgetSlices();
  // One turn, one file: the first slice is the model guessing at the shape of the file.
  assert.equal(noteSlice(1, "src/main.ts", 0), 1);
  assert.equal(noteSlice(1, "src/main.ts", 10), 2);
  assert.equal(noteSlice(1, "src/main.ts", 20), 3);
  // A different file, or a different conversation, is a different guess.
  assert.equal(noteSlice(1, "src/other.ts", 30), 1);
  assert.equal(noteSlice(2, "src/main.ts", 40), 1);
  // Long after, the same path is a new question rather than more paging.
  assert.equal(noteSlice(1, "src/main.ts", 40 + 11 * 60_000), 1);
});

test("six reads in a row earn one hint, and the streak restarts", () => {
  forgetReads(7);
  for (let i = 1; i < READ_RUN; i += 1) assert.equal(noteReadCall(7, "read", i), i);
  assert.equal(noteReadCall(7, "read", READ_RUN), READ_RUN);
  // Restarting keeps a hint the model ignored from landing on every following result.
  assert.equal(noteReadCall(7, "read", READ_RUN + 1), 1);
});

test("a write, or a different conversation, breaks the streak", () => {
  forgetReads(8);
  forgetReads(9);
  noteReadCall(8, "read", 0);
  noteReadCall(8, "project_search", 1);
  // bash counts as reading here too: the shell is where most of the one-at-a-time reads happened.
  assert.equal(noteReadCall(8, "bash", 2), 3);
  assert.equal(noteReadCall(8, "edit", 3), 0, "a write ends the streak");
  assert.equal(noteReadCall(8, "read", 4), 1);
  assert.equal(noteReadCall(9, "read", 5), 1);
});

test("the hint asks for batching without demanding it", () => {
  const hint = batchingHint();
  assert.match(hint, /一条一条/);
  assert.match(hint, /project_search/);
  // A genuine read-then-decide is not the thing being discouraged.
  assert.match(hint, /前一条的答案决定了下一条/);
});

test("a run's shape is counted from the entries it left, not from a clock", () => {
  const entries = [
    { kind: "pi.user", model: [{ content: [{ type: "text", text: "go" }] }] },
    {
      kind: "pi.assistant",
      model: [
        { content: [{ type: "toolCall", name: "bash" }, { type: "toolCall", name: "read" }] },
      ],
    },
    { kind: "pi.tool-result", model: [] },
    { kind: "pi.assistant", model: [{ content: [{ type: "toolCall", name: "read" }] }] },
    { kind: "pi.assistant", model: [{ content: [{ type: "text", text: "done" }] }] },
  ];
  assert.deepEqual(shapeOf(entries), { rounds: 3, calls: 3, tools: { bash: 1, read: 2 } });
});

test("a row keeps what the log is queried by", () => {
  const row = turnRow("oc_x", "answer", { rounds: 2, calls: 3, tools: { bash: 3 } }, new Date(0));
  assert.deepEqual(row, {
    at: "1970-01-01T00:00:00.000Z",
    conversation: "oc_x",
    reason: "answer",
    rounds: 2,
    calls: 3,
    tools: { bash: 3 },
  });
});
