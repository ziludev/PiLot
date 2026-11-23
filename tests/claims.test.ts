import assert from "node:assert/strict";
import { test } from "node:test";
import {
  auditBrief,
  parseVerdicts,
  renderTally,
  repoFromSpec,
  tally,
  unaudited,
  type AuditState,
  type Report,
} from "../src/claims.ts";

const report = (taskId: number, repo?: string): Report => ({
  taskId,
  conversationId: 1,
  spec: "修一下登录限流",
  report: "我加了令牌桶，测试通过。",
  repo,
});

test("verdicts are read from the verifier's own file, one line each", () => {
  const verdicts = parseVerdicts(
    [
      "CLAIM 2 WRONG src/login.ts:41 限流器根本没接进路由",
      "CLAIM 1 GROUNDED src/login.ts:12",
      "CLAIM 3 UNKNOWN 分支已经 rebase，找不到那个文件",
      "顺便说一句：整体还行",
    ].join("\n"),
  );
  assert.deepEqual(
    verdicts.map((v) => [v.claim, v.status]),
    [
      [1, "GROUNDED"],
      [2, "WRONG"],
      [3, "UNKNOWN"],
    ],
  );
  assert.equal(verdicts[1]!.why, "src/login.ts:41 限流器根本没接进路由");
});

test("the error rate divides by judged claims, not by everything including unknowns", () => {
  const state: AuditState = {
    version: 1,
    runs: [
      {
        taskId: 1,
        at: "2026-11-24T00:00:00Z",
        verdicts: parseVerdicts("CLAIM 1 GROUNDED a.ts:1\nCLAIM 2 WRONG a.ts:9\nCLAIM 3 UNKNOWN b.ts:3"),
      },
    ],
  };
  assert.deepEqual(tally(state.runs), { claims: 3, grounded: 1, wrong: 1, unknown: 1, rate: 0.5 });
  assert.match(renderTally(state), /错误率 50\.0%/);
});

test("a task is audited once, so a fixed bug is not counted twice", () => {
  const state: AuditState = { version: 1, runs: [{ taskId: 2, at: "x", verdicts: [] }] };
  assert.deepEqual(
    unaudited([report(1), report(2), report(3)], state, 5).map((r) => r.taskId),
    [1, 3],
  );
});

test("a path in the spec is where the verifier looks", () => {
  assert.equal(repoFromSpec("在 /Users/alexwang/work/sitin-server (Java) 改"), "/Users/alexwang/work/sitin-server");
  assert.equal(repoFromSpec("仓库 ~/work/sitin-dhfix，worktree"), "~/work/sitin-dhfix");
  assert.equal(repoFromSpec("没有路径的需求"), undefined);
});

test("the verifier is told to distrust the report and write to a file", () => {
  const brief = auditBrief(report(7, "/Users/alexwang/x"), "/tmp/audit-7.txt");
  assert.match(brief, /不要相信报告里/);
  assert.match(brief, /\/tmp\/audit-7\.txt/);
  assert.match(brief, /文件:行/);
});
