import type { Harness } from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";

export const APPROVAL_EVENT = "app.approval";
export const DONE_EVENT = "app.done";

/**
 * One line of a task's story that happened in Feishu, not in the model's rows.
 *
 * The card is the only place "等你批准 rm -rf" and "目标未达成，卡在 X" were written down, so the
 * task dialog — which draws the task's own transcript — showed a turn that asked permission, ran,
 * and ended with nothing said about either. The task id rides in `data` rather than on the entry:
 * an approval is answered outside the task's commit scope, so `byTaskId` would be missing exactly
 * where it is needed.
 */
export async function noteTaskEvent(
  harness: Harness | undefined,
  context: Context | undefined,
  conversationId: number | undefined,
  taskId: number | undefined,
  kind: typeof APPROVAL_EVENT | typeof DONE_EVENT,
  text: string,
): Promise<void> {
  const body = text.replace(/\s+/g, " ").trim();
  if (harness === undefined || context === undefined || conversationId === undefined || taskId === undefined) return;
  if (body.length === 0) return;
  await harness
    .commit(async (tx) => {
      await tx.appendEntry(conversationId as never, {
        kind,
        data: { taskId, text: body.slice(0, 600), at: Date.now() } as never,
      });
    }, context)
    .catch(() => undefined);
}
