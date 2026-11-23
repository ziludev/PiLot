import assert from "node:assert/strict";
import { test } from "node:test";
import { advance, hintLine, renderSteps, skillOf, stepCount, toolIcon, withIcons, type Trail } from "../src/steps.ts";

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

test("a skill call wears the skill, not the shell", () => {
  // The shape the agent writes: the skill's folder, then a script inside it.
  const cmd =
    `export SKILL_DIR="/Users/alexwang/.pi/agent/skills/presence-diagnose"; for q in 'draw'; ` +
    `do bash "$SKILL_DIR/scripts/sls.sh" query dev dora-dev-stdout "$q"; done`;
  assert.equal(skillOf(cmd), "presence-diagnose");
  assert.equal(toolIcon("bash", cmd), "🧩", "a skill is not a keyboard");
  // Everything that is not a skill still reads its command.
  assert.equal(toolIcon("bash", "git status"), "🌿");
  assert.equal(skillOf("git status"), undefined);
  // A hint cut mid-name must not pass the fragment off as the skill.
  assert.equal(skillOf('export SKILL_DIR="/Users/alexwang/.pi/agent/skills/presence-dia'), undefined);
});

test("the step line names the skill instead of its SKILL_DIR preamble", () => {
  let trail: Trail = { rounds: [], since: 0 };
  const hint = 'export SKILL_DIR="/Users/alexwang/.pi/agent/skills/lark-doc"; sed -n 1,80p "$SKILL_DIR/SKILL.md"';
  trail = advance(trail, live(["bash"], ["a"]), 0, hint);
  const card = renderSteps(trail, 10_000);
  assert.match(card, /🧩 bash `lark-doc`/);
  assert.match(card, /`lark-doc`/);
  assert.ok(!card.includes("SKILL_DIR"), `still showing the preamble: ${card}`);
});

test("two parallel calls in one round are one entry with a count", () => {
  // `bash · bash` used to draw the keyboard twice: one fact, told twice, on a line the card cannot spare.
  assert.equal(withIcons("bash · bash"), "⌨️ bash ×2");
  assert.equal(withIcons("bash · read · bash"), "⌨️ bash ×2 · 📄 read");
  assert.equal(withIcons("bash"), "⌨️ bash");
});

test("a round of parallel calls still names each of them once", () => {
  let trail: Trail = { rounds: [], since: 0 };
  trail = advance(trail, live(["bash", "bash", "read"], ["a", "b", "c"]), 0);
  trail = advance(trail, live(["grep"], ["d"]), 2_000);
  const lines = renderSteps(trail, 3_000).split("\n");
  assert.equal(lines[0], "✓ ⌨️ bash ×2 · 📄 read · 2s");
  assert.equal(lines[1], "▸ 🔍 grep · 1s");
  assert.equal(trail.rounds[0].label, "bash · bash · read", "the record keeps every call");
});

test("the last output line is cut like an argument, so a path cannot fold the card", () => {
  // What the card showed: `· ./chat-service-infra/src/…/ReplyOnBehalfVisibility.java` on four lines.
  const path =
    "./chat-service-infra/src/main/java/ai/sitin/chat/service/infra/service/ReplyOnBehalfVisibility.java";
  let trail: Trail = { rounds: [], since: 0 };
  trail = advance(trail, live(["bash", "bash"], ["a", "b"], path), 0);
  trail = advance(trail, live(["read"], ["c"]), 1_500);
  const line = renderSteps(trail, 2_000).split("\n")[0] ?? "";
  assert.match(line, /^✓ ⌨️ bash ×2 · 2s · \.\/chat-service-infra\/src\/m/);
  assert.ok(!line.includes("ReplyOnBehalfVisibility"), `tail was not cut: ${line}`);
  assert.ok(line.length < 64, `line still too long (${line.length}): ${line}`);
});

test("a round that took less than a second does not print a zero", () => {
  // A real card showed three `0s` on six lines; the clock earns its place only when it says something.
  let trail: Trail = { rounds: [], since: 0 };
  trail = advance(trail, live(["bash"], ["a"]), 0);
  trail = advance(trail, live(["read"], ["b"]), 400);
  trail = advance(trail, live(["grep"], ["c"]), 2_400);
  const lines = renderSteps(trail, 3_000).split("\n");
  assert.equal(lines[0], "✓ ⌨️ bash", "400ms: no clock at all");
  assert.equal(lines[1], "✓ 📄 read · 2s");
});

test("an output tail that is a bare word is not worth the room", () => {
  // `db_query` on dev ends its output with the word `dev` — the shape of a value, not a fact.
  let trail: Trail = { rounds: [], since: 0 };
  trail = advance(trail, live(["db_query"], ["a"], "dev"), 0);
  trail = advance(trail, live(["db_query"], ["b"], "ok pitta/internal/app"), 1_000);
  trail = advance(trail, live(["read"], ["c"]), 3_000);
  const lines = renderSteps(trail, 4_000).split("\n");
  assert.equal(lines[0], "✓ 🗄️ db_query · 1s");
  assert.equal(lines[1], "✓ 🗄️ db_query · 2s · ok pitta/internal/app");
});

test("a command too long for the line drops its cd preamble, not its verb", () => {
  // Three of five lines on a real card began `cd ~/.presence/projects/sitin-server &&` and were
  // cut before the command they were announcing.
  const long = "cd /Users/alexwang/.presence/projects/sitin-server && sed -n 1,120p proto/chat.proto";
  assert.equal(hintLine(long), "sed -n 1,120p proto/chat.proto");
  // A short one fits whole and keeps its directory: that is where the run happened.
  assert.equal(hintLine("cd client-go && go test ./..."), "cd client-go && go test ./...");
});
