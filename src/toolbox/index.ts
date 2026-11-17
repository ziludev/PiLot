import { config } from "../config.ts";
import { log } from "../log.ts";
import { Toolbox } from "./client.ts";

/**
 * Starts the Go service that serves the coding tools when `AGENT_TOOLBOX=go`.
 *
 * Called once at boot, before the harness opens: a toolbox that cannot answer is
 * fatal there, because with no in-process fallback every turn would fail instead.
 */
export async function startToolbox(): Promise<Toolbox> {
  const started = Date.now();
  const toolbox = await Toolbox.start({ bin: config.toolbox.bin, socketPath: config.toolbox.socketPath });
  const ping = (await toolbox.call("ping", {}, { cwd: config.workspace })) as { version: string; tools: string[] };
  log(
    "toolbox",
    `ready pid=${toolbox.pid} version=${ping.version} tools=${ping.tools.join(",")} in ${Date.now() - started}ms`,
  );
  return toolbox;
}

export { Toolbox };
