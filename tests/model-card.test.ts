import assert from "node:assert/strict";
import { test } from "node:test";
import { parseModelCommand, pickModelChoices, resolveModelPick } from "../src/feishu.ts";

/**
 * `/model` in the Feishu chat. The line under an answer card used to be tested here too; it now
 * carries only the files a turn touched (`touchedLine`, pinned in turn-touched.test.ts).
 */

test("/model forms", () => {
  assert.deepEqual(parseModelCommand(""), { kind: "list" });
  assert.deepEqual(parseModelCommand("   "), { kind: "list" });
  assert.deepEqual(parseModelCommand("vendor/model-pro"), {
    kind: "set",
    modelId: "vendor/model-pro",
  });
  assert.deepEqual(parseModelCommand("default auto/glm-5.3"), { kind: "default", modelId: "auto/glm-5.3" });
  assert.deepEqual(parseModelCommand("thinking high"), { kind: "thinking", level: "high" });
  // A model id that merely starts with a keyword is still an id.
  assert.deepEqual(parseModelCommand("defaultish/x"), { kind: "set", modelId: "defaultish/x" });
});

test("resolveModelPick works across providers and slashed magpie ids", () => {
  const catalog = [
    { provider: "magpie", id: "auto/glm-5.3", thinkingLevels: [] },
    { provider: "magpie", id: "sitin-deepseek/deepseek-v3", thinkingLevels: ["low", "high"] },
    { provider: "openai", id: "gpt-4o", thinkingLevels: [] },
    { provider: "anthropic", id: "claude-sonnet-4-5", thinkingLevels: ["medium", "high"] },
  ];

  assert.equal(resolveModelPick(catalog, "gpt-4o", "magpie")?.provider, "openai");
  assert.equal(resolveModelPick(catalog, "anthropic/claude-sonnet-4-5", "openai")?.id, "claude-sonnet-4-5");
  assert.equal(resolveModelPick(catalog, "auto/glm-5.3", "magpie")?.provider, "magpie");
  assert.equal(resolveModelPick(catalog, "missing-model", "openai"), undefined);
});

test("pickModelChoices scopes to active provider and falls back cleanly", () => {
  const catalog = [
    { provider: "magpie", id: "auto/x", thinkingLevels: [] },
    { provider: "magpie", id: "sitin-deepseek/deepseek-v3", thinkingLevels: [] },
    { provider: "openai", id: "gpt-4o", thinkingLevels: [] },
    { provider: "openai", id: "o3", thinkingLevels: [] },
  ];

  const openaiPicks = pickModelChoices(catalog, "openai", "gpt-4o");
  assert.deepEqual(
    openaiPicks.map((item) => `${item.provider}/${item.id}`),
    ["openai/gpt-4o", "openai/o3"],
  );

  const magpiePicks = pickModelChoices(catalog, "magpie", "sitin-deepseek/deepseek-v3");
  assert.deepEqual(
    magpiePicks.map((item) => item.id),
    ["sitin-deepseek/deepseek-v3"],
  );
});
