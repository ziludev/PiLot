import assert from "node:assert/strict";
import test from "node:test";
import { pickReportSurface } from "../src/access.ts";

/** A finished task reports in the owner's DM, not in the middle of a group conversation. */

test("a group gets a background report through the owner's DM", () => {
  assert.equal(pickReportSurface("oc_group", "oc_owner"), "oc_owner");
  assert.equal(pickReportSurface("oc_owner", "oc_owner"), "oc_owner", "the DM stays the DM");
});

test("with no DM learned yet, the report goes where the work was asked for", () => {
  assert.equal(pickReportSurface("oc_group", undefined), "oc_group");
  assert.equal(pickReportSurface("oc_group", ""), "oc_group");
});

test("a surface that is not a Feishu chat keeps its own notifier", () => {
  // The desktop client and the CLI draw their own transcript; redirecting them to a chat id
  // would send their reports to a place they never look.
  assert.equal(pickReportSurface("client:conv-7", "oc_owner"), "client:conv-7");
  assert.equal(pickReportSurface("", "oc_owner"), "");
});
