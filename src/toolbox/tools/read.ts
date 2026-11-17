import { DEFAULT_IMAGE_LIMITS, createReadTool } from "@earendil-works/pi-durable/tools";
import { defineTool, type ToolExecutionApi, type ToolExecutionResult, type ToolRegistration } from "@earendil-works/pi-durable";
import { requireCwd, toolboxError } from "./shared.ts";
import type { Toolbox } from "../client.ts";

/** The in-process tool: its schema, description, and image limits are the contract. */
const reference: ToolRegistration = createReadTool();

type ImageFacts = {
  vision?: boolean;
  maxImageBytes?: number;
  maxImageWidth?: number;
  maxImageHeight?: number;
};

/**
 * What the model can see of an image: pi-durable asks the model registry for it, so the toolbox
 * receives it as an argument instead of guessing.
 */
async function imageFacts(api: ToolExecutionApi, context: { abortSignal?: AbortSignal }): Promise<ImageFacts> {
  const toolApi = api as ToolExecutionApi & {
    agent?: (context: unknown) => Promise<{ model?: { provider: string; modelId: string } }>;
    models?: { getModel: (provider: string, id: string) => { input: string[]; inputLimits?: { images?: { resize?: Partial<typeof DEFAULT_IMAGE_LIMITS> } } } | undefined };
  };
  if (toolApi.agent === undefined || toolApi.models === undefined) return {};
  const ref = (await toolApi.agent(context)).model;
  const model = ref === undefined ? undefined : toolApi.models.getModel(ref.provider, ref.modelId);
  const resize = model?.inputLimits?.images?.resize;
  return {
    vision: model?.input.includes("image") ?? true,
    maxImageWidth: resize?.maxWidth ?? DEFAULT_IMAGE_LIMITS.maxWidth,
    maxImageHeight: resize?.maxHeight ?? DEFAULT_IMAGE_LIMITS.maxHeight,
    maxImageBytes: resize?.maxBytes ?? DEFAULT_IMAGE_LIMITS.maxBytes,
  };
}

/**
 * The `read` tool served by the toolbox: the file is opened and bounded in the Go process, and the
 * result is assembled here in the shape the harness and the model already expect.
 */
export function createReadToolProxy(toolbox: Toolbox) {
  return defineTool({
    name: reference.name,
    description: reference.description,
    parameters: reference.parameters,
    structuredOutputSchema: reference.structuredOutputSchema,
    async execute(rawArgs, api, context): Promise<ToolExecutionResult> {
      const args = rawArgs as { path: string; offset?: number; limit?: number };
      const cwd = requireCwd(api);
      const facts = await imageFacts(api, context);
      const call = await toolbox.call(
        "read",
        { path: args.path, offset: args.offset, limit: args.limit, ...facts },
        { cwd, signal: context.abortSignal },
      );
      if (call.failure !== undefined) throw toolboxError(call.failure);

      const diagnostics = (call.result.diagnostics as ToolExecutionResult["diagnostics"]) ?? [];
      if (call.result.isError === true) return { output: [], isError: true, diagnostics };
      if (call.result.image !== undefined) {
        const image = call.result.image as { type: "image"; data: string; mimeType: string };
        return { output: [image], structuredOutput: image, diagnostics };
      }
      const text = String(call.result.text ?? "");
      const details = call.result.details as unknown as ToolExecutionResult["details"];
      return {
        output: text === "" ? [] : [{ type: "text", text }],
        structuredOutput: text,
        ...(details === undefined ? {} : { details }),
        ...(diagnostics.length === 0 ? {} : { diagnostics }),
      };
    },
  });
}
