import assert from "node:assert/strict";
import { test } from "node:test";
import { approvalCard } from "../src/feishu.ts";
import { approvalReason } from "../src/approval.ts";

/**
 * The card is the only way to answer from Feishu, so a wrong button payload is silent:
 * the click lands, nothing happens. These assert the shapes the handler reads.
 */
const request = {
  callId: "call-1",
  conversationId: 7,
  tool: "bash",
  summary: "rm -rf /tmp/x",
  reason: "删除文件：rm -rf /tmp/x",
  surfaceId: "oc_test",
};

type Button = { text: { content: string }; value: Record<string, unknown>; type?: string; disabled?: boolean };

function buttonsOf(card: Record<string, unknown>): Button[] {
  const body = card["body"] as { elements?: Array<Record<string, unknown>> };
  const rows = (body.elements ?? []).filter((element) => element["tag"] === "column_set");
  for (const row of rows) {
    const columns = (row["columns"] as Array<{ elements?: Array<Record<string, unknown>> }> | undefined) ?? [];
    const buttons = columns.flatMap((column) => (column.elements ?? []).filter((el) => el["tag"] === "button"));
    if (buttons.length > 0) return buttons as unknown as Button[];
  }
  return [];
}

function bodyTextOf(card: Record<string, unknown>): string {
  const parts: string[] = [];
  const walk = (node: unknown): void => {
    if (Array.isArray(node)) {
      for (const item of node) walk(item);
      return;
    }
    if (node !== null && typeof node === "object") {
      const record = node as Record<string, unknown>;
      if (typeof record["content"] === "string") parts.push(record["content"]);
      for (const value of Object.values(record)) walk(value);
    }
  };
  walk(card["body"]);
  return parts.join("\n");
}

test("the approval card is 2.0, because 1.0 cannot show a collapsible panel", () => {
  const built = approvalCard(request);
  assert.equal(built["schema"], "2.0");
  assert.equal(built["elements"], undefined);
  assert.ok(Array.isArray((built["body"] as { elements?: unknown[] }).elements));
});

test("a grantable call offers both conversation-wide answers", () => {
  const buttons = buttonsOf(approvalCard({ ...request, grantKey: "rm" }));
  const labels = buttons.map((button) => button.text.content);
  assert.deepEqual(labels, ["允许", "本会话允许这类", "本会话全部允许", "拒绝"]);
  const session = buttons.find((button) => button.text.content === "本会话允许这类");
  assert.equal(session?.value["approval"], "call-1");
  assert.equal(session?.value["ok"], true);
  assert.equal(session?.value["session"], true);
  assert.equal(session?.value["key"], "rm");
  assert.equal(session?.value["all"], undefined);
  // The blanket button carries `all`: the handler stores "*" for the conversation
  // and the class key is only there because the card is built from one.
  const blanket = buttons.find((button) => button.text.content === "本会话全部允许");
  assert.equal(blanket?.value["approval"], "call-1");
  assert.equal(blanket?.value["ok"], true);
  assert.equal(blanket?.value["session"], true);
  assert.equal(blanket?.value["all"], true);
});

test("a call with no grantable class keeps the two buttons", () => {
  const labels = buttonsOf(approvalCard(request)).map((button) => button.text.content);
  assert.deepEqual(labels, ["允许", "拒绝"]);
});

test("risk classes are stable strings the grant can key on", () => {
  assert.deepEqual(approvalReason("bash", { command: "rm -rf /tmp/x" }), { reason: "删除文件：rm -rf /tmp/x", key: "rm" });
  assert.equal(approvalReason("bash", { command: "git push --force" })?.key, "force-push");
  assert.equal(approvalReason("bash", { command: "ls -la" }), undefined);
  // A write outside the workspace is its own class; inside it needs no approval at all.
  const outside = approvalReason("write", { path: "/Users/someone/else/repo/a.ts" });
  assert.equal(outside?.key, "write-outside");
});

/**
 * A settled card is the record of what was allowed: it must keep the request it asked about and
 * the answer that was used, and it must stop accepting clicks. Replacing it with one line (what
 * this card did before) left the chat history with 「✅ 已允许」 and nothing it referred to.
 */
test("a settled card keeps the request and freezes every button, keeping the used one lit", () => {
  const built = approvalCard(
    { ...request, grantKey: "rm" },
    { ok: true, by: "feishu", pick: "all", waitedMs: 42_000 },
  );
  const text = bodyTextOf(built);
  assert.match(text, /rm -rf \/tmp\/x/, "the command it asked about is still on the card");
  const buttons = buttonsOf(built);
  assert.deepEqual(
    buttons.map((button) => button.text.content),
    ["允许", "本会话允许这类", "本会话全部允许", "拒绝"],
  );
  assert.ok(buttons.every((button) => button.disabled === true), "no button is clickable after the answer");
  assert.equal(buttons.find((button) => button.text.content === "本会话全部允许")?.type, "default");
  // The used one keeps its colour; the rest flatten to grey so the card shows one choice.
  const header = built["header"] as { template: string; title: { content: string } };
  assert.equal(header.template, "green");
  assert.match(header.title.content, /已允许/);
  assert.match(text, /选了「本会话全部允许」/, "the card names the answer that was used, not just yes");
  assert.match(text, /等了 42s/);
});

test("a card the owner answered from the desktop says so, and a timeout says nobody did", () => {
  const fromClient = approvalCard(request, { ok: false, by: "client", pick: "deny", waitedMs: 90_000 });
  const clientText = bodyTextOf(fromClient);
  assert.match(clientText, /桌面客户端选了「拒绝」/);
  assert.equal((fromClient["header"] as { template: string }).template, "red");

  const timedOut = approvalCard({ ...request, timeoutMs: 300_000 }, { ok: false, by: "timeout", waitedMs: 300_000 });
  const timedText = bodyTextOf(timedOut);
  assert.match(timedText, /300s 内没人回答/);
  // Nobody chose anything: no button may look like the answer.
  const types = buttonsOf(timedOut).map((button) => button.type);
  assert.deepEqual(types, ["default", "default"]);
  assert.ok(buttonsOf(timedOut).every((button) => button.disabled === true));
});

test("a pending card still counts down and stays clickable, and shows its source chat in header subtitle", () => {
  const built = approvalCard({ ...request, chatTitle: "研发排查群" });
  const header = built["header"] as {
    subtitle?: { content: string };
    icon?: { token: string; color?: string };
    text_tag_list?: Array<{ text: { content: string } }>;
  };
  assert.equal(header.subtitle?.content, "研发排查群");
  assert.equal(header.icon?.token, "warning_outlined");
  assert.equal(header.text_tag_list?.[0]?.text.content, "permission");
  const body = built["body"] as { elements: Array<Record<string, unknown>> };
  assert.equal(body.elements[0]?.["background_style"], "orange-50");
  const buttons = buttonsOf(built);
  assert.ok(buttons.every((button) => button.disabled === undefined));
  assert.equal(buttons[0]?.type, "primary");
});
