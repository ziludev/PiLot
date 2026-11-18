import assert from "node:assert/strict";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import {
  bootNews,
  clearBoot,
  formatCrashNotice,
  formatRestartNotice,
  markBoot,
  readBootMark,
  takeRestartNote,
} from "../src/restart.ts";

const now = 1_800_000_000_000;

test("a queued restart says why and how long it waited", () => {
  const text = formatRestartNotice(
    { kind: "planned", reason: "我排队重启，等手上的回合跑完再加载新代码", queuedAt: now - 11 * 60_000, at: now },
    now,
    4321,
  );
  assert.match(text, /已重启/);
  assert.match(text, /等手上的回合跑完/);
  assert.match(text, /等了 11 分钟/);
  assert.match(text, /pid 4321/);
});

test("a restart that landed at once does not claim it waited", () => {
  const text = formatRestartNotice({ kind: "manual", at: now, queuedAt: now - 2_000 }, now, 7);
  assert.match(text, /手动重启/);
  assert.ok(!text.includes("等了"), text);
});

test("a note without a reason falls back to what the switch was asked to do", () => {
  const text = formatRestartNotice({ kind: "queued", at: now }, now, 7);
  assert.match(text, /加载新代码/);
});

test("a crash is reported with how long the dead process lasted", () => {
  const text = formatCrashNotice({ pid: 99, startedAt: now - 3 * 60_000 }, now, 100);
  assert.match(text, /没正常退出/);
  assert.match(text, /pid 99/);
  assert.match(text, /3 分钟/);
});

test("a note is read once and consumed", () => {
  const dir = mkdtempSync(join(tmpdir(), "restart-"));
  try {
    writeFileSync(join(dir, "restart-pending.json"), JSON.stringify({ kind: "queued", reason: "r", at: now }));
    assert.equal(takeRestartNote(dir)?.reason, "r");
    assert.equal(takeRestartNote(dir), undefined);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a planned restart is not also reported as a crash", () => {
  const dir = mkdtempSync(join(tmpdir(), "restart-"));
  try {
    // The previous process was cut before it could clear its marker, which alone reads as a crash.
    markBoot(dir, 11, now - 60_000);
    writeFileSync(join(dir, "restart-pending.json"), JSON.stringify({ kind: "planned", at: now }));
    const news = bootNews(dir, now, 12);
    assert.match(news ?? "", /已重启/);
    assert.ok(!(news ?? "").includes("没正常退出"), news);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a stale boot marker means the last process died without cleaning up", () => {
  const dir = mkdtempSync(join(tmpdir(), "restart-"));
  try {
    markBoot(dir, 11, now - 120_000);
    assert.match(bootNews(dir, now, 12) ?? "", /没正常退出/);
    // Reading the news does not stop the next boot from reporting the same death.
    markBoot(dir, 12, now);
    clearBoot(dir);
    assert.equal(readBootMark(dir), undefined);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a first run has no news to send", () => {
  const dir = mkdtempSync(join(tmpdir(), "restart-"));
  try {
    assert.equal(bootNews(dir, now, 1), undefined);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
