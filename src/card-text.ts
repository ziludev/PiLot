import { defineDoc, type Harness } from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";

/**
 * What a card said, kept by message id.
 *
 * Every answer goes out as a card, and Feishu will not give a card's text back: `im.message.get`
 * returns the 2.0 placeholder ("请升级至最新版本客户端"), so quoting an answer handed the model a
 * sentence about a client upgrade instead of the answer. The text we sent is the only copy left,
 * so it is written down at send time and read back when somebody quotes the card.
 */
export type SentCard = { text: string; at: number };

/** Enough for the quotes a day actually produces, small enough that the doc stays cheap. */
export const CARD_TEXT_MAX = 300;
/** A quote is context, not a transcript: the answer's opening is what the next question is about. */
const CARD_TEXT_CHARS = 4_000;

export const CardTextDoc = defineDoc<{ cards: Record<string, SentCard> }>({
  kind: "app.card-text",
  version: 1,
  scope: "session",
  initial: () => ({ cards: {} }),
  checkpointWhen: () => false,
});

/** Drops the oldest entries in place, so the doc does not grow for the life of the bot. */
export function pruneCards(cards: Record<string, SentCard>, keep = CARD_TEXT_MAX): void {
  const ids = Object.keys(cards);
  if (ids.length <= keep) return;
  ids
    .sort((a, b) => (cards[a]?.at ?? 0) - (cards[b]?.at ?? 0))
    .slice(0, ids.length - keep)
    .forEach((id) => delete cards[id]);
}

/** Remembers what a card said. Never throws: a lost line must not take the answer down with it. */
export async function rememberCardText(
  harness: Harness,
  context: Context,
  messageId: string | undefined,
  text: string,
): Promise<void> {
  const trimmed = text.trim();
  if (messageId === undefined || messageId.length === 0 || trimmed.length === 0) return;
  await harness
    .commit(async (tx) => {
      const doc = await tx.doc(CardTextDoc);
      doc.cards[messageId] = { text: trimmed.slice(0, CARD_TEXT_CHARS), at: Date.now() };
      pruneCards(doc.cards);
    }, context)
    .catch(() => undefined);
}

/** What we said in a card, or `undefined` for a card somebody else sent. */
export async function sentCardText(
  harness: Harness,
  context: Context,
  messageId: string,
): Promise<string | undefined> {
  const doc = await harness.snapshot(CardTextDoc, context).catch(() => undefined);
  return doc?.cards[messageId]?.text;
}
