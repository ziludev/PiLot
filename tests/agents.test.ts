import assert from "node:assert/strict";
import { test } from "node:test";
import { AGENT_TASKS, AGENT_TOOLS, Agents } from "../src/agents.ts";
import { buildStagePrompt, SUBAGENT_TOOLS } from "../src/subagents.ts";

test("the agent tools are named the way the model is told to call them", () => {
  const names = AGENT_TOOLS.map((tool) => tool.name);
  assert.deepEqual(names.sort(), ["list_agents", "spawn_agent", "stop_agent", "tell_agent"]);
  for (const name of names) assert.equal(SUBAGENT_TOOLS.some((tool) => tool.name === name), false, `${name} is not listed twice`);
});

test("spawn and tell explain that an answer comes back without waiting", () => {
  const spawn = AGENT_TOOLS.find((tool) => tool.name === "spawn_agent");
  assert.ok(spawn !== undefined);
  assert.match(spawn.description, /长期|接着聊/);
  assert.match(spawn.description, /立刻返回|发回来/);
});

test("the registry is per conversation, so two chats hire separately", () => {
  const definition = Agents.definition as { kind: string; scope: string };
  assert.equal(definition.kind, "app.agents");
  assert.equal(definition.scope, "conversation", "an agent hired in one chat is not on the payroll of another");
});

test("an agent's message is delivered by a background task, not by the turn that asked", () => {
  assert.equal(AGENT_TASKS.length, 1);
  const definition = AGENT_TASKS[0]!.definition as { name: string };
  assert.equal(definition.name, "app.agent-turn");
});

test("the review stage reads the diff, not just the reports", () => {
  const prompt = buildStagePrompt("feature-review", "加个按钮", {
    "feature-design": "设计要点",
    "feature-implement": "实现了",
    "feature-test": "12 个测试通过",
  });
  assert.match(prompt, /git diff/);
  assert.match(prompt, /12 个测试通过/, "the reviewer has to see what the change had to pass");
  assert.match(prompt, /最多三条/, "a review that lists everything is a review nobody reads");
});
