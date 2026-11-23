import assert from "node:assert/strict";
import { test } from "node:test";
import { cliFailure, parseDbQuery, parseLogsQuery, searchRoot } from "../src/diagnostics.ts";

/**
 * The two read-only tools exist so a colleague can search the code and look at logs without a
 * shell. Their limits have to hold on the arguments, since the blast radius is the tool.
 */

const WS = "/Users/example/learn/pitta/workspace";
const PROJECT = "/Users/example/work/example-service";

test("logs_query fills in sane defaults", () => {
  delete process.env["SLS_LOGSTORE_PREFIX"]; // the prefix is per-installation; assert the fallback
  const parsed = parseLogsQuery({ query: '"2100083235"' });
  assert.deepEqual(parsed, { env: "dev", logstore: "app-dev-stdout", query: '"2100083235"', minutes: 60, limit: 50 });
});

test("logs_query clamps the window and the row count", () => {
  const wide = parseLogsQuery({ query: "level: ERROR", minutes: 99_999, limit: 9_999 });
  assert.equal(typeof wide, "object");
  assert.equal((wide as { minutes: number }).minutes, 1440);
  assert.equal((wide as { limit: number }).limit, 200);
  const zero = parseLogsQuery({ query: "x", minutes: 0, limit: 0 });
  assert.equal((zero as { minutes: number }).minutes, 60);
});

test("logs_query refuses a bad environment, a suspicious logstore, and an empty query", () => {
  assert.match(String(parseLogsQuery({ query: "x", env: "staging" })), /dev 或 prod/);
  for (const logstore of ["../etc/passwd", "Dora-Dev-Stdout", "a", "app dev stdout'", "app;rm"]) {
    assert.match(String(parseLogsQuery({ query: "x", logstore })), /不合法/, logstore);
  }
  assert.match(String(parseLogsQuery({ query: "   " })), /不能为空/);
  assert.match(String(parseLogsQuery({ query: "x".repeat(501) })), /太长/);
});

test("project_search stays inside the chat's own project", () => {
  const roots = [WS, PROJECT];
  assert.deepEqual(searchRoot({}, roots), { ok: true, path: WS });
  assert.deepEqual(searchRoot({ path: "user-service/src" }, roots), { ok: true, path: `${WS}/user-service/src` });
  assert.equal(searchRoot({ path: `${PROJECT}/user-service/src` }, roots).ok, true);
  assert.equal(searchRoot({ path: "/Users/example/work/other-repo" }, roots).ok, false);
  assert.equal(searchRoot({ path: "/etc" }, roots).ok, false);
  // A sibling whose name starts the same is not inside the project.
  assert.equal(searchRoot({ path: "/Users/example/work/example-service-secrets" }, roots).ok, false);
  assert.equal(searchRoot({ path: "../../../etc" }, roots).ok, false);
});

test("db_query defaults to the dev cluster and clamps the row count", () => {
  assert.deepEqual(parseDbQuery({ sql: "select 1" }), {
    env: "dev",
    cliEnv: "test",
    source: "pgCfg",
    sql: "select 1",
    maxRows: 100,
  });
  const prod = parseDbQuery({ sql: "select 1", env: "prod", maxRows: 9_999 });
  assert.equal((prod as { cliEnv: string }).cliEnv, "prod");
  assert.equal((prod as { maxRows: number }).maxRows, 500);
  assert.equal((parseDbQuery({ sql: "select 1", maxRows: 0 }) as { maxRows: number }).maxRows, 100);
});

test("db_query only lets reads through, one statement at a time", () => {
  for (const sql of ["delete from user_coins", "update t set a = 1", "drop table t", "truncate t"]) {
    assert.match(String(parseDbQuery({ sql })), /只允许查询/, sql);
  }
  assert.match(String(parseDbQuery({ sql: "select 1; delete from t" })), /一条语句/);
  assert.equal(typeof parseDbQuery({ sql: "select 1 from t where note = 'a;b'" }), "object");
  // A keyword inside a string literal is data, not a statement.
  const literal = parseDbQuery({ sql: "select * from t where kind = 'create'" });
  assert.equal(typeof literal, "object");
});

test("db_query refuses a bad environment and a suspicious source name", () => {
  assert.match(String(parseDbQuery({ sql: "select 1", env: "staging" })), /dev 或 prod/);
  for (const source of ["../etc/passwd", "pg;drop", "pg$Cfg", "a b"]) {
    assert.match(String(parseDbQuery({ sql: "select 1", source })), /不合法/, source);
  }
  assert.match(String(parseDbQuery({ sql: "  " })), /不能为空/);
});

/**
 * A failed query used to surface as `Command failed: …node …/index.js db pg test …` — the psql
 * stderr was on stdout, so 24h of "column status does not exist" looked like a flaky tool and got
 * retried with another guess. The reason has to reach the model.
 */
test("a failing CLI call carries psql's own words, not just the command line", () => {
  const stdout = JSON.stringify({
    ok: false,
    error: {
      code: "UPSTREAM",
      message: "psql prod/pgCfg failed (exit 1)",
      detail: { stderr: 'ERROR:  column "status" does not exist\n第 1 行: SELECT status, …\n                ^', stdout: "" },
    },
  });
  const reason = cliFailure(stdout);
  assert.match(String(reason), /psql prod\/pgCfg failed \(exit 1\)/);
  assert.match(String(reason), /column "status" does not exist/);
  // A wall of psql context is noise next to the model's context budget.
  assert.equal(String(reason).split("\n").length, 1);
});

test("a failure that is not the CLI's JSON envelope keeps the original error", () => {
  assert.equal(cliFailure("not json"), undefined);
  assert.equal(cliFailure('{"ok":false,"error":{"code":"AUTH"}}'), "AUTH");
});
