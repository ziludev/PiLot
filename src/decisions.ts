/**
 * The decision trail of the current turn: what had to be decided, by whom, and how long it took.
 *
 * A card that only lists tool calls answers "what ran" but not "why it stopped for two minutes",
 * and that second question is the one the owner asks in the morning. The approval path already
 * knows the reason (`approvalReason`) and the wait; the turn knows why it ended. Collecting them
 * here keeps the answer card honest without asking the model to narrate itself.
 *
 * In memory only, keyed by conversation: the trail describes the live turn and is written into
 * `app.turn-details` by whoever settles it. Nothing here is durable on its own.
 */
export type DecisionNote = {
  at: number;
  kind: "approval" | "stop";
  text: string;
};

const TRAILS = new Map<string, DecisionNote[]>();
const MAX_NOTES = 12;

export function noteDecision(key: string, note: Omit<DecisionNote, "at">, now = Date.now()): void {
  const trail = TRAILS.get(key) ?? [];
  trail.push({ ...note, at: now });
  // A loop that asks twelve questions is already a problem; the thirteenth line tells nobody more.
  TRAILS.set(key, trail.slice(-MAX_NOTES));
}

/** Reads and clears: a settled turn owns its trail, and the next turn starts empty. */
export function takeDecisions(key: string): DecisionNote[] {
  const trail = TRAILS.get(key) ?? [];
  TRAILS.delete(key);
  return trail;
}

/** `14:07 你允许了 write · 等了 2 分钟` — the card side wants a clock, not an epoch. */
export function formatDecision(note: DecisionNote): string {
  const at = new Date(note.at);
  const clock = `${String(at.getHours()).padStart(2, "0")}:${String(at.getMinutes()).padStart(2, "0")}`;
  return `${clock} ${note.text}`;
}
