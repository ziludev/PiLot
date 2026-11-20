import { createBashTool } from "@earendil-works/pi-durable/tools";
import { defineTool, type ToolExecutionApi, type ToolExecutionResult, type ToolRegistration } from "@earendil-works/pi-durable";
import { requireCwd, toolboxError } from "./shared.ts";
import { noteSlice, parseSliceRead, widenedNote } from "./bash-read.ts";
import { abortedCall, type Toolbox, type ToolboxFailure } from "../client.ts";

/** The in-process tool: its schema, description, and result shape are the contract. */
const reference: ToolRegistration = createBashTool();

// Copied from pi-durable's bash tool: both paths must reject the same values.
const MAX_TIMEOUT_SECONDS = 2_147_483_647 / 1000;

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
  return toolboxError(failure);
}

/**
 * The `bash` tool served by the toolbox: the command runs in the Go process, its
 * output streams back as it arrives, and the result is assembled here so the
 * harness keeps the same tail it always kept.
 */
export function createBashToolProxy(toolbox: Toolbox) {
  return defineTool({
    name: reference.name,
    description: reference.description,
    parameters: reference.parameters,
    structuredOutputSchema: reference.structuredOutputSchema,
    outputLimits: reference.outputLimits,
    async execute(rawArgs, api, context): Promise<ToolExecutionResult> {
      const args = rawArgs as { command: string; timeout?: number };
      validateTimeout(args.timeout);
      // The reference's exec refuses an already-aborted call before it starts anything.
      if (context.abortSignal?.aborted) throw abortedCall();
      // A second slice of the same file is answering a question the first slice raised; the whole
      // file answers it and the next few. Falls through to the shell when the read itself fails.
      const slice = parseSliceRead(args.command);
      if (slice !== undefined) {
        const count = noteSlice(api.conversationId as unknown as number, slice.path);
        if (count > 1) {
          const read = await toolbox.call(
            "read",
            { path: slice.path, offset: slice.offset, limit: 2000 },
            { cwd: requireCwd(api), signal: context.abortSignal },
          );
          if (read.failure === undefined && read.result.isError !== true) {
            const text = String(read.result.text ?? "");
            api.diagnostic({
              severity: "info",
              code: "widened_read",
              message: `Read ${slice.path} whole instead of another slice`,
            });
            return {
              structuredOutput: {
                output: `${text}\n\n${widenedNote(slice.path, count)}`,
                truncated: false,
                exitCode: 0,
              },
            };
          }
        }
      }
      const call = await toolbox.call(
        "bash",
        { command: args.command, timeout: args.timeout },
        {
          cwd: requireCwd(api),
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
