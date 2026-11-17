import { config } from "../config.ts";
import { log } from "../log.ts";
import { Toolbox } from "./client.ts";
import { createBashTool } from "./tools/bash.ts";
import { createEditToolProxy, createWriteToolProxy } from "./tools/files.ts";
import { createReadToolProxy } from "./tools/read.ts";

/**
 * Starts the Go service that serves the coding tools when `AGENT_TOOLBOX=go`.
 *
 * Called once at boot, before the harness opens: a toolbox that cannot answer is
 * fatal there, because with no in-process fallback every turn would fail instead.
 */
export async function startToolbox(): Promise<Toolbox> {
  const started = Date.now();
  const toolbox = await Toolbox.start({ bin: config.toolbox.bin, socketPath: config.toolbox.socketPath });
  const outcome = await toolbox.call("ping", {}, { cwd: config.workspace });
  if (outcome.failure !== undefined) throw new Error(`toolbox: ${outcome.failure.message}`);
  const ping = outcome.result as unknown as { version: string; tools: string[] };
  log(
    "toolbox",
    `ready pid=${toolbox.pid} version=${ping.version} tools=${ping.tools.join(",")} in ${Date.now() - started}ms`,
  );
  return toolbox;
}

export { Toolbox };

/**
 * The four coding tools served by the toolbox, under the name pi-durable's own extension uses, so
 * installing this replaces `CodingTools` instead of adding a second `bash` next to it.
 */
export function codingToolsFromToolbox(toolbox: Toolbox) {
  return {
    name: "coding-tools",
    tools: [createReadToolProxy(toolbox), createWriteToolProxy(toolbox), createEditToolProxy(toolbox), createBashTool(toolbox)],
  };
}
