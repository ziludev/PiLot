import { config, requireFeishuOwner } from "./config.ts";
import { openRuntime } from "./harness.ts";
import { startFeishuBridge } from "./feishu.ts";
import { startAgentServer } from "./server.ts";
import { bootNews, clearBoot, markBoot } from "./restart.ts";
import { dirname } from "node:path";

// Next to the database, so a test database and a real one keep their own notes.
const stateDir = dirname(config.dbPath);
// Read before marking: the marker we leave behind is what a later boot reads as "died badly".
const bootMessage = bootNews(stateDir, Date.now(), process.pid);
markBoot(stateDir, process.pid);

const runtime = await openRuntime();
console.log(
  `[agent] model=${config.model.provider}/${config.model.modelId} workspace=${config.workspace} db=${config.dbPath}`,
);

// Local client bridge: the desktop app is just another watcher on the same storage.
const server = await startAgentServer(runtime);

// A misconfigured Feishu side must not cost the surfaces that do work: the bridge is the only
// part that needs the app credentials and an owner, so a refusal there leaves the client and the
// CLI running and says why. Exiting instead would let launchd restart into the same error.
let bridge: { stop: () => void; notifyOwner?: (text: string) => Promise<void> } = { stop: () => {} };
try {
  requireFeishuOwner();
  bridge = await startFeishuBridge(runtime);
  const announce = bridge.notifyOwner?.bind(bridge);
  if (bootMessage !== undefined && announce !== undefined) {
    // Nobody is waiting on this message, so a failed send must not cost the boot.
    await announce(bootMessage).catch((error: unknown) => {
      console.error("[agent] could not announce the restart", error);
    });
  }
} catch (error) {
  console.error(`[feishu] not started: ${(error as Error).message}`);
  console.error("[feishu] 桌面客户端和 CLI 仍然可用；修好配置后重启进程。");
}

// A storage error closes the harness for good; nothing works afterwards. Exit so
// launchd restarts a clean process instead of leaving a zombie serving errors.
void runtime.harness.closed.then((end) => {
  console.error(`[agent] harness closed: ${end.kind}`);
  clearBoot(stateDir);
  bridge.stop();
  void server.close().catch(() => {});
  process.exit(end.kind === "failed" ? 1 : 0);
});

let closing = false;
async function shutdown(signal: string): Promise<void> {
  if (closing) return;
  closing = true;
  console.log(`[agent] ${signal}, closing`);
  clearBoot(stateDir);
  bridge.stop();
  await server.close().catch(() => {});
  try {
    // Waits for running task code to return, so in-flight turns settle cleanly.
    await runtime.close();
  } finally {
    process.exit(0);
  }
}

process.on("SIGINT", () => void shutdown("SIGINT"));
process.on("SIGTERM", () => void shutdown("SIGTERM"));
