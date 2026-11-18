import assert from "node:assert/strict";
import test from "node:test";
import { cutAtLine } from "../src/subagents.ts";

test("a long report is cut at a line, and says what is left", () => {
  const text = ["first line", "second line", "third line", "fourth line"].join("\n");
  const cut = cutAtLine(text, 30);
  assert.ok(cut.startsWith("first line\nsecond line"), cut);
  assert.match(cut, /还有约 \d+ 字/);
  assert.ok(!cut.includes("fourth line"));
});

test("a short report is left alone", () => {
  assert.equal(cutAtLine("done", 100), "done");
});
