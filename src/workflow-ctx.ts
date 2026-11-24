import type { Harness } from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";
import { listProjects, projectOf } from "./projects.ts";

// The two directories a stage report needs: the checkout a chat works in, and the
// repository it belongs to. Both are read from what the chat already stores, never
// guessed, so a report can only describe a directory that exists.

export async function dirForConversation(
  harness: Harness,
  context: Context,
  conversationId: number,
): Promise<string> {
  const conversation = await harness.conversation(conversationId as never, context).catch(() => undefined);
  const agent = conversation === undefined ? undefined : await conversation.agent(context).catch(() => undefined);
  return agent?.cwd ?? process.cwd();
}

export async function projectPathOf(
  harness: Harness,
  context: Context,
  conversationId: number,
): Promise<string> {
  // projectOf answers an id; the path comes from the list, so a chat whose project
  // was removed falls back to its own checkout instead of reporting a missing path.
  const id = await projectOf(harness, context, conversationId).catch(() => undefined);
  if (id !== undefined) {
    const rows = await listProjects(harness, context).catch(() => []);
    const hit = rows.find((row) => row.id === id);
    if (hit !== undefined && hit.path.length > 0) return hit.path;
  }
  return dirForConversation(harness, context, conversationId);
}

/** The project a chat belongs to, for looking up its workflow. */
export async function projectIdOf(
  harness: Harness,
  context: Context,
  conversationId: number,
): Promise<string | undefined> {
  return projectOf(harness, context, conversationId);
}
