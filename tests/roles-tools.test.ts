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
