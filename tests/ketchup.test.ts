import assert from "node:assert/strict";
import { test } from "node:test";
import { elapsed, formatKetchup, withinWindow, type KetchupInput } from "../src/ketchup.ts";
import type { AuditEvent } from "../src/audit.ts";

/**
 * `/ketchup` is the answer to "怎么样了?", so its shapes are pinned: an empty window says so,
 * a run in flight is named, a waiting approval carries how long it has waited, and the audit
 * tail is capped instead of becoming the reply.
 */

const NOW = Date.UTC(2026, 10, 16, 10, 0, 0); // 18:00 in Asia/Shanghai

const base = (over: Partial<KetchupInput> = {}): KetchupInput => ({
  now: NOW,
  hours: 12,
  tasks: [],
  approvals: [],
  events: [],
  ...over,
});

test("a quiet window says so instead of showing empty sections", () => {
  assert.equal(formatKetchup(base()), "**最近 12 小时没有动静。**");
});

test("a run in flight and a task behind it", () => {
  const card = formatKetchup(
    base({
      tasks: [
        { title: "迁移调用方", state: "running" },
        { title: "每日简报", state: "sleeping", until: NOW + 2 * 3_600_000 },
      ],
    }),
  );
  assert.match(card, /在跑 1 件/);
  assert.match(card, /· 迁移调用方 · running/);
  assert.match(card, /排在后面 1 件/);
  assert.match(card, /· 每日简报 · sleeping · \d\d:\d\d 起来/);
});

test("an approval says what it waits for and for how long", () => {
  const card = formatKetchup(
    base({ approvals: [{ tool: "bash", summary: "rm -rf ./build", waitedMs: 7 * 60_000 }] }),
  );
  assert.match(card, /等你决定 1 件/);
  assert.match(card, /`bash` · rm -rf \.\/build · 已等 7 分钟/);
});

test("the audit tail is one short line per event", () => {
  const events: AuditEvent[] = [
    { at: NOW - 60_000, kind: "approve", tool: "bash", detail: "x".repeat(200) },
    { at: NOW - 30_000, kind: "deny", detail: "git push" },
  ];
  const card = formatKetchup(base({ events }));
  assert.match(card, /做过 2 条/);
  const lines = card.split("\n").filter((line) => line.startsWith("· ") && line.includes("approve"));
  assert.equal(lines.length, 1);
  assert.ok(lines[0]!.length < 120, "detail is clipped");
});

test("elapsed reads as a person would say it", () => {
  assert.equal(elapsed(10_000), "刚刚");
  assert.equal(elapsed(9 * 60_000), "9 分钟");
  assert.equal(elapsed(3 * 3_600_000), "3 小时");
  assert.equal(elapsed(50 * 3_600_000), "2 天");
});

test("the window keeps only recent events, newest last, capped", () => {
  const old: AuditEvent = { at: NOW - 20 * 3_600_000, kind: "access" };
  const fresh = Array.from({ length: 9 }, (_, index) => ({
    at: NOW - (9 - index) * 60_000,
    kind: "query" as const,
  }));
  const kept = withinWindow([old, ...fresh], NOW, 12, 6);
  assert.equal(kept.length, 6);
  assert.ok(kept.every((event) => event.at >= NOW - 12 * 3_600_000));
  assert.equal(kept[kept.length - 1]!.at, fresh[fresh.length - 1]!.at);
});
