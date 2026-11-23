import { defineDoc, type Harness } from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";
import { log } from "./log.ts";

/**
 * Risk classes the owner allowed for the rest of one conversation.
 * A chat that is deliberately editing a repo outside the workspace should not raise the
 * same card for every file; the grant is per conversation and per class, never global.
 *
 * Grants are stored per conversation under either a class key (`rm`, `write-outside`, …) or
 * `*`, which is the blanket answer to "本会话全部允许".
 */
const SessionGrants = defineDoc<{ byConversation: Record<string, string[]> }>({
  kind: "app.session-grants",
  version: 1,
  scope: "session",
  initial: () => ({ byConversation: {} }),
  checkpointWhen: () => false,
});

/** The key a "本会话全部允许" grant is stored under: every class not in NEVER_BLANKET. */
export const BLANKET = "*";

/**
 * Classes that still ask after a blanket grant. They destroy data or hand control to someone
 * else (`sudo`, `rm -rf`, piping the network into a shell, writing a disk), and one click on a
 * card is not a good enough answer for them.
 */
export const NEVER_BLANKET: ReadonlySet<string> = new Set([
  "sudo",
  "rm",
  "pipe-to-shell",
  "disk",
  "raw-device",
  "power",
]);

/** Cached so the guard does not read the doc on every tool call. */
const cache = new Map<string, Set<string>>();
let loaded = false;

export async function loadGrants(harness: Harness, context: Context): Promise<void> {
  const doc = await harness.snapshot(SessionGrants, context);
  cache.clear();
  for (const [conversationId, keys] of Object.entries(doc?.byConversation ?? {})) {
    cache.set(String(conversationId), new Set(keys));
  }
  loaded = true;
}

export function hasGrant(conversationId: number, key: string): boolean {
  if (!loaded) return false;
  const granted = cache.get(String(conversationId));
  if (granted === undefined) return false;
  if (granted.has(key)) return true;
  return granted.has(BLANKET) && !NEVER_BLANKET.has(key);
}

function rememberGrant(conversationId: number, key: string): void {
  const set = cache.get(String(conversationId)) ?? new Set<string>();
  set.add(key);
  cache.set(String(conversationId), set);
}

export async function grantSession(
  harness: Harness,
  context: Context,
  conversationId: number,
  key: string,
): Promise<void> {
  rememberGrant(conversationId, key);
  await harness.commit(async (tx) => {
    const doc = await tx.doc(SessionGrants);
    const id = String(conversationId);
    const keys = new Set(doc.byConversation[id] ?? []);
    keys.add(key);
    doc.byConversation[id] = [...keys];
  }, context);
  log("approval", "session grant", { conversationId, key });
}

