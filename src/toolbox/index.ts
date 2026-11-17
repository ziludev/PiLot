import { config } from "../config.ts";
import { log } from "../log.ts";
import { Toolbox } from "./client.ts";
import { createBashToolProxy } from "./tools/bash.ts";
import { createEditToolProxy, createWriteToolProxy } from "./tools/files.ts";
import { createReadToolProxy } from "./tools/read.ts";

/**
 * Starts the Go service that serves the coding tools.
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
 * The four coding tools served by the toolbox, under the extension name pi-durable's own coding
 * tools use: installing this replaces that extension rather than adding a second `bash`.
 */
export function codingToolsFromToolbox(toolbox: Toolbox) {
  return {
    name: "coding-tools",
    tools: [createReadToolProxy(toolbox), createWriteToolProxy(toolbox), createEditToolProxy(toolbox), createBashToolProxy(toolbox)],
  };
}
