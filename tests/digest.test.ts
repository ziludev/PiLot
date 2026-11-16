import assert from "node:assert/strict";
import { test } from "node:test";
import { formatDigest, nextDigestAt } from "../src/digest.ts";
import type { AuditEvent } from "../src/audit.ts";
import type { Verdict } from "../src/verdicts.ts";

/**
 * The digest is read at 08:00 with one question: did anything need me? So the tests are about
 * what is *missing* from it — achieved turns that need no line, empty nights that should not
 * invent a section, and a system-stopped turn that must not hide behind "no claim".
 */

const at = (hhmm: string): number => new Date(`2026-11-17T${hhmm}:00+08:00`).getTime();
const now = at("08:00");

const verdict = (over: Partial<Verdict>): Verdict => ({ at: at("02:10"), met: true, why: "npm test 131 pass", ...over });
const event = (over: Partial<AuditEvent>): AuditEvent => ({ at: at("03:00"), kind: "approve", ...over });

test("an empty night says so in one line", () => {
  assert.equal(formatDigest({ now, hours: 12, verdicts: [], audit: [] }), "近 12 小时没有动静。");
});

test("achieved turns are counted, not listed", () => {
  const body = formatDigest({ now, hours: 12, verdicts: [verdict({}), verdict({ at: at("03:10") })], audit: [] });
  assert.match(body, /干活 2 轮：2 轮报了达成/);
  assert.doesNotMatch(body, /npm test/, "an achieved turn needs no line of its own");
});

test("a turn the system stopped is a shortfall even with no claim", () => {
  const body = formatDigest({
    now,
    hours: 12,
    verdicts: [verdict({ met: false, why: "", reason: "turn_limit" })],
    audit: [],
  });
  assert.match(body, /· 02:10 没说明（到了步数上限）/);
});

test("unmet turns carry the reason they gave and a clock", () => {
  const body = formatDigest({
    now,
    hours: 12,
    verdicts: [verdict({ met: false, why: "卡在 contact 权限" })],
    audit: [],
  });
  assert.match(body, /· 02:10 卡在 contact 权限（未达成）/);
});

test("approvals and the audit tail are summarised", () => {
  const body = formatDigest({
    now,
    hours: 12,
    verdicts: [],
    audit: [event({ detail: "bash rm -rf /tmp/x" }), event({ at: at("04:00"), kind: "deny", tool: "write" })],
  });
  assert.match(body, /审批 1 次：bash rm -rf \/tmp\/x/);
  assert.match(body, /审计 2 条/);
  assert.match(body, /· 04:00 deny write/);
});

test("the last turn's age uses the shared elapsed wording", () => {
  const body = formatDigest({ now, hours: 12, verdicts: [verdict({ at: now - 90 * 60_000 })], audit: [] });
  assert.match(body, /最近一轮：06:30 起，1 小时前/);
});

test("the deadline is the next occurrence, today or tomorrow, in Shanghai", () => {
  const beforeEight = at("07:00");
  assert.equal(nextDigestAt(beforeEight, "08:30"), at("08:30"));
  const afterEight = at("09:00");
  assert.equal(nextDigestAt(afterEight, "08:30"), at("08:30") + 86_400_000);
});

test("an unset or broken deadline is not a deadline", () => {
  assert.equal(nextDigestAt(now, "off"), undefined);
  assert.equal(nextDigestAt(now, "25:00"), undefined);
  assert.equal(nextDigestAt(now, ""), undefined);
});
