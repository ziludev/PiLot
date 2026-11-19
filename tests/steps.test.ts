import assert from "node:assert/strict";
import { test } from "node:test";
import { advance, renderSteps, stepCount, type Trail } from "../src/steps.ts";

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
  assert.equal(text, "▸ bash · 1m05s");
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
  assert.match(lines.at(-1) ?? "", /^▸ bash · 3s$/);
  assert.equal(stepCount(trail).done, 9);
});

test("a round is named from the arguments the model committed with it", async () => {
  const { readCallArgs } = await import("../src/live.ts");
  const entries = [
    { model: [{ role: "user", content: "check the build" }] },
    {
      model: [
        {
          role: "assistant",
          content: [
            { type: "text", text: "running it" },
            { type: "toolCall", id: "call_1", name: "bash", arguments: { command: "npm test" } },
          ],
        },
      ],
    },
    { model: [{ role: "toolResult", content: [{ type: "text", text: "ok" }] }] },
  ];
  assert.deepEqual(readCallArgs(entries), { call_1: { command: "npm test" } });

  let trail: Trail = { rounds: [], since: 0 };
  trail = advance(trail, { text: "", tools: ["bash"], callIds: ["call_1"], args: readCallArgs(entries), busy: true }, 1_000);
  trail = advance(trail, { text: "", tools: ["read"], callIds: ["call_2"], busy: true }, 9_000);
  assert.equal(trail.rounds[0].label, "跑 npm test");
  assert.equal(trail.label, "read");
});
