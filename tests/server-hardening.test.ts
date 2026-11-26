import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { approvalReason } from "../src/approval.ts";
import { allowedCorsOrigin, resolveWorkspaceFile } from "../src/server.ts";

test("allowedCorsOrigin accepts loopback / WebView null origins and rejects external sites", () => {
  assert.equal(allowedCorsOrigin("http://localhost:3000"), "http://localhost:3000");
  assert.equal(allowedCorsOrigin("http://127.0.0.1:8787"), "http://127.0.0.1:8787");
  assert.equal(allowedCorsOrigin("http://[::1]:5173"), "http://[::1]:5173");
  assert.equal(allowedCorsOrigin("null"), "null");
  assert.equal(allowedCorsOrigin("https://evil.example"), undefined);
  assert.equal(allowedCorsOrigin(undefined), undefined);
});

test("resolveWorkspaceFile allows normal files and blocks traversal or symlink escapes", async () => {
  const dir = mkdtempSync(join(tmpdir(), "pilot-srv-"));
  const root = join(dir, "repo");
  const outside = join(dir, "secret.txt");
  mkdirSync(join(root, "sub"), { recursive: true });
  writeFileSync(join(root, "sub/ok.txt"), "hello");
  writeFileSync(outside, "TOP_SECRET");
  symlinkSync(outside, join(root, "leak.txt"));

  try {
    const ok = await resolveWorkspaceFile(root, "sub/ok.txt");
    assert.equal(ok.ok, true);

    const traversal = await resolveWorkspaceFile(root, "../secret.txt");
    assert.deepEqual(traversal, { ok: false, status: 400, error: "path outside workspace" });

    const escaped = await resolveWorkspaceFile(root, "leak.txt");
    assert.deepEqual(escaped, { ok: false, status: 400, error: "path outside workspace" });
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("approvalReason allows writes inside bound project roots and asks outside all roots", () => {
  const roots = ["/tmp/workspace", "/tmp/project-repo"];
  assert.equal(approvalReason("write", { path: "/tmp/project-repo/.worktrees/conv-1/a.ts" }, roots), undefined);
  assert.equal(approvalReason("edit", { path: "/etc/hosts" }, roots)?.key, "write-outside");
});
