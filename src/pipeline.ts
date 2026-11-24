import { classify, glab, type GlabFailure } from "./glab.ts";
import { remoteInfo } from "./git-info.ts";
import { log } from "./log.ts";

// CI pipelines for the branch a conversation is on, through the same glab the
// merge requests use. Reading a pipeline is three API calls (the branch's latest,
// its jobs, and one job's trace when asked); writing is a retry or a cancel,
// which GitLab takes as a POST on the job or the pipeline itself.

/** One job, as the client draws it. */
export type PipelineJob = {
  id: number;
  name: string;
  stage: string;
  status: string;
  webUrl?: string;
  /** Seconds. Absent while the job has not finished. */
  duration?: number;
  allowFailure: boolean;
};

/** One pipeline, reduced to what a drawer shows. */
export type PipelineInfo = {
  id: number;
  status: string;
  ref: string;
  sha: string;
  webUrl?: string;
  created?: string;
  updated?: string;
  source?: string;
};

export type PipelineLookup = {
  status: "ok" | GlabFailure | "not-gitlab" | "detached";
  message?: string;
  project?: string;
  remote?: { url: string; webUrl?: string };
  branch?: string;
  /** The branch's latest pipeline, or null when the branch never ran one. */
  pipeline?: PipelineInfo | null;
  jobs?: PipelineJob[];
  /** Stage names in the order the jobs first mention them. */
  stages?: string[];
};

type RawPipeline = {
  id?: number;
  status?: string;
  ref?: string;
  sha?: string;
  web_url?: string;
  created_at?: string;
  updated_at?: string;
  source?: string;
};

type RawJob = {
  id?: number;
  name?: string;
  stage?: string;
  status?: string;
  web_url?: string;
  duration?: number | null;
  allow_failure?: boolean;
};

/** reduce keeps the fields the drawer needs and drops GitLab's long tail. */
function reducePipeline(raw: RawPipeline): PipelineInfo | undefined {
  if (typeof raw.id !== "number") return undefined;
  return {
    id: raw.id,
    status: raw.status ?? "unknown",
    ref: raw.ref ?? "",
    sha: raw.sha ?? "",
    webUrl: raw.web_url,
    created: raw.created_at,
    updated: raw.updated_at,
    source: raw.source,
  };
}

function reduceJob(raw: RawJob): PipelineJob | undefined {
  if (typeof raw.id !== "number") return undefined;
  return {
    id: raw.id,
    name: raw.name ?? "",
    stage: raw.stage ?? "",
    status: raw.status ?? "unknown",
    webUrl: raw.web_url,
    duration: typeof raw.duration === "number" ? raw.duration : undefined,
    allowFailure: raw.allow_failure === true,
  };
}

/** stageOrder lists stages as the jobs mention them, which is GitLab's own order. */
export function stageOrder(jobs: PipelineJob[]): string[] {
  const seen: string[] = [];
  for (const job of jobs) {
    if (job.stage.length > 0 && !seen.includes(job.stage)) seen.push(job.stage);
  }
  return seen;
}

function parseJson<T>(text: string): T | undefined {
  try {
    return JSON.parse(text) as T;
  } catch {
    return undefined;
  }
}

/** latestPipeline is the branch's most recent run, or null when it has none. */
export async function latestPipeline(projectPath: string, branch: string): Promise<PipelineLookup> {
  const remote = await remoteInfo(projectPath);
  if (remote === undefined || remote.kind !== "gitlab") {
    return { status: "not-gitlab", project: projectPath, branch };
  }
  const query = `projects/:id/pipelines?ref=${encodeURIComponent(branch)}&per_page=1`;
  const result = await glab(projectPath, ["api", query]);
  const why = classify(result);
  if (why !== undefined) {
    log(`pipeline: glab said ${why}`);
    return { status: why, project: projectPath, branch, message: result.stderr.trim().slice(0, 400) };
  }
  const rows = parseJson<RawPipeline[]>(result.stdout);
  if (rows === undefined) {
    return { status: "failed", project: projectPath, branch, message: "glab did not answer with JSON" };
  }
  const pipeline = rows.length > 0 ? reducePipeline(rows[0]!) : undefined;
  return { status: "ok", project: projectPath, remote: { url: remote.url ?? "", webUrl: remote.webUrl ?? "" }, branch, pipeline: pipeline ?? null, jobs: [], stages: [] };
}

