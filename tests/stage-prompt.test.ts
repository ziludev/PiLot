import assert from "node:assert/strict";
import { test } from "node:test";
import { buildStagePrompt } from "../src/subagents.ts";

const spec = "给登录接口加限流";
const design = "设计摘要：令牌桶，按用户维度。";

test("a reviewer is handed the changed files and the spec, not the implementer's account", () => {
  const prompt = buildStagePrompt("feature-review", spec, {
    "feature-design": design,
    "feature-implement": "我实现了全部需求，构建通过，一切正常。",
    "feature-test": "12 个用例全部通过。",
  }, ["src/login.ts", "tests/login.test.ts"]);

  assert.match(prompt, /src\/login\.ts/);
  assert.match(prompt, /tests\/login\.test\.ts/);
  // The producer's own view is what makes a verifier miss the same thing: it must not be in here.
  assert.doesNotMatch(prompt, /我实现了全部需求/);
  assert.doesNotMatch(prompt, /12 个用例全部通过/);
  assert.match(prompt, /UNKNOWN/);
});

test("a reviewer with no machine scope is told to find the diff itself", () => {
  const prompt = buildStagePrompt("feature-review", spec, { "feature-design": design });
  assert.match(prompt, /git status/);
});

test("the other stages still see the work they build on", () => {
  const tester = buildStagePrompt("feature-test", spec, {
    "feature-design": design,
    "feature-implement": "改了 src/login.ts",
  });
  assert.match(tester, /改了 src\/login\.ts/);
});
