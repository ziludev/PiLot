import assert from "node:assert/strict";
import { test } from "node:test";
import { splitTables, stripInlineCode } from "../src/feishu.ts";

const TABLE = `近 7 天共 3479 次。

| 接口 | 通过 | 窗口 | 合计 |
|---|---|---|---|
| 24002 \`/paymentApi/submitCashOut\` | 2173 | 1142 | 3315 |
| 4908 | 149 | 14 | 163 |

依据是日志。`;

test("a table between paragraphs keeps its place", () => {
  const parts = splitTables(TABLE);
  assert.equal(parts.length, 3);
  assert.equal(typeof parts[0], "string");
  const table = parts[1];
  assert.ok(typeof table !== "string");
  assert.deepEqual(table.columns, ["接口", "通过", "窗口", "合计"]);
  assert.equal(table.rows.length, 2);
  assert.deepEqual(table.rows[0], ["24002 `/paymentApi/submitCashOut`", "2173", "1142", "3315"]);
  assert.match(String(parts[2]), /依据是日志/);
});

test("pipes without a rule row are prose, not a table", () => {
  const parts = splitTables("a | b\nc | d");
  assert.equal(parts.length, 1);
  assert.equal(typeof parts[0], "string");
});

test("a short row is padded, an escaped pipe stays in its cell", () => {
  const parts = splitTables("| a | b |\n| --- | --- |\n| 1 \\| 2 |\n| 3 | 4 | 5 |");
  const table = parts[0];
  assert.ok(typeof table !== "string");
  assert.deepEqual(table.rows, [
    ["1 | 2", ""],
    ["3", "4"],
  ]);
});

test("alignment colons and no outer pipes still read as a table", () => {
  const parts = splitTables("a | b\n:--- | ---:\n1 | 2");
  const table = parts[0];
  assert.ok(typeof table !== "string");
  assert.deepEqual(table.columns, ["a", "b"]);
  assert.deepEqual(table.rows, [["1", "2"]]);
});

test("inline code loses its ticks, a fenced block keeps them", () => {
  assert.equal(stripInlineCode("run `npm test` now"), "run npm test now");
  assert.equal(stripInlineCode("```\nconst a = `x`;\n```"), "```\nconst a = `x`;\n```");
  assert.equal(stripInlineCode("`a` and\n```\nb\n```\n`c`"), "a and\n```\nb\n```\nc");
});
