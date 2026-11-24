import { log } from "./log.ts";
import { classify, glab, type GlabFailure } from "./glab.ts";

// Merge requests, through the glab the machine already has. The CLI call itself
// lives in glab.ts, because pipelines need the same one.

/** One merge request, as the client draws it. */
export type MergeRequest = {
  iid: number;
  title: string;
  state: string;
  draft: boolean;
  webUrl?: string;
  source: string;
  target: string;
  /** mergeable | conflicted | unchecked, reduced from GitLab's two fields. */
  merge: "mergeable" | "conflicted" | "unchecked";
  notes: number;
  author?: string;
  updated?: string;
};

type RawMr = {
  iid?: number;
  title?: string;
  state?: string;
  draft?: boolean;
  work_in_progress?: boolean;
  web_url?: string;
  source_branch?: string;
  target_branch?: string;
  has_conflicts?: boolean;
  detailed_merge_status?: string;
  merge_status?: string;
  user_notes_count?: number;
  author?: { name?: string; username?: string };
  updated_at?: string;
};

function mergeState(raw: RawMr): MergeRequest["merge"] {
  if (raw.has_conflicts === true) return "conflicted";
  const said = (raw.detailed_merge_status ?? raw.merge_status ?? "").toLowerCase();
  if (said === "mergeable" || said === "can_be_merged") return "mergeable";
  if (said === "conflict" || said === "cannot_be_merged") return "conflicted";
  return "unchecked";
}

/** The parser is exported because it is the part that breaks when GitLab moves. */
export function parseMerge(raw: RawMr): MergeRequest {
  return {
    iid: typeof raw.iid === "number" ? raw.iid : 0,
    title: typeof raw.title === "string" ? raw.title : "",
    state: typeof raw.state === "string" ? raw.state : "unknown",
    draft: raw.draft === true || raw.work_in_progress === true,
    webUrl: typeof raw.web_url === "string" ? raw.web_url : undefined,
    source: typeof raw.source_branch === "string" ? raw.source_branch : "",
    target: typeof raw.target_branch === "string" ? raw.target_branch : "",
    merge: mergeState(raw),
    notes: typeof raw.user_notes_count === "number" ? raw.user_notes_count : 0,
    author: raw.author?.name ?? raw.author?.username,
    updated: typeof raw.updated_at === "string" ? raw.updated_at : undefined,
  };
}

export type MrLookup =
  | { status: "ok"; mr?: MergeRequest }
  | { status: "no-remote" | "not-authenticated" | "no-glab" | "failed"; message?: string };

/**
 * The open merge request whose source is this branch, if there is one. An empty
 * list is a real answer — nothing has been opened yet — so it reads as ok with no
 * request rather than as an error.
 */
export async function findMergeRequest(projectPath: string, branch: string): Promise<MrLookup> {
  const query = `projects/:id/merge_requests?state=opened&per_page=20&source_branch=${encodeURIComponent(branch)}`;
  const result = await glab(projectPath, ["api", query]);
  const why = classify(result);
  if (why !== undefined) return { status: why, message: result.stderr.trim().split("\n")[0] };
  let rows: RawMr[];
  try {
    const parsed: unknown = JSON.parse(result.stdout);
    rows = Array.isArray(parsed) ? (parsed as RawMr[]) : [];
  } catch {
    return { status: "failed", message: "glab returned something that is not JSON" };
  }
  const mine = rows.filter((row) => row.source_branch === branch);
  if (mine.length === 0) return { status: "ok" };
  // The list endpoint reports merge_status "unchecked" because it does not look
  // at the target branch; only the single request says whether it would merge.
  const detail = await glab(projectPath, ["api", `projects/:id/merge_requests/${mine[0]!.iid}`]);
  if (detail.ok) {
    try {
      return { status: "ok", mr: parseMerge(JSON.parse(detail.stdout) as RawMr) };
    } catch {
      // Fall through to the list row: a stale copy beats no answer.
    }
  }
  return { status: "ok", mr: parseMerge(mine[0]!) };
}

export type MrWrite =
  | { status: "created"; mr?: MergeRequest; url?: string }
  | { status: "merged"; iid: number; message?: string }
  | { status: "no-remote" | "not-authenticated" | "no-glab" | "failed"; message?: string };

/**
 * Opens a merge request. The CLI is given every field as its own argv, and
 * `--yes` plus `--no-editor` because there is nobody at a terminal to answer.
 */
export async function createMergeRequest(
  projectPath: string,
  fields: { source: string; target: string; title: string; body?: string; draft?: boolean },
): Promise<MrWrite> {
  const args = [
    "mr",
    "create",
    "--source-branch",
    fields.source,
    "--target-branch",
    fields.target,
    "--title",
    fields.title,
    "--yes",
    "--no-editor",
  ];
  if (fields.body !== undefined && fields.body.length > 0) args.push("--description", fields.body);
  if (fields.draft === true) args.push("--draft");
  args.push("--print-url");
  const result = await glab(projectPath, args);
  const why = classify(result);
  if (why !== undefined) return { status: why, message: result.stderr.trim().split("\n").slice(-1)[0] ?? "" };
  const url = result.stdout.trim().split("\n").filter((line) => line.includes("/merge_requests/")).pop();
  log("mr", "created", { source: fields.source, target: fields.target, url });
  // The new request is read back rather than parsed out of the URL: the CLI's
  // output is a URL, the client wants the request.
  const back = await findMergeRequest(projectPath, fields.source);
  const mr = back.status === "ok" ? back.mr : undefined;
  return { status: "created", mr, url };
}

/** Merges one, optionally squashing or dropping the branch afterwards. */
export async function mergeMergeRequest(
  projectPath: string,
  iid: number,
  options: { squash?: boolean; removeSource?: boolean } = {},
): Promise<MrWrite> {
  if (!Number.isFinite(iid) || iid <= 0) return { status: "failed", message: "iid required" };
  const args = ["mr", "merge", String(iid), "--yes"];
  if (options.squash === true) args.push("--squash");
  if (options.removeSource === true) args.push("--remove-source-branch");
  const result = await glab(projectPath, args, 60_000);
  const why = classify(result);
  if (why !== undefined) return { status: why, message: result.stderr.trim().split("\n").slice(-1)[0] ?? "" };
  log("mr", "merged", { iid });
  return { status: "merged", iid, message: result.stdout.trim() };
}
