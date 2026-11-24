import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { jobTrace, jobsOf, latestPipeline, pipelineWithJobs, retryWork, stageOrder } from "../src/pipeline.ts";

// A glab that records argv and answers from files: pipelines are three API calls
// on paths that differ only by suffix, so the stub switches on the path itself.
const STUB = `#!/bin/sh
if [ -n "$STUB_FAIL" ]; then
  printf '%s\\n' "$STUB_FAIL" >&2
  exit 1
fi
printf 'CALL\\0' >> "$STUB_ARGV"
for a in "$@"; do printf '%s\\0' "$a" >> "$STUB_ARGV"; done
path=""
for a in "$@"; do path="$a"; done
case "$path" in
  *"/pipelines?"*) cat "$STUB_PIPELINES" ;;
  *"/pipelines/"*"/jobs?per_page="*) cat "$STUB_JOBS" ;;
  *"/jobs/"*"/trace") cat "$STUB_TRACE" ;;
  *) printf '{"ok":true}\\n' ;;
esac
`;

type Stub = {
  dir: string;
  calls: () => string[][];
  pipelines: (json: string) => void;
  jobs: (json: string) => void;
  trace: (text: string) => void;
  fail: (text: string) => void;
};

function stubGlab(): Stub {
  const dir = mkdtempSync(join(tmpdir(), "pilot-ci-"));
  const bin = join(dir, "glab");
  writeFileSync(bin, STUB);
  chmodSync(bin, 0o755);
  const argvPath = join(dir, "argv");
  writeFileSync(argvPath, "");
  const pipelines = join(dir, "pipelines.json");
  const jobs = join(dir, "jobs.json");
  const trace = join(dir, "trace.txt");
  writeFileSync(pipelines, "[]");
  writeFileSync(jobs, "[]");
  writeFileSync(trace, "");
  process.env["PILOT_GLAB"] = bin;
  process.env["STUB_ARGV"] = argvPath;
  process.env["STUB_PIPELINES"] = pipelines;
  process.env["STUB_JOBS"] = jobs;
  process.env["STUB_TRACE"] = trace;
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
    pipelines: (json: string) => writeFileSync(pipelines, json),
    jobs: (json: string) => writeFileSync(jobs, json),
    trace: (text: string) => writeFileSync(trace, text),
    fail: (text: string) => {
      process.env["STUB_FAIL"] = text;
    },
  };
}

/** repo is a real git repository with a GitLab remote, which the pipeline read requires. */
function repo(): string {
  const dir = mkdtempSync(join(tmpdir(), "pilot-repo-"));
  execFileSync("git", ["init", "-q", "-b", "main"], { cwd: dir });
  execFileSync("git", ["remote", "add", "origin", "git@gitlab.example.com:g/p.git"], { cwd: dir });
  return dir;
}

const one = {
  id: 900,
  status: "failed",
  ref: "agent/conv-7",
  sha: "abc123",
  web_url: "https://gitlab.example.com/g/p/-/pipelines/900",
  created_at: "2026-11-24T02:00:00Z",
  updated_at: "2026-11-24T02:03:00Z",
  source: "push",
};

const jobRows = [
  { id: 70, name: "build", stage: "build", status: "success", duration: 31.4, allow_failure: false },
  { id: 71, name: "test:unit", stage: "test", status: "failed", duration: 12, allow_failure: false },
  { id: 72, name: "lint", stage: "test", status: "skipped", allow_failure: true, web_url: "https://x/72" },
];

test("a branch with no pipeline is an answer, not a failure", async () => {
  const stub = stubGlab();
  stub.pipelines("[]");
  const lookup = await latestPipeline(repo(), "agent/conv-7");
  assert.equal(lookup.status, "ok");
  assert.equal(lookup.pipeline, null);
  assert.equal(lookup.branch, "agent/conv-7");
  const asked = stub.calls()[0]!;
  assert.equal(asked[0], "api");
  assert.match(asked[1]!, /^projects\/:id\/pipelines\?ref=agent%2Fconv-7/);
});

