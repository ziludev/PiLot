import assert from "node:assert/strict";
import test from "node:test";

import { taskTitle } from "../src/task-title.ts";

test("a reminder is its own text", () => {
  assert.equal(taskTitle("app.reminder", { text: "  23:00 关灯 " }), "23:00 关灯");
});

test("a routine is the line that says what to do, not the one that says who it is", () => {
  const text = [
    "你是无人值守的流程复盘回合。alex 不在线。",
    "",
    "目标：从过去 24 小时的真实痕迹里找出流程哪里不对。",
    "另外：别动线上。",
  ].join("\n");
  assert.equal(taskTitle("app.routine", { text }), "从过去 24 小时的真实痕迹里找出流程哪里不对。");
});

test("a routine without a goal line falls back to its first line", () => {
  const text = "\n\n  盯 sitin4.4 buglist（每小时）。\n 其余无关。";
  assert.equal(taskTitle("app.routine", { text }), "盯 sitin4.4 buglist（每小时）。");
});

test("a long title is cut on one line", () => {
  const title = taskTitle("app.feature", { spec: "a".repeat(200) });
  assert.equal(title!.length, 61);
  assert.ok(title!.endsWith("…"));
});

test("kinds with nothing to say get no title", () => {
  assert.equal(taskTitle("pi.generation", { text: "hello" }), undefined);
  assert.equal(taskTitle("app.routine", undefined), undefined);
});
