import type { ToolExecutionApi } from "@earendil-works/pi-durable";
import type { ToolboxFailure } from "../client.ts";

/**
 * The directory a tool runs in. Every tool in the box needs one, and the answer
 * (or the refusal) must be the same as in-process, so it lives in one place.
 */
export function requireCwd(api: ToolExecutionApi): string {
  const env = api.env;
  if (env === undefined) throw new Error("No execution environment is configured");
  return env.cwd;
}

/** A refusal from the toolbox, as the error the in-process tool would have thrown. */
export function toolboxError(failure: ToolboxFailure): Error {
  return Object.assign(new Error(failure.message), failure);
}
