import type { Harness } from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";
import { log } from "./log.ts";

// Which extensions exist, and which of them this chat is using. Installed is the
// registry's business; enabled is the conversation's, because a chat can turn one
// off without changing what any other chat sees.
//
// Read-only for installed: nothing here can install or uninstall. A page that could
// remove an extension would need the boot order, and the boot order is code.

// System extensions: the bridge's own machinery. The hands a turn runs on, the
// store it writes to, the bookkeeping the window draws from. A page that offered
// these a switch would be offering to break the app it is drawn by, so it does
// not offer one — and setExtension refuses even if asked directly.
const SYSTEM = new Set([
  "coding-tools",
  "batching",
  "deferred-tools",
  "projects",
  "workflow",
  "usage-sampler",
  "turn-log",
  "turn-snapshot",
  "context-guard",
  "stall",
  "title",
  "digest",
  "done",
  "guard",
]);

export type ExtensionRow = {
  name: string;
  namespace?: string;
  /** Tool names, so a page can say what turning it off would take away. */
  tools: string[];
  sections: number;
  hooks: number;
  wraps: number;
  tasks: number;
  enabled: boolean;
  /** Why it cannot be switched off from a page, when it cannot. */
  fixed?: string;
};

export type RegistryLike = {
  snapshot(): {
    installed(): ReadonlyArray<{ name: string; namespace?: unknown; tools?: readonly unknown[]; sections?: readonly unknown[]; hooks?: readonly unknown[]; wraps?: readonly unknown[]; tasks?: readonly unknown[] }>;
  };
};

/** Everything installed, with this chat's view of which are on. */
export async function listExtensions(
  harness: Harness,
  registry: RegistryLike,
  context: Context,
  conversationId: number,
): Promise<ExtensionRow[]> {
  const snapshot = registry.snapshot();
  const on = new Set<string>();
  const conversation = await harness.conversation(conversationId as never, context).catch(() => undefined);
  const agent = conversation === undefined ? undefined : await conversation.agent(context).catch(() => undefined);
  for (const extension of agent?.extensions ?? []) on.add(extension.name);
  const rows: ExtensionRow[] = [];
  for (const extension of snapshot.installed()) {
    rows.push({
      name: extension.name,
      namespace: typeof extension.namespace === "string" ? extension.namespace : undefined,
      tools: (extension.tools ?? []).map((tool) => String((tool as { name?: string }).name ?? "")),
      sections: (extension.sections ?? []).length,
      hooks: (extension.hooks ?? []).length,
      wraps: (extension.wraps ?? []).length,
      tasks: (extension.tasks ?? []).length,
      enabled: on.size === 0 ? true : on.has(extension.name),
    });
  }
  // The tools that a turn runs on cannot be removed: without them the model has no
  // hands, and a page that offers the switch would be lying.
  for (const row of rows) {
    // The tools a turn runs on: without them the model has no hands, so the page
    // does not offer the switch rather than offering one that would be refused.
    if (SYSTEM.has(row.name)) row.fixed = "系统扩展，不能关闭";
  }
  return rows;
}

/**
 * Turns one extension on or off for one chat. The write is a real change to what the
 * model sees next turn, so it is reported rather than assumed.
 */
export async function setExtension(
  harness: Harness,
  registry: RegistryLike,
  context: Context,
  conversationId: number,
  name: string,
  enabled: boolean,
): Promise<{ status: "ok" | "unknown" | "fixed"; extensions: ExtensionRow[] }> {
  const snapshot = registry.snapshot();
  const target = snapshot.installed().find((extension) => extension.name === name);
  if (target === undefined) return { status: "unknown", extensions: await listExtensions(harness, registry, context, conversationId) };
  if (!enabled && SYSTEM.has(name)) {
    return { status: "fixed", extensions: await listExtensions(harness, registry, context, conversationId) };
  }
  const conversation = await harness.conversation(conversationId as never, context).catch(() => undefined);
  if (conversation === undefined) return { status: "unknown", extensions: await listExtensions(harness, registry, context, conversationId) };
  const change = enabled
    ? { extensions: { add: [target as never] } }
    : { extensions: { remove: [target as never] } };
  await conversation.configure(change as never, context);
  log("extensions", "toggled", { conversationId, name, enabled });
  return { status: "ok", extensions: await listExtensions(harness, registry, context, conversationId) };
}
