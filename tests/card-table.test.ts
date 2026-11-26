import assert from "node:assert/strict";
import { test } from "node:test";
import { bodyElements, clip, MAX_CARD_CHARS, MAX_CARD_TABLES, splitTables, stripInlineCode } from "../src/feishu.ts";

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

test("a heading becomes a bold line, a rule becomes an hr element", () => {
  const elements = bodyElements("## 结论\n3479 次全通过\n---\n下一步");
  assert.deepEqual(elements, [
    { tag: "markdown", content: "**结论**\n3479 次全通过" },
    { tag: "hr" },
    { tag: "markdown", content: "下一步" },
  ]);
});

test("a table keeps its neighbours' order in a card body", () => {
  const elements = bodyElements(TABLE);
  assert.deepEqual(
    elements.map((element) => (element as { tag: string }).tag),
    ["markdown", "table", "markdown"],
  );
});

test("truncating a clipped answer closes the code fence it cut open", () => {
  const body = "```\n" + "x".repeat(MAX_CARD_CHARS) + "\n```";
  const cut = clip(body);
  const fences = cut.split("\n").filter((line) => line.trim() === "```").length;
  assert.equal(fences % 2, 0);
});

test("tables past MAX_CARD_TABLES fall back to fenced code blocks instead of overflowing the card", () => {
  const many = Array.from({ length: MAX_CARD_TABLES + 2 }, (_, i) => `| 表${i} | 值 |\n|---|---|\n| a | ${i} |`).join("\n\n");
  const elements = bodyElements(many);
  const tableElements = elements.filter((el) => el["tag"] === "table");
  const fallbackBlocks = elements.filter((el) => el["tag"] === "markdown" && String(el["content"] ?? "").includes("```"));
  assert.equal(tableElements.length, MAX_CARD_TABLES);
  assert.equal(fallbackBlocks.length, 2);
});