test("the latest run is reduced to the fields the drawer draws", async () => {
  const stub = stubGlab();
  stub.pipelines(JSON.stringify([one]));
  const lookup = await latestPipeline(repo(), "agent/conv-7");
  assert.equal(lookup.status, "ok");
  assert.equal(lookup.pipeline?.id, 900);
  assert.equal(lookup.pipeline?.status, "failed");
  assert.equal(lookup.pipeline?.webUrl, "https://gitlab.example.com/g/p/-/pipelines/900");
  assert.equal(lookup.pipeline?.source, "push");
});

test("jobs keep their stage order and their allowance to fail", async () => {
  const stub = stubGlab();
  stub.pipelines(JSON.stringify([one]));
  stub.jobs(JSON.stringify(jobRows));
  const lookup = await pipelineWithJobs(repo(), "agent/conv-7");
  assert.equal(lookup.status, "ok");
  assert.deepEqual(lookup.stages, ["build", "test"]);
  assert.equal(lookup.jobs?.length, 3);
  assert.equal(lookup.jobs?.[1]?.status, "failed");
  assert.equal(lookup.jobs?.[2]?.allowFailure, true);
  assert.equal(lookup.jobs?.[2]?.duration, undefined);
  assert.equal(stub.calls()[1]![1], "projects/:id/pipelines/900/jobs?per_page=100");
});

test("stageOrder lists a stage once, first seen", () => {
  assert.deepEqual(stageOrder([]), []);
  assert.deepEqual(
    stageOrder([
      { id: 1, name: "a", stage: "build", status: "success", allowFailure: false },
      { id: 2, name: "b", stage: "test", status: "success", allowFailure: false },
      { id: 3, name: "c", stage: "build", status: "success", allowFailure: false },
    ]),
    ["build", "test"],
  );
});

test("a trace is cut to its tail and says so", async () => {
  const stub = stubGlab();
  const lines = Array.from({ length: 120 }, (_, i) => `line ${i + 1}`);
  stub.trace(`${lines.join("\n")}\n`);
  const trace = await jobTrace(repo(), 70, 5);
  assert.equal(trace.status, "ok");
  assert.equal(trace.cut, true);
  assert.equal(trace.text.split("\n").length, 5);
  assert.match(trace.text, /line 120/);
  assert.equal(stub.calls()[0]![1], "projects/:id/jobs/70/trace");
});

test("a short trace is kept whole and not called cut", async () => {
  const stub = stubGlab();
  stub.trace("one\ntwo\n");
  const trace = await jobTrace(repo(), 70, 80);
  assert.equal(trace.cut, false);
  assert.equal(trace.text, "one\ntwo\n");
});

test("a retry posts to the job, a cancel posts to the pipeline", async () => {
  const stub = stubGlab();
  assert.deepEqual(await retryWork(repo(), { jobId: 71, action: "retry" }), { status: "ok" });
  assert.deepEqual(await retryWork(repo(), { pipelineId: 900, action: "cancel" }), { status: "ok" });
  const calls = stub.calls();
  assert.deepEqual(calls[0], ["api", "--method", "POST", "projects/:id/jobs/71/retry"]);
  assert.deepEqual(calls[1], ["api", "--method", "POST", "projects/:id/pipelines/900/cancel"]);
});

test("a write with neither id says so instead of guessing", async () => {
  const stub = stubGlab();
  const said = await retryWork(repo(), { action: "retry" });
  assert.equal(said.status, "failed");
  assert.match(said.message ?? "", /jobId or pipelineId/);
  assert.equal(stub.calls().length, 0);
});

test("an unread pipeline id and an unlogged glab both report themselves", async () => {
  const stub = stubGlab();
  assert.equal((await jobsOf(repo(), 0)).status, "failed");
  stub.fail("glab: no token found for gitlab.example.com; run glab auth login");
  const said = await latestPipeline(repo(), "agent/conv-7");
  assert.equal(said.status, "not-authenticated");
  assert.match(said.message ?? "", /no token/);
});

test("a directory that is not a GitLab checkout is not asked about pipelines", async () => {
  const stub = stubGlab();
  const said = await latestPipeline(mkdtempSync(join(tmpdir(), "pilot-plain-")), "main");
  assert.equal(said.status, "not-gitlab");
  assert.equal(stub.calls().length, 0);
});
