import assert from "node:assert/strict";
import { test } from "node:test";
import { ROLES, toolsFor, type RoleName } from "../src/roles.ts";

const roles = Object.keys(ROLES) as RoleName[];

test("every role can search the memory its own prompt points at", () => {
  // The work-memory index sits in every child's system prompt with "search it with recall"; a role
  // without the tool is told to do something it cannot do.
  for (const role of roles) {
    const names = toolsFor(role).tools.map((tool) => tool.name);
    assert.ok(names.includes("recall"), `${role} is told to recall but cannot`);
  }
});

test("adding recall does not widen a read-only role", () => {
  const names = toolsFor("reviewer").tools.map((tool) => tool.name).sort();
  assert.deepEqual(names, ["bash", "read", "recall"]);
  assert.equal(names.includes("write"), false);
  assert.equal(names.includes("edit"), false);
});

test("an implementer still gets the full coding set plus recall", () => {
  const names = toolsFor("implementer").tools.map((tool) => tool.name).sort();
  assert.deepEqual(names, ["bash", "edit", "read", "recall", "write"]);
});

test("the reviewer is told to cite a location and to say UNKNOWN out loud", () => {
  const text = ROLES.reviewer.instructions;
  assert.match(text, /文件:行/);
  assert.match(text, /UNKNOWN/);
});

test("the designer is told to write its element list as the denominator first", () => {
  const text = ROLES.designer.instructions;
  assert.match(text, /清单/);
  assert.match(text, /分母/);
});

test("no role is allowed to call an unrun verification a pass", () => {
  assert.match(ROLES.implementer.instructions, /没有真跑过的验证不许写成「通过」/);
  assert.match(ROLES.tester.instructions, /汇总行/);
});
