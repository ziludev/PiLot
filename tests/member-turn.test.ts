import assert from "node:assert/strict";
import { test } from "node:test";
import { resolve } from "node:path";
import { approvalReason } from "../src/approval.ts";
import {
  adoptChatLevels,
  isIsolatedPolicy,
  levelOf,
  readRoots,
  setChatTurn,
  type ChatLevel,
} from "../src/access.ts";
import { queryWho } from "../src/diagnostics.ts";
import { groupInstructions } from "../src/feishu.ts";
import { config } from "../src/config.ts";

/**
 * The guest tier is gone, so the tests that used to check "what a guest may read" now check the
 * two things that replaced it: an old opened group still becomes a group (nobody loses access),
 * and a group turn is only marked as a member for memory and the audit trail — never for the
 * tool surface.
 *
 * Boundary cases live here because the parts that must NOT change are the quiet ones: the
 * adoption of legacy chats, the per-conversation memory flag, and the guard's behaviour when a
 * tool calls it without the arguments it expects.
 */

const context = {} as never;

/** A harness that only knows session docs, keyed by doc kind — enough for adoption and writes. */
type Store = Map<string, Record<string, unknown>>;

type Doc = { definition: { kind: string; initial: () => unknown } };

function sessionHarness(store: Store, writes: Store[] = []) {
  const kindOf = (doc: Doc) => doc.definition.kind;
  const read = async (doc: Doc) => {
    const current = store.get(kindOf(doc));
    if (current !== undefined) return structuredClone(current);
    return undefined;
  };
  const harness = {
    snapshot: async (doc: Doc) => read(doc),
    commit: async (body: (tx: unknown) => Promise<void>) => {
      const pending: Store = new Map();
      const open = (doc: Doc) => {
        if (!pending.has(kindOf(doc))) {
          const base = store.get(kindOf(doc)) ?? (doc.definition.initial() as Record<string, unknown>);
          pending.set(kindOf(doc), structuredClone(base) as Record<string, unknown>);
        }
        return pending.get(kindOf(doc)) as Record<string, unknown>;
      };
      await body({ doc: async (doc: never) => open(doc) });
      // Commit is all-or-nothing, like the real one: nothing lands if `body` throws.
      for (const [kind, doc] of pending) store.set(kind, doc);
      writes.push(pending);
    },
  };
  return harness as never;
}

function policyHarness() {
  const doc: Record<string, unknown> = { isolated: true, topic: "只聊这个群的订单问题" };
  const harness = {
    commit: async (body: (tx: { doc: () => Promise<Record<string, unknown>> }) => Promise<void>) => {
      await body({ doc: async () => doc });
    },
  };
  return { doc, harness: harness as never };
}

test("chats opened before the rename are adopted as groups, not dropped", async () => {
  // The two legacy lists came from /open and /allow of the guest era. Nobody who could already
  // talk to the bot in a group may end up closed because a flag was renamed.
  const store: Store = new Map([
    ["app.guest-chats", { ids: ["oc_guest"] }],
    ["app.allowed-chats", { extra: ["oc_allowed"] }],
  ]);
  const harness = sessionHarness(store);

  const levels = await adoptChatLevels(harness, context, ["oc_owner"]);

  assert.deepEqual(levels, { oc_guest: "group", oc_allowed: "group", oc_owner: "owner" });
  assert.deepEqual((store.get("app.chat-access") as { chats: Record<string, ChatLevel> }).chats, levels);
  const audit = store.get("app.audit") as { events: Array<{ detail?: string }> };
  assert.match(String(audit.events.at(-1)?.detail), /2 group\(s\)/, "one audit line says an upgrade opened these");
});

test("adoption never overwrites a level someone already chose", async () => {
  // A chat the owner explicitly closed must stay closed even though a legacy list still lists it.
  const store: Store = new Map([
    ["app.chat-access", { chats: { oc_closed: "closed", oc_owner: "owner" } }],
    ["app.guest-chats", { ids: ["oc_closed", "oc_owner"] }],
  ]);
  const harness = sessionHarness(store);

  const levels = await adoptChatLevels(harness, context, ["oc_owner"]);

  assert.equal(levels["oc_closed"], "closed");
  assert.equal(levels["oc_owner"], "owner", "an env chat is not downgraded to group");
});

test("nothing to adopt means no write at all", async () => {
  // Boundary: every startup calls adoption; on a settled install it must stay a pure read.
  const store: Store = new Map();
  const writes: Store[] = [];
  const levels = await adoptChatLevels(sessionHarness(store, writes), context, []);
  assert.deepEqual(levels, {});
  assert.equal(writes.length, 0);
});

