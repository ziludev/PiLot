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

test("a decision buried in the design is lifted to the top of the report", async () => {
  const { buildSummary } = await import("../src/subagents.ts");
  const summary = buildSummary(
    "加限流",
    {
      "feature-design": "## 目标\n做限流\n\n## 需要拍板\n- 限流阈值：60/min 还是 120/min？建议 60。\n- 要不要同时改网关：建议先不动。\n\n## 方案\n令牌桶",
    },
    [{ key: "feature-design" }],
    {},
  );
  assert.match(summary, /要你拍板/);
  assert.match(summary, /60\/min/);
  assert.ok(summary.indexOf("要你拍板") < summary.indexOf("需求：加限流") + 40, "拍板在最上面");
});

test("a design that says nothing needs a decision adds no empty block", async () => {
  const { buildSummary } = await import("../src/subagents.ts");
  const summary = buildSummary("加限流", { "feature-design": "## 需要拍板\n无\n\n## 方案\n令牌桶" }, [], {});
  assert.doesNotMatch(summary, /要你拍板/);
});
