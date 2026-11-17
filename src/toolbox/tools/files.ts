import { createEditTool, createWriteTool } from "@earendil-works/pi-durable/tools";
import { defineTool, type ToolExecutionResult, type ToolRegistration } from "@earendil-works/pi-durable";
import type { Toolbox, ToolboxFailure } from "../client.ts";

const writeReference: ToolRegistration = createWriteTool();
const editReference: ToolRegistration = createEditTool();

function failureOf(failure: ToolboxFailure): Error {
  return Object.assign(new Error(failure.message), failure);
}

/** The `write` tool served by the toolbox: parent directories and the write happen in Go. */
export function createWriteToolProxy(toolbox: Toolbox) {
  return defineTool({
    name: writeReference.name,
    description: writeReference.description,
    parameters: writeReference.parameters,
    async execute(rawArgs, api, context): Promise<ToolExecutionResult> {
      const args = rawArgs as { path: string; content: string };
      const env = api.env;
      if (env === undefined) throw new Error("No execution environment is configured");
      const call = await toolbox.call("write", { path: args.path, content: args.content }, { cwd: env.cwd, signal: context.abortSignal });
      if (call.failure !== undefined) throw failureOf(call.failure);
      return { output: [{ type: "text", text: String(call.result.text ?? "") }] };
    },
  });
}

/** The `edit` tool served by the toolbox: matching, line endings, and the diff come back from Go. */
export function createEditToolProxy(toolbox: Toolbox) {
  return defineTool({
    name: editReference.name,
    description: editReference.description,
    parameters: editReference.parameters,
    prepareArguments: editReference.prepareArguments,
    async execute(rawArgs, api, context): Promise<ToolExecutionResult> {
      const args = rawArgs as { path: string; edits: Array<{ oldText: string; newText: string }> };
      const env = api.env;
      if (env === undefined) throw new Error("No execution environment is configured");
      const call = await toolbox.call("edit", { path: args.path, edits: args.edits }, { cwd: env.cwd, signal: context.abortSignal });
      if (call.failure !== undefined) throw failureOf(call.failure);
      const details: Record<string, unknown> = {
        diff: call.result.diff,
        patch: call.result.patch,
      };
      if (call.result.firstChangedLine !== undefined) details.firstChangedLine = call.result.firstChangedLine;
      return { output: [{ type: "text", text: String(call.result.text ?? "") }], details: details as unknown as ToolExecutionResult["details"] };
    },
  });
}