test("levelOf falls back to .env only, and everything unknown is closed", () => {
  assert.equal(levelOf("oc_env", {}, ["oc_env"]), "owner");
  assert.equal(levelOf("oc_env", { oc_env: "group" }, ["oc_env"]), "group", "the stored level wins");
  assert.equal(levelOf("oc_new", {}, ["oc_env"]), "closed", "a stranger is not a member");
  assert.equal(levelOf("oc_new", {}, []), "closed");
});

test("a member turn is recorded as member and leaves the memory flag alone", async () => {
  const { doc, harness } = policyHarness();
  await setChatTurn(harness, context, 7, "member");
  assert.deepEqual(doc, { isolated: true, topic: "只聊这个群的订单问题", turn: "member" });
});

test("a turn recorded before the rename still reads as a member", () => {
  // `turn` is only compared against "owner"; an old "guest" value must not become "not a member".
  assert.equal(queryWho({ guest: true, turn: "guest" } as never), "member");
  assert.equal(queryWho({ isolated: true }), "member", "a missing turn is a member turn");
  assert.equal(queryWho({ isolated: true, turn: "owner" }), undefined);
});

test("isIsolatedPolicy only trusts an explicit true under either name", () => {
  assert.equal(isIsolatedPolicy({}), false);
  assert.equal(isIsolatedPolicy({ guest: false }), false);
  assert.equal(isIsolatedPolicy({ guest: true, isolated: false }), true, "a half-migrated doc still isolates");
  assert.equal(isIsolatedPolicy({ isolated: "true" } as never), false, "a string is not the flag");
});

test("readRoots keeps the project root that group questions need", async () => {
  const reader = (docs: Record<string, unknown>, fail?: string) =>
    ({
      snapshot: async (doc: Doc) => {
        const kind = doc.definition.kind;
        if (fail === kind) throw new Error("boom");
        return docs[kind];
      },
    }) as never;

  assert.deepEqual(await readRoots(reader({}), 1, context), [config.workspace]);
  assert.deepEqual(
    await readRoots(
      reader({
        "app.conversation-project": { byConversation: { "1": "p1" } },
        "app.projects": { projects: [{ id: "p1", name: "svc", path: "/Users/example/work/svc", createdAt: 0 }] },
      }),
      1,
      context,
    ),
    [config.workspace, "/Users/example/work/svc"],
  );
  // A link to a project that was removed must not cost the agent its own workspace.
  assert.deepEqual(
    await readRoots(reader({ "app.conversation-project": { byConversation: { "1": "gone" } } }), 1, context),
    [config.workspace],
  );
  assert.deepEqual(await readRoots(reader({}, "app.conversation-project"), 1, context), [config.workspace]);
});

test("the guard does not decide anything from the shape of the arguments", () => {
  // Error path: a tool that calls with missing or non-string arguments must neither throw nor ask.
  const outcome = (tool: string, args: Record<string, unknown>) => approvalReason(tool, args) === undefined;
  assert.equal(outcome("bash", {}), true);
  assert.equal(outcome("bash", { command: ["rm", "-rf", "/"] }), true);
  assert.equal(outcome("write", {}), true);
  assert.equal(outcome("write", { path: 42 }), true);
  assert.equal(outcome("edit", { path: "" }), true);
  assert.equal(outcome("mcp_some_tool", { anything: true }), true);
});

test("the workspace edge is decided by the resolved path, in both directions", () => {
  const asks = (path: string) => approvalReason("write", { path }) !== undefined;
  assert.equal(asks(config.workspace), false, "the workspace itself is writable");
  assert.equal(asks(resolve(config.workspace, "..", "workspace", "a.ts")), false, "a loop back inside is inside");
  assert.equal(asks(resolve(config.workspace, "..", "elsewhere", "a.ts")), true);
  assert.equal(asks("../elsewhere/a.ts"), true);
  assert.equal(asks("/tmp/a.ts"), config.workspace.startsWith("/tmp/") === false);
});

test("the group instructions describe one tool surface and keep the apology rule", () => {
  const text = groupInstructions();
  assert.match(text, /工具面对群里所有人一样/);
  assert.match(text, /read \/ project_search \/ logs_query \/ db_query \/ bash \/ write \/ edit/);
  assert.match(text, /审批卡/);
  assert.match(text, /群成员/, "the bracket convention still says member, not guest");
  assert.match(text, /旧规则已作废/, "without the auto-correct this paragraph is the only brake");
  assert.doesNotMatch(text, /访客/);
  assert.doesNotMatch(text, /只读白名单/);
  assert.equal(groupInstructions("只回答本群主题").endsWith("- 只回答本群主题"), true);
});
