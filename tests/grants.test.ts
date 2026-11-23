import assert from "node:assert/strict";
import { test } from "node:test";
import { BLANKET, hasGrant, loadGrants, grantSession, NEVER_BLANKET } from "../src/grants.ts";

/**
 * Grants are the difference between "one click, one tool call" and "stop asking in this chat".
 * Two things must hold and are easy to break: a class grant covers only its own class, and a
 * blanket grant still leaves the destructive classes asking.
 */

const context = {} as never;

/** The same shape tests/member-turn.test.ts uses: one store keyed by doc kind. */
function fakeHarness(store: Map<string, Record<string, unknown>> = new Map()) {
  type Doc = { definition: { kind: string; initial: () => unknown } };
  const kindOf = (doc: Doc) => doc.definition.kind;
  const harness = {
    snapshot: async (doc: Doc) => structuredClone(store.get(kindOf(doc))),
    commit: async (body: (tx: unknown) => Promise<void>) => {
      const pending = new Map<string, Record<string, unknown>>();
      const open = (doc: Doc) => {
        if (!pending.has(kindOf(doc))) {
          const base = store.get(kindOf(doc)) ?? (doc.definition.initial() as Record<string, unknown>);
          pending.set(kindOf(doc), structuredClone(base) as Record<string, unknown>);
        }
        return pending.get(kindOf(doc)) as Record<string, unknown>;
      };
      await body({ doc: async (doc: never) => open(doc) });
      for (const [kind, doc] of pending) store.set(kind, doc);
    },
  };
  return { harness: harness as never, store };
}

test("a class grant covers that class only, in that conversation only", async () => {
  const { harness } = fakeHarness();
  await loadGrants(harness, context);
  await grantSession(harness, context, 7, "write-outside");
  assert.equal(hasGrant(7, "write-outside"), true);
  assert.equal(hasGrant(7, "rm"), false);
  assert.equal(hasGrant(8, "write-outside"), false);
});

test("a blanket grant covers every class except the ones that destroy data", async () => {
  const { harness } = fakeHarness();
  await loadGrants(harness, context);
  await grantSession(harness, context, 7, BLANKET);
  for (const key of ["write-outside", "feishu-write", "force-push", "git-clean", "reset-hard", "npm-publish"]) {
    assert.equal(hasGrant(7, key), true, `${key} should be covered by the blanket grant`);
  }
  for (const key of NEVER_BLANKET) {
    assert.equal(hasGrant(7, key), false, `${key} must still ask after a blanket grant`);
  }
  assert.equal(hasGrant(8, "write-outside"), false);
});

test("grants survive a reload and a class grant can sit beside a blanket one", async () => {
  const { harness, store } = fakeHarness();
  await loadGrants(harness, context);
  await grantSession(harness, context, 7, BLANKET);
  // A class the blanket refused; answering its card once adds it without dropping "*".
  await grantSession(harness, context, 7, "sudo");
  assert.equal(hasGrant(7, "sudo"), true);

  const reloaded = fakeHarness(store);
  await loadGrants(reloaded.harness, context);
  assert.equal(hasGrant(7, "write-outside"), true);
  assert.equal(hasGrant(7, "sudo"), true);
  assert.equal(hasGrant(7, "rm"), false);
});

test("a conversation nobody granted anything in still asks", () => {
  // hasGrant is called from the guard on every tool call; a false here means "ask".
  assert.equal(hasGrant(424242, "write-outside"), false);
});
