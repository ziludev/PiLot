import { execFile } from "node:child_process";
import { promisify } from "node:util";

// The glab the machine already has, shared by everything that asks GitLab
// something. Nothing here talks to the GitLab API directly: the CLI owns the
// token, the host and the project id, so the bridge never holds a credential.
// Every call passes argv without a shell, so a branch name or a job title can
// hold anything.

const run = promisify(execFile);

/** The binary. Tests point it at a stub; production uses the one on PATH. */
export function glabBin(): string {
  const override = process.env["PILOT_GLAB"];
  return override !== undefined && override.length > 0 ? override : "glab";
}

export type GlabRun = { ok: boolean; stdout: string; stderr: string };

export async function glab(projectPath: string, args: string[], timeout = 30_000): Promise<GlabRun> {
  try {
    const { stdout, stderr } = await run(glabBin(), args, {
      cwd: projectPath,
      timeout,
      maxBuffer: 8 * 1024 * 1024,
      env: { ...process.env, NO_COLOR: "1", GIT_TERMINAL_PROMPT: "0" },
    });
    return { ok: true, stdout, stderr };
  } catch (error) {
    const failed = error as { stdout?: string; stderr?: string; code?: number };
    return { ok: false, stdout: failed.stdout ?? "", stderr: failed.stderr ?? String(error) };
  }
}

/** Why a call failed, in words a person can act on. */
export type GlabFailure = "no-remote" | "not-authenticated" | "no-glab" | "failed";

export function classify(result: GlabRun): GlabFailure | undefined {
  if (result.ok) return undefined;
  const said = `${result.stdout}\n${result.stderr}`.toLowerCase();
  if (said.includes("no token") || said.includes("not logged in") || said.includes("authentication")) {
    return "not-authenticated";
  }
  if (said.includes("enoent") || said.includes("command not found")) return "no-glab";
  if (said.includes("no remote") || said.includes("could not determine")) return "no-remote";
  if (said.includes("none of the git remotes")) return "no-remote";
  if (said.includes("not a git repository")) return "no-remote";
  return "failed";
}