/** jobsOf reads every job of one pipeline and the stage order they imply. */
export async function jobsOf(projectPath: string, pipelineId: number): Promise<{ status: "ok" | GlabFailure; message?: string; jobs: PipelineJob[]; stages: string[] }> {
  if (!Number.isInteger(pipelineId) || pipelineId <= 0) {
    return { status: "failed", message: "pipelineId required", jobs: [], stages: [] };
  }
  const result = await glab(projectPath, ["api", `projects/:id/pipelines/${pipelineId}/jobs?per_page=100`]);
  const why = classify(result);
  if (why !== undefined) return { status: why, message: result.stderr.trim().slice(0, 400), jobs: [], stages: [] };
  const rows = parseJson<RawJob[]>(result.stdout);
  if (rows === undefined) return { status: "failed", message: "glab did not answer with JSON", jobs: [], stages: [] };
  const jobs = rows.map(reduceJob).filter((job): job is PipelineJob => job !== undefined);
  return { status: "ok", jobs, stages: stageOrder(jobs) };
}

/** pipelineWithJobs is what the client asks for: the latest run and its jobs. */
export async function pipelineWithJobs(projectPath: string, branch: string): Promise<PipelineLookup> {
  const lookup = await latestPipeline(projectPath, branch);
  if (lookup.status !== "ok" || lookup.pipeline === undefined || lookup.pipeline === null) return lookup;
  const jobs = await jobsOf(projectPath, lookup.pipeline.id);
  if (jobs.status !== "ok") {
    return { ...lookup, status: jobs.status, message: jobs.message };
  }
  return { ...lookup, jobs: jobs.jobs, stages: jobs.stages };
}

/** jobTrace is the last lines of a job's log: the part that says why it failed. */
export async function jobTrace(projectPath: string, jobId: number, tail = 80): Promise<{ status: "ok" | GlabFailure; message?: string; text: string; cut: boolean }> {
  if (!Number.isInteger(jobId) || jobId <= 0) {
    return { status: "failed", message: "jobId required", text: "", cut: false };
  }
  const result = await glab(projectPath, ["api", `projects/:id/jobs/${jobId}/trace`], 60_000);
  const why = classify(result);
  if (why !== undefined) return { status: why, message: result.stderr.trim().slice(0, 400), text: "", cut: false };
  // The trace comes back as plain text, so an error page and a log look alike;
  // one that does not start like a log is reported instead of drawn.
  const text = result.stdout.replace(/\r\n/g, "\n");
  const lines = text.split("\n");
  const kept = tail > 0 && lines.length > tail ? lines.slice(lines.length - tail) : lines;
  return { status: "ok", text: kept.join("\n"), cut: kept.length !== lines.length };
}

/** retryWork is the write half: a job retry, a job cancel, or the pipeline's. */
export async function retryWork(projectPath: string, what: { jobId?: number; pipelineId?: number; action: "retry" | "cancel" }): Promise<{ status: "ok" | GlabFailure; message?: string }> {
  const target =
    what.jobId !== undefined && what.jobId > 0
      ? `projects/:id/jobs/${what.jobId}`
      : what.pipelineId !== undefined && what.pipelineId > 0
        ? `projects/:id/pipelines/${what.pipelineId}`
        : undefined;
  if (target === undefined) return { status: "failed", message: "jobId or pipelineId required" };
  const result = await glab(projectPath, ["api", "--method", "POST", `${target}/${what.action}`], 60_000);
  const why = classify(result);
  if (why !== undefined) {
    log(`pipeline: ${what.action} said ${why}`);
    return { status: why, message: result.stderr.trim().slice(0, 400) };
  }
  return { status: "ok" };
}
