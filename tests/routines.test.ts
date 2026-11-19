import assert from "node:assert/strict";
import { test } from "node:test";
import { describe, nextOccurrence, parseSchedule, type Schedule } from "../src/routines.ts";

/**
 * A routine is only as good as its calendar. The tests that matter are the edges: the instant a
 * schedule fires is *not* the instant it fires again, "weekdays" must skip a weekend without an
 * unbounded loop, and "every 6h" must stay on its grid across a restart rather than drift by
 * however long the process was down.
 */

const at = (iso: string): number => Date.parse(iso);
const iso = (ms: number): string => new Date(ms).toISOString();

const schedule = (raw: string): Schedule => {
  const parsed = parseSchedule(raw);
  assert.equal(typeof parsed, "object", `expected a schedule for ${raw}, got: ${String(parsed)}`);
  return parsed as Schedule;
};

test("a bare clock is a daily one", () => {
  assert.deepEqual(schedule("09:00"), { kind: "daily", at: 540 });
  assert.deepEqual(schedule("7:05"), { kind: "daily", at: 425 });
});

test("the Chinese spellings the owner actually types parse too", () => {
  assert.deepEqual(schedule("每天 9:30"), { kind: "daily", at: 570 });
  assert.deepEqual(schedule("工作日 09:00"), { kind: "weekdays", at: 540 });
  assert.deepEqual(schedule("每周一 09:00"), { kind: "weekly", weekday: 1, at: 540 });
  assert.deepEqual(schedule("每周日 21:00"), { kind: "weekly", weekday: 0, at: 1260 });
});

test("weekly takes a name, a number or a contraction", () => {
  assert.deepEqual(schedule("weekly mon 09:00"), { kind: "weekly", weekday: 1, at: 540 });
  assert.deepEqual(schedule("weekly 3 12:00"), { kind: "weekly", weekday: 3, at: 720 });
  assert.deepEqual(schedule("weekly thu 08:15"), { kind: "weekly", weekday: 4, at: 495 });
});

test("every accepts minutes, hours and days", () => {
  assert.deepEqual(schedule("every 30m"), { kind: "every", everyMs: 1_800_000 });
  assert.deepEqual(schedule("every 6h"), { kind: "every", everyMs: 21_600_000 });
  assert.deepEqual(schedule("every 2d"), { kind: "every", everyMs: 172_800_000 });
  assert.deepEqual(schedule("every 30 minutes"), { kind: "every", everyMs: 1_800_000 });
});

test("a schedule that cannot work comes back as the sentence to say", () => {
  // Not a thrown error: the caller is a tool, and a tool that throws loses the correction.
  assert.match(String(parseSchedule("every 1m")), /最快 5 分钟/);
  assert.match(String(parseSchedule("每天 25:00")), /看不出几点/);
  assert.match(String(parseSchedule("weekly funday 09:00")), /看不出星期几/);
  assert.match(String(parseSchedule("买菜")), /看不懂的周期/);
});

test("daily fires today, and the same time tomorrow once it has passed", () => {
  const spec = schedule("daily 09:00");
  const before = at("2026-11-19T08:59:00+08:00");
  assert.equal(iso(nextOccurrence(spec, before, before)), iso(at("2026-11-19T09:00:00+08:00")));
  // The instant it fires is already in the past for the next computation: no double fire.
  const firing = at("2026-11-19T09:00:00+08:00");
  assert.equal(iso(nextOccurrence(spec, firing, firing)), iso(at("2026-11-20T09:00:00+08:00")));
});

test("the wall clock is Shanghai, not UTC", () => {
  const spec = schedule("daily 09:00");
  // 00:30 UTC on the 20th is 08:30 in Shanghai: still the same morning, still today's 09:00.
  const now = at("2026-11-20T00:30:00Z");
  assert.equal(iso(nextOccurrence(spec, now, now)), iso(at("2026-11-20T09:00:00+08:00")));
});

test("weekdays skips the weekend", () => {
  const spec = schedule("weekdays 09:00");
  // Friday 2026-11-20 10:00 → Monday the 23rd, not Saturday the 21st.
  const friday = at("2026-11-20T10:00:00+08:00");
  assert.equal(iso(nextOccurrence(spec, friday, friday)), iso(at("2026-11-23T09:00:00+08:00")));
});

test("weekly lands on its own weekday", () => {
  const spec = schedule("weekly mon 09:00");
  // Thursday the 19th → Monday the 23rd.
  const thursday = at("2026-11-19T00:00:00+08:00");
  assert.equal(iso(nextOccurrence(spec, thursday, thursday)), iso(at("2026-11-23T09:00:00+08:00")));
  // Monday 08:00 → later the same day.
  const monday = at("2026-11-23T08:00:00+08:00");
  assert.equal(iso(nextOccurrence(spec, monday, monday)), iso(at("2026-11-23T09:00:00+08:00")));
});

test("an interval keeps its grid instead of drifting with downtime", () => {
  const spec = schedule("every 6h");
  const anchor = at("2026-11-19T00:00:00+08:00");
  // Up for 7 hours: the 00:00 and 06:00 slots are gone, the next one is 12:00.
  const now = anchor + 7 * 3_600_000;
  assert.equal(nextOccurrence(spec, now, anchor), anchor + 12 * 3_600_000);
  // Down for a week: one catch-up fire, then back on the grid — not a week of backlog.
  const back = anchor + 7 * 86_400_000 + 1;
  assert.equal(nextOccurrence(spec, back, anchor), anchor + 7 * 86_400_000 + 6 * 3_600_000);
});

test("descriptions read like the sentence the owner would use", () => {
  assert.equal(describe(schedule("daily 09:00")), "每天 09:00");
  assert.equal(describe(schedule("weekdays 09:30")), "工作日 09:30");
  assert.equal(describe(schedule("weekly mon 09:00")), "每周一 09:00");
  assert.equal(describe(schedule("every 30m")), "每 30 分钟");
  assert.equal(describe(schedule("every 6h")), "每 6 小时");
  assert.equal(describe(schedule("every 2d")), "每 2 天");
});
