import assert from "node:assert/strict";
import { test } from "node:test";
import { formatDecision, noteDecision, takeDecisions } from "../src/decisions.ts";

/**
 * The trail is per conversation and per turn: reading it clears it, so a turn that ends without
 * an answer cannot leak its notes onto the next card.
 */

test("notes come back in order, and only once", () => {
  noteDecision("42", { kind: "approval", text: "问你：write" }, 1_000);
  noteDecision("42", { kind: "approval", text: "你允许了 write" }, 2_000);
  const trail = takeDecisions("42");
  assert.deepEqual(
    trail.map((note) => note.text),
    ["问你：write", "你允许了 write"],
  );
  assert.equal(trail[0]?.at, 1_000);
  assert.deepEqual(takeDecisions("42"), [], "a second read finds nothing");
});

test("conversations do not share a trail", () => {
  noteDecision("7", { kind: "approval", text: "chat seven" });
  noteDecision("8", { kind: "approval", text: "chat eight" });
  assert.deepEqual(
    takeDecisions("7").map((note) => note.text),
    ["chat seven"],
  );
  assert.deepEqual(
    takeDecisions("8").map((note) => note.text),
    ["chat eight"],
  );
});

test("a loop that asks forever keeps the last twelve notes", () => {
  for (let index = 0; index < 15; index += 1) {
    noteDecision("9", { kind: "approval", text: `ask ${index}` }, index);
  }
  const trail = takeDecisions("9");
  assert.equal(trail.length, 12);
  assert.equal(trail[0]?.text, "ask 3");
  assert.equal(trail.at(-1)?.text, "ask 14");
});

test("a note reads as a clock and a sentence", () => {
  const at = new Date(2026, 0, 2, 9, 5).getTime();
  assert.equal(formatDecision({ at, kind: "stop", text: "结束：turn_limit" }), "09:05 结束：turn_limit");
});
