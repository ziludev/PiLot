import { bool, num, obj, opt, str } from "@earendil-works/pi-schema";
import { defineTool, type ToolExecutionApi, type ToolExecutionResult } from "@earendil-works/pi-durable";
import { DEFAULT_MAX_BYTES, DEFAULT_MAX_LINES } from "../limits.ts";
import type { Toolbox, ToolboxFailure } from "../client.ts";

// Copied from pi-durable's bash tool: both paths must reject the same values.
const MAX_TIMEOUT_SECONDS = 2_147_483_647 / 1000;

const bashSchema = obj(
  {
    command: str({ description: "Bash command to execute" }),
    timeout: opt(num({ description: "Timeout in seconds (optional, no default timeout)" })),
  },
  { open: true },
);

/** What a program that calls `bash` receives; unchanged from the in-process tool. */
export const shellStructuredOutputSchema = obj(
  {
    output: str({ description: "Combined stdout and stderr: the retained tail, as the model sees it" }),
    truncated: bool({ description: "Whether earlier output was dropped" }),
    fullOutputPath: opt(str({ description: "File with the complete output, when it was spilled" })),
    exitCode: num(),
  },
  { open: true },
);

function validateTimeout(timeout: number | undefined): void {
  if (timeout === undefined) return;
  if (!Number.isFinite(timeout) || timeout <= 0) {
    throw new Error("Invalid timeout: must be a finite number of seconds");
  }
  if (timeout > MAX_TIMEOUT_SECONDS) {
    throw new Error(`Invalid timeout: maximum is ${MAX_TIMEOUT_SECONDS} seconds`);
  }
}

/** The same diagnostics and errors the in-process tool produces, from a toolbox failure. */
function failureOf(failure: ToolboxFailure, timeout: number | undefined, api: ToolExecutionApi, context: {
  abortSignal?: AbortSignal;
}): Error {
  if (failure.spillPath !== undefined) {
    api.diagnostic({ severity: "info", code: "full_output", message: `Full output: ${failure.spillPath}` });
  }
  if (failure.code === "aborted" && context.abortSignal?.aborted) return Object.assign(new Error("aborted"), failure);
  if (failure.code === "timeout") return new Error(`Command timed out after ${timeout} seconds`);
  if (failure.code === "aborted") return new Error("Command aborted");
  return Object.assign(new Error(failure.message), failure);
}

/**
 * The `bash` tool served by the toolbox: the command runs in the Go process, its
 * output streams back as it arrives, and the result is assembled here so the
 * harness keeps the same tail it always kept.
 */
export function createBashTool(toolbox: Toolbox) {
  return defineTool({
    name: "bash",
    description: `Execute a bash command in the current working directory. Returns combined stdout and stderr. Output is truncated to last ${DEFAULT_MAX_LINES} lines or ${DEFAULT_MAX_BYTES / 1024}KB (whichever is hit first). If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds.`,
    parameters: bashSchema,
    structuredOutputSchema: shellStructuredOutputSchema,
    outputLimits: { retain: "tail" },
    async execute(args, api, context): Promise<ToolExecutionResult> {
      validateTimeout(args.timeout);
      const env = api.env;
      if (env === undefined) throw new Error("No execution environment is configured");
      const call = await toolbox.call(
        "bash",
        { command: args.command, timeout: args.timeout },
        {
          cwd: env.cwd,
          signal: context.abortSignal,
          onChunk: (text) => api.output(text),
        },
      );
      if (call.failure !== undefined) throw failureOf(call.failure, args.timeout, api, context);

      const fullOutputPath = call.result.fullOutputPath as string | undefined;
      if (fullOutputPath !== undefined) {
        api.diagnostic({ severity: "info", code: "full_output", message: `Full output: ${fullOutputPath}` });
      }
      const exitCode = call.result.exitCode as number;
      const retained = api.retainedOutput();
      const structuredOutput = {
        output: retained.text,
        truncated: retained.truncated,
        ...(fullOutputPath === undefined ? {} : { fullOutputPath }),
        exitCode,
      };
      if (exitCode === 0) return { structuredOutput };
      const exited = { severity: "error" as const, code: "exit_code", message: `Command exited with code ${exitCode}` };
      return { structuredOutput, isError: true, diagnostics: [exited] };
    },
  });
}
