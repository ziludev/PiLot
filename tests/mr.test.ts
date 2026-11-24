import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { createMergeRequest, findMergeRequest, mergeMergeRequest, parseMerge } from "../src/mr.ts";

// A glab that records its argv and answers from files, so the tests can see the
// exact argument vector the bridge builds without a GitLab account.
const STUB = `#!/bin/sh
if [ -n "$STUB_FAIL" ]; then
  printf '%s\\n' "$STUB_FAIL" >&2
  exit 1
fi
printf 'CALL\\0' >> "$STUB_ARGV"
for a in "$@"; do printf '%s\\0' "$a" >> "$STUB_ARGV"; done
case "$1 $2" in
  "api "*) case "$2" in
    */merge_requests/[0-9]*) cat "$STUB_API_DETAIL" ;;
    *) cat "$STUB_API" ;;
  esac ;;
  "mr create") cat "$STUB_CREATE" ;;
  "mr merge") cat "$STUB_MERGE" ;;
esac
`;

// argv returns one entry per glab invocation, each its argument vector: NUL
// separated, because a merge request body may hold newlines.
type Stub = {
  dir: string;
  calls: () => string[][];
  api: (json: string) => void;
  detail: (json: string) => void;
  fail: (text: string) => void;
};

function stubGlab(): Stub {
  const dir = mkdtempSync(join(tmpdir(), "pilot-glab-"));
  const bin = join(dir, "glab");
  writeFileSync(bin, STUB);
  chmodSync(bin, 0o755);
  const argvPath = join(dir, "argv");
  const apiPath = join(dir, "api.json");
  writeFileSync(apiPath, "[]");
  writeFileSync(argvPath, "");
  writeFileSync(join(dir, "create"), "https://gitlab.example.com/g/p/-/merge_requests/7\n");
  writeFileSync(join(dir, "merge"), "merged\n");
  process.env["PILOT_GLAB"] = bin;
  process.env["STUB_ARGV"] = argvPath;
  process.env["STUB_API"] = apiPath;
  const detailPath = join(dir, "detail.json");
  process.env["STUB_API_DETAIL"] = detailPath;
  process.env["STUB_CREATE"] = join(dir, "create");
  process.env["STUB_MERGE"] = join(dir, "merge");
  delete process.env["STUB_FAIL"];
  return {
    dir,
    calls: () => {
      const parts = readFileSync(argvPath, "utf8").split("\0");
      const calls: string[][] = [];
      for (const part of parts) {
        if (part === "CALL") calls.push([]);
        else if (part.length > 0 && calls.length > 0) calls[calls.length - 1]!.push(part);
      }
      return calls;
    },
    api: (json: string) => writeFileSync(apiPath, json),
    detail: (json: string) => writeFileSync(detailPath, json),
    fail: (text: string) => {
      process.env["STUB_FAIL"] = text;
    },
  };
}

const repo = () => mkdtempSync(join(tmpdir(), "pilot-repo-"));

const open = {
  iid: 7,
  title: "Add the branch list",
  state: "opened",
  draft: false,
  web_url: "https://gitlab.example.com/g/p/-/merge_requests/7",
  source_branch: "agent/conv-41",
  target_branch: "main",
  has_conflicts: false,
  detailed_merge_status: "mergeable",
  user_notes_count: 2,
  author: { name: "Wang" },
  updated_at: "2026-11-24T02:00:00Z",
};

test("parseMerge reduces GitLab's two status fields and both draft flags", () => {
  const once = parseMerge(open);
  assert.equal(once.merge, "mergeable");
  assert.equal(once.draft, false);
  assert.equal(once.notes, 2);
  assert.equal(once.author, "Wang");

  const wip = parseMerge({ ...open, draft: undefined, work_in_progress: true, has_conflicts: true });
  assert.equal(wip.draft, true, "work_in_progress is the older spelling of draft");
  assert.equal(wip.merge, "conflicted", "has_conflicts outranks the status string");

  const unknown = parseMerge({ iid: 1 });
  assert.equal(unknown.merge, "unchecked");
  assert.equal(unknown.state, "unknown");
  assert.equal(unknown.title, "");
});

