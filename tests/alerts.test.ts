import assert from "node:assert/strict";
import { test } from "node:test";
import type { Report } from "@earendil-works/pi-durable";
import { alertReport, alertText, forgetAlerts, isFresh } from "../src/alerts.ts";

const TASK = { id: 7 as never, kind: "app.feature", conversationId: 3 as never };

function report(info: Record<string, unknown>, error: unknown = new Error("boom")): Report {
  return { ...info, error } as Report;
}

test("a broken task reaches the chat with the task and the reason in it", () => {
  const text = alertText(report({ kind: "task_faulted", task: TASK, reason: "threw" }));
  assert.ok(text !== undefined);
  assert.match(text, /task_faulted/);
  assert.match(text, /app\.feature/);
  assert.match(text, /threw/);
  assert.match(text, /boom/);
});

test("closing storage is worth a message, a task's own report is not", () => {
  assert.match(alertText(report({ kind: "storage_failed" })) ?? "", /storage_failed/);
  assert.equal(alertText(report({ kind: "task_error", task: TASK })), undefined);
  assert.equal(alertText(report({ kind: "listener_error" })), undefined);
});

test("an extension failure names the extension, not a conversation id", () => {
  const text = alertText(report({ kind: "extension_error", extension: "codemode" })) ?? "";
  assert.match(text, /codemode/);
  assert.doesNotMatch(text, /\d{3,}/, "ids mean nothing to the person reading it");
});

test("the same failure is said once per window, a different one still gets through", () => {
  forgetAlerts();
  assert.equal(isFresh("task_faulted:app.feature:threw", 0), true);
  assert.equal(isFresh("task_faulted:app.feature:threw", 60_000), false);
  assert.equal(isFresh("task_faulted:app.feature:restart_limit", 60_000), true);
  assert.equal(isFresh("task_faulted:app.feature:threw", 11 * 60_000), true, "an hour later it is news again");
});

test("no home chat configured means no message and no throw", async () => {
  forgetAlerts();
  const sent: string[] = [];
  const ok = await alertReport(report({ kind: "storage_failed" }), async (_to, text) => void sent.push(text), undefined);
  assert.equal(ok, false);
  assert.deepEqual(sent, []);
});

test("a failed send is swallowed: alerts run where a throw would be reported back", async () => {
  forgetAlerts();
  const ok = await alertReport(
    report({ kind: "storage_failed" }),
    async () => {
      throw new Error("feishu down");
    },
    "chat-1",
  );
  assert.equal(ok, true, "it was worth sending; the transport failing is the transport's problem");
});
