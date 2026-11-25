import assert from "node:assert/strict";
import { test } from "node:test";
import { parseModelCommand } from "../src/feishu.ts";

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
