import assert from "node:assert/strict";
import { test } from "node:test";
import { advance, renderSteps, stepCount, toolIcon, withIcons, type Trail } from "../src/steps.ts";

/** A running card has to answer "what did it do so far", not just "something is happening". */

const live = (tools: string[], callIds: string[], activity?: string) => ({
  text: "",
  tools,
  callIds,
  ...(activity === undefined ? {} : { activity }),
  busy: true,
});

test("two bash rounds in a row stay two steps", () => {
  let trail: Trail = { rounds: [], since: 0 };
  trail = advance(trail, live(["bash"], ["a"], "cloning"), 1_000);
  trail = advance(trail, live(["bash"], ["b"]), 13_000);
  assert.equal(trail.rounds.length, 1);
  assert.deepEqual(trail.rounds[0], { label: "bash", ms: 12_000, tail: "cloning" });
  trail = advance(trail, live(["read"], ["c"]), 14_000);
  assert.equal(trail.rounds.length, 2);
  assert.deepEqual(trail.rounds[1], { label: "bash", ms: 1_000 });
  assert.deepEqual(stepCount(trail), { done: 2, running: true });
});

test("the running round keeps its last output line, so a hang is visible", () => {
  let trail: Trail = { rounds: [], since: 0 };
  trail = advance(trail, live(["bash"], ["a"], "waiting for ci"), 0);
  trail = advance(trail, live(["bash"], ["a"]), 5_000);
  const text = renderSteps(trail, 65_000);
  assert.equal(text, "▸ ⌨️ bash · 1m05s");
  assert.equal(trail.tail, "waiting for ci", "the tail survives a snapshot without activity");
});

test("a long trail shows the newest steps and counts the rest", () => {
  let trail: Trail = { rounds: [], since: 0 };
  for (let i = 0; i < 9; i += 1) {
    trail = advance(trail, live([`tool${i}`], [`c${i}`]), i * 1_000);
  }
  trail = advance(trail, live(["bash"], ["now"]), 9_000);
  const lines = renderSteps(trail, 12_000).split("\n");
  assert.equal(lines[0], "⋯ 前面还有 4 步");
  assert.equal(lines.length, 7, "4 hidden, 5 shown, 1 running");
  assert.match(lines.at(-1) ?? "", /^▸ ⌨️ bash · 3s$/);
  assert.equal(stepCount(trail).done, 9);
});

test("a round is named by the tool the model called, not by a retelling of it", () => {
  let trail: Trail = { rounds: [], since: 0 };
  trail = advance(trail, { text: "", tools: ["bash"], callIds: ["call_1"], busy: true }, 1_000);
  trail = advance(trail, { text: "", tools: ["read"], callIds: ["call_2"], busy: true }, 9_000);
  // The arguments used to be rewritten into Chinese ("跑 npm test"), which read nicer and told
  // less: what the card must show is the call, so the name is the name.
  assert.equal(trail.rounds[0].label, "bash");
  assert.equal(trail.label, "read");
});

test("every tool on the card carries one icon, and an unknown one says nothing it cannot back", () => {
  assert.equal(withIcons("bash · read"), "⌨️ bash · 📄 read");
  assert.equal(withIcons("grep"), "🔍 grep");
  assert.equal(withIcons("feishu_doc_read"), "💬 feishu_doc_read");
  assert.equal(toolIcon("some_new_tool"), "⚙️", "unknown stays neutral");
  assert.equal(toolIcon("bash"), "⌨️", "same icon however the card is drawn");
});

test("a finished round and the running one wear the same icon for the same tool", () => {
  let trail: Trail = { rounds: [], since: 0 };
  trail = advance(trail, { text: "", tools: ["bash"], callIds: ["a"], busy: true }, 0);
  trail = advance(trail, { text: "", tools: ["grep"], callIds: ["b"], busy: true }, 2_000);
  const lines = renderSteps(trail, 5_000).split("\n");
  assert.equal(lines[0], "✓ ⌨️ bash · 2s");
  assert.equal(lines[1], "▸ 🔍 grep · 3s");
  assert.equal(trail.rounds[0].label, "bash", "the record stays the bare name");
});

test("what a bash round ran is part of the step, and the icon follows it", () => {
  // Two `bash` rounds with different commands are two different things even though the tool name
  // is the same; the line has to say which, or the card is a column of the word "bash".
  let trail: Trail = { rounds: [], since: 0 };
  trail = advance(trail, live(["bash"], ["a"]), 1_000, "git log --oneline -3");
  trail = advance(trail, live(["bash"], ["b"]), 3_000, "curl -s https://example.com");
  trail = advance(trail, live(["bash"], ["c"]), 5_000);
  const text = renderSteps(trail, 6_000);
  assert.equal(text.split("\n")[0], "✓ 🌿 bash `git log --oneline -3` · 2s");
  assert.match(text, /✓ 🌐 bash `curl -s https:\/\/example\.com` · 2s/);
});

test("a command longer than the line keeps its verb when it is cut", () => {
  // Cutting the end leaves `git commit -m "docs: …`, which still says what happened; cutting the
  // front would leave a wall of flags and hide the one word that matters.
  let trail: Trail = { rounds: [], since: 0 };
  const long = "npm test -- --reporter=dot --experimental-strip-types --test-name-pattern=everything";
  trail = advance(trail, live(["bash"], ["a"]), 0, `git commit -m "${long}"`);
  assert.match(renderSteps(trail, 1_000), /🌿 bash `git commit -m "npm test/);
  assert.match(renderSteps(trail, 1_000), /…`/);
});