test("findMergeRequest asks for the branch's open requests and picks that branch", async () => {
  const stub = stubGlab();
  const other = { ...open, iid: 9, source_branch: "someone/else" };
  stub.api(JSON.stringify([other, open]));
  stub.detail(JSON.stringify({ ...open, detailed_merge_status: "conflict", has_conflicts: true }));
  const found = await findMergeRequest(repo(), "agent/conv-41");
  assert.equal(found.status, "ok");
  assert.equal(found.status === "ok" ? found.mr?.iid : 0, 7);
  assert.equal(found.status === "ok" ? found.mr?.merge : "", "conflicted", "the single request is what says it would merge");
  const [argv] = stub.calls();
  assert.equal(argv?.[0], "api");
  assert.match(argv?.[1] ?? "", /^projects\/:id\/merge_requests\?state=opened/);
  assert.match(argv?.[1] ?? "", /source_branch=agent%2Fconv-41/, "the branch is URL-encoded, not shell-quoted");
});

test("findMergeRequest treats an empty list as no request, not as a failure", async () => {
  const stub = stubGlab();
  const found = await findMergeRequest(repo(), "agent/conv-41");
  assert.equal(found.status, "ok");
  assert.equal(found.status === "ok" ? found.mr : "missing", undefined);
  void stub;
});

test("findMergeRequest reports a missing login instead of pretending there is no MR", async () => {
  const stub = stubGlab();
  stub.fail("No token found (checked config file, keyring, and environment variables).");
  const found = await findMergeRequest(repo(), "agent/conv-41");
  assert.equal(found.status, "not-authenticated");
});

test("findMergeRequest survives output that is not JSON", async () => {
  const stub = stubGlab();
  stub.api("panic: nil map\n");
  const found = await findMergeRequest(repo(), "agent/conv-41");
  assert.equal(found.status, "failed");
  assert.match(found.status === "failed" ? (found.message ?? "") : "", /not JSON/);
});

test("createMergeRequest passes every field as its own argument", async () => {
  const stub = stubGlab();
  stub.api(JSON.stringify([open]));
  stub.detail(JSON.stringify({ ...open, detailed_merge_status: "mergeable" }));
  const made = await createMergeRequest(repo(), {
    source: "agent/conv-41",
    target: "main",
    title: "Add the branch list",
    body: "line one\nline two with a $dollar and a `tick`",
    draft: true,
  });
  assert.equal(made.status, "created");
  assert.equal(made.status === "created" ? made.url : "", "https://gitlab.example.com/g/p/-/merge_requests/7");
  assert.equal(made.status === "created" ? made.mr?.iid : 0, 7, "the request is read back, not guessed from the URL");
  const [createArgv] = stub.calls();
  assert.deepEqual(createArgv, [
    "mr",
    "create",
    "--source-branch",
    "agent/conv-41",
    "--target-branch",
    "main",
    "--title",
    "Add the branch list",
    "--yes",
    "--no-editor",
    "--description",
    "line one\nline two with a $dollar and a `tick`",
    "--draft",
    "--print-url",
  ]);
});

test("mergeMergeRequest squashes and drops the branch when asked", async () => {
  const stub = stubGlab();
  const merged = await mergeMergeRequest(repo(), 7, { squash: true, removeSource: true });
  assert.equal(merged.status, "merged");
  const [argv] = stub.calls();
  assert.deepEqual(argv, ["mr", "merge", "7", "--yes", "--squash", "--remove-source-branch"]);
});

test("mergeMergeRequest refuses an id that is not one", async () => {
  stubGlab();
  const merged = await mergeMergeRequest(repo(), 0);
  assert.equal(merged.status, "failed");
});
