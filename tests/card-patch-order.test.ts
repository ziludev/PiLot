import assert from "node:assert/strict";
import { test } from "node:test";

const sleep = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

test("patches to one card land in the order they were asked for", async () => {
  const { serializePatch } = await import("../src/feishu.ts");
  const order: string[] = [];
  // The first patch is slow, the second fast: without a queue the fast one would finish first and
  // the card would keep the older content — which is exactly how a finished card stayed "处理中".
  const slow = serializePatch("om_same", async () => {
    await sleep(30);
    order.push("slow");
  });
  const fast = serializePatch("om_same", async () => {
    order.push("fast");
  });
  await Promise.all([slow, fast]);
  assert.deepEqual(order, ["slow", "fast"]);
});

test("a failed patch does not hold back the patch behind it", async () => {
  const { serializePatch } = await import("../src/feishu.ts");
  const ran: string[] = [];
  const broken = serializePatch("om_fail", async () => {
    throw new Error("feishu 230020");
  });
  const after = serializePatch("om_fail", async () => {
    ran.push("after");
  });
  await assert.rejects(broken, /230020/);
  await after;
  assert.deepEqual(ran, ["after"]);
});

test("different cards patch independently", async () => {
  const { serializePatch } = await import("../src/feishu.ts");
  let released: (() => void) | undefined;
  const gate = new Promise<void>((resolve) => {
    released = resolve;
  });
  const held = serializePatch("om_a", async () => {
    await gate;
  });
  // If the queue were global this would wait for om_a's patch to finish.
  await serializePatch("om_b", async () => {});
  released?.();
  await held;
});
