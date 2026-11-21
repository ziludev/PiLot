import assert from "node:assert/strict";
import { test } from "node:test";
import type { Harness } from "@earendil-works/pi-durable";
import { proactiveOf, setProactive } from "../src/access.ts";

function fakePolicy(initial: Record<string, unknown> = {}) {
  const doc: Record<string, unknown> = { isolated: false, ...initial };
  const harness = {
    commit: async (fn: (tx: unknown) => Promise<unknown>) => fn({ doc: async () => doc }),
  } as unknown as Harness;
  return { harness, doc };
}

const context = {} as never;

test("a chat that never chose is auto, and auto is spelled as a missing field", async () => {
  const { harness, doc } = fakePolicy();
  assert.equal(await proactiveOf(harness, context, 1), "auto");
  await setProactive(harness, context, 1, "off");
  assert.equal(doc.proactive, "off");
  assert.equal(await proactiveOf(harness, context, 1), "off");
  await setProactive(harness, context, 1, "auto");
  assert.equal("proactive" in doc, false, "default is not written down");
  assert.equal(await proactiveOf(harness, context, 1), "auto");
});

test("an old document with no field still reads as auto", async () => {
  const { harness } = fakePolicy();
  assert.equal(await proactiveOf(harness, context, 7), "auto");
});
