import { appendFileSync, mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { defineExtension, hook, GenerationTask, type EntryRecord } from "@earendil-works/pi-durable";
import { config } from "./config.ts";
import { logError } from "./log.ts";

/**
 * One line per finished run: how many assistant messages, how many calls, which tools.
 *
 * Without it, "did that change help?" was answered by a throwaway script reading a 338 MB JSON
 * store and re-deriving the same counts every time. A row here is the same answer for ten lines of
 * `jq`, and it is the only way a prompt change can be judged instead of argued about.
 *
 * JSON lines, not a table: the rows are read by hand, never by the agent, and a file cannot
 * corrupt a schema or fail a migration.
 */
export const TURN_LOG = join(dirname(config.dbPath), "turns.jsonl");

export type TurnShape = { rounds: number; calls: number; tools: Record<string, number> };

/** What a run did, from the entries it left behind. Reads no clock, so it is plain to test. */
export function shapeOf(entries: readonly Pick<EntryRecord, "kind" | "model">[]): TurnShape {
  const tools: Record<string, number> = {};
  let rounds = 0;
  let calls = 0;
  for (const entry of entries) {
    if (entry.kind !== "pi.assistant" || entry.model === undefined) continue;
    rounds += 1;
    for (const message of entry.model) {
      for (const block of message.content as readonly { type?: string; name?: string }[]) {
        if (block.type !== "toolCall" || typeof block.name !== "string") continue;
        calls += 1;
        tools[block.name] = (tools[block.name] ?? 0) + 1;
      }
    }
  }
  return { rounds, calls, tools };
}

/** One row, in the shape the log is read back as. */
export function turnRow(
  conversationId: string,
  reason: string,
  shape: TurnShape,
  at = new Date(),
): Record<string, unknown> {
  return { at: at.toISOString(), conversation: conversationId, reason, ...shape };
}

export const turnLogExtension = defineExtension({
  name: "turn-log",
  tasks: [],
  hooks: [
    hook(GenerationTask, {
      // A run that ends any other way (turn ceiling, model error) never reaches here; those are
      // counted from the failure itself, which is where they already show up.
      afterRun: async (run, api, context) => {
        try {
          const page = await api.entries(
            { minEntryId: run.first, maxEntryId: run.last },
            2000,
            undefined,
            context,
          );
          const row = turnRow(String(api.conversationId), run.reason, shapeOf(page.items));
          mkdirSync(dirname(TURN_LOG), { recursive: true });
          appendFileSync(TURN_LOG, `${JSON.stringify(row)}\n`);
        } catch (error) {
          logError("turn-log", "could not record the run", error);
        }
        return { tasks: [] };
      },
    }),
  ],
});
