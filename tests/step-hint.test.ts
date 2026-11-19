import assert from "node:assert/strict";
import { test } from "node:test";
import { stepHint } from "../src/details.ts";
import { stepHeadline } from "../src/feishu.ts";

/** Forty `bash` rows in one turn: the hint is what makes them readable, so it has to be right. */
test("the hint is the argument that answers what the call did", () => {
  assert.equal(
    stepHint("bash", '{"command":"cd /repo && git status --porcelain=v1"}'),
    "cd /repo && git status --porcelain=v1",
  );
  assert.equal(stepHint("write", '{"path":"src/steps.ts","content":"…"}'), "src/steps.ts");
  assert.equal(stepHint("grep", '{"pattern":"turnMeta","path":"src"}'), "turnMeta");
  assert.equal(stepHint("read", { file_path: "/tmp/a.md" }), "/tmp/a.md");
});

test("a multi-line command becomes one line, cut short", () => {
  const hint = stepHint("bash", JSON.stringify({ command: "python3 - <<'PY'\nimport os\nprint(1)\nPY" }));
  assert.equal(hint.includes("\n"), false);
  assert.ok(hint.startsWith("python3 - <<'PY' import os"), hint);

  const long = stepHint("bash", JSON.stringify({ command: "x".repeat(200) }));
  assert.equal(long.length, 60);
  assert.ok(long.endsWith("…"));
});

test("nothing to say prints nothing, not a stray string", () => {
  assert.equal(stepHint("bash", undefined), "");
  assert.equal(stepHint("bash", "not json"), "");
  assert.equal(stepHint("bash", "{}"), "");
  assert.equal(stepHint("recall", '{"query":"   "}'), "");
});

test("the headline keeps the tool's own name and drops a backtick that would break the span", () => {
  assert.equal(stepHeadline({ name: "bash" }), "**bash**");
  assert.equal(stepHeadline({ name: "bash", hint: "" }), "**bash**");
  assert.equal(stepHeadline({ name: "bash", hint: "git status" }), "**bash** `git status`");
  assert.equal(stepHeadline({ name: "bash", hint: "cd `pwd`" }), "**bash** `cd 'pwd'`");
});
