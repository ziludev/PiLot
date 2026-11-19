/**
 * Outbound plumbing the extensions need but cannot own: sending a message to a surface
 * (Feishu chat) and asking a human to approve a tool call. The bridge installs both at
 * boot; the CLI installs a stdout/stdin version. Failures fail closed.
 */
type Notifier = (surfaceId: string, text: string) => Promise<void>;

let notifier: Notifier | undefined;

export function setNotifier(next: Notifier | undefined): void {
  notifier = next;
}

export async function notify(surfaceId: string, text: string): Promise<void> {
  if (notifier === undefined) {
    console.log(`[notify:${surfaceId}] ${text}`);
    return;
  }
  await notifier(surfaceId, text);
}

/**
 * Starts a turn in a chat as if a message had arrived — the routine timer's way of knocking. The
 * bridge installs the Feishu version (card, queue slot, delivery); the CLI prints it, because a
 * routine firing on the command line has nobody to talk to.
 */
type TurnStarter = (surfaceId: string, text: string, key: string) => Promise<boolean>;

let turnStarter: TurnStarter | undefined;

export function setTurnStarter(next: TurnStarter | undefined): void {
  turnStarter = next;
}

/**
 * True when a turn was actually started; false when there was nowhere to run it. `key` is the
 * request id, and it must be stable across attempts so a retry dedupes instead of running twice.
 */
export async function startTurn(surfaceId: string, text: string, key: string): Promise<boolean> {
  if (turnStarter === undefined) {
    console.log(`[turn:${surfaceId}] ${text}`);
    return false;
  }
  return turnStarter(surfaceId, text, key);
}

export type ApprovalRequest = {
  /** Tool call id; unique per call and used as the durable memo key. */
  callId: string;
  /** Conversation the blocked call belongs to, so the wait can be charted on its turn. */
  conversationId: number;
  /** Risk class, for a grant that covers the rest of the conversation. */
  grantKey?: string;
  tool: string;
  /** One-line summary of what the model is about to do. */
  summary: string;
  /** Why it needs a human. */
  reason: string;
  surfaceId: string;
  timeoutMs: number;
};

type ApprovalDecider = (request: ApprovalRequest) => Promise<boolean>;

let decider: ApprovalDecider | undefined;

export function setApprovalDecider(next: ApprovalDecider | undefined): void {
  decider = next;
}

export async function requestApproval(request: ApprovalRequest): Promise<boolean> {
  if (decider === undefined) {
    // No decider means no human to ask: deny rather than silently run a risky call.
    console.warn(`[approval] no decider configured, denying ${request.tool}`);
    return false;
  }
  return decider(request);
}

/**
 * conversation -> surface map, kept in memory only. The durable copy lives in the
 * session document `app.chats`; this is the fast lookup the extensions use to answer
 * "where do I send this?" without touching storage in a hot path.
 */
const surfaces = new Map<number, string>();

export function rememberSurface(conversationId: number, surfaceId: string): void {
  surfaces.set(conversationId, surfaceId);
}

export function surfaceForConversation(conversationId: number): string | undefined {
  return surfaces.get(conversationId);
}

/** The conversation a surface last ran in, for callers that hold a chat id and need its ledger. */
export function conversationForSurface(surfaceId: string): number | undefined {
  for (const [conversationId, surface] of surfaces) if (surface === surfaceId) return conversationId;
  return undefined;
}
