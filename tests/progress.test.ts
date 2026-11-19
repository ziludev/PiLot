import assert from "node:assert/strict";
import test from "node:test";

import { describeCall } from "../src/progress.ts";

test("a round is named by what it did", () => {
  assert.equal(describeCall("bash", { command: "npm test" }), "跑 npm test");
  assert.equal(
    describeCall("read", { path: "client-go/ui/rail.go" }),
    "读 rail.go",
  );
  assert.equal(describeCall("edit", { file_path: "/tmp/a/b.ts" }), "改 b.ts");
  assert.equal(
    describeCall("project_search", { pattern: "turn_limit" }),
    "搜 turn_limit",
  );
  assert.equal(
    describeCall("delegate", { role: "implementer", task: "改 rail 的标题" }),
    "让 implementer：改 rail 的标题",
  );
});

test("a long argument is cut, one line per round", () => {
  const long =
    "for f in $(ls | grep -v node_modules); do echo $f; done && npm test";
  const label = describeCall("bash", { command: long });
  assert.equal(label, `跑 ${long.slice(0, 28)}…`);
  assert.ok(!label.includes("\n"));
});

test("no argument, no verb: the bare tool name is the honest label", () => {
  assert.equal(describeCall("bash", undefined), "bash");
  assert.equal(describeCall("read", {}), "read");
  assert.equal(describeCall("some_mcp_tool", { anything: 1 }), "some_mcp_tool");
});
