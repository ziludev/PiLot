import type { Message } from "@earendil-works/pi-ai";
import { estimateMessageTokens as upstream } from "@earendil-works/pi-ai/utils/estimate";

/**
 * Token counts for our own material, which is mostly Chinese.
 *
 * `pi-ai` estimates at 3.5 characters per token — right for English, wrong by about 3.5x for
 * Chinese, where one character is roughly one token. Every threshold we own is written in tokens
 * ("start trimming past 24k"), so with the upstream number a Chinese run only starts trimming at
 * ~84k characters: the constant looks right and never fires. PiBot ships the same correction
 * (`runtime/context-estimate.ts`) for the same reason.
 *
 * This only corrects the estimate; provider usage stays the lower bound wherever it is available.
 */

const ASCII_CHARS_PER_TOKEN = 3.5;

/** Characters above ASCII count one token each; the rest at the upstream ratio. */
export function textTokens(text: string): number {
  let nonAscii = 0;
  // Iterate code points: a surrogate pair is one character but two UTF-16 units, and an emoji costs
  // about two tokens, which is what counting its units gives.
  for (const point of text) if ((point.codePointAt(0) ?? 0) > 127) nonAscii += point.length;
  return Math.ceil((text.length - nonAscii) / ASCII_CHARS_PER_TOKEN + nonAscii);
}

/** What the upstream estimate undercounts for this text. */
function shortfall(text: string): number {
  return textTokens(text) - Math.ceil(text.length / ASCII_CHARS_PER_TOKEN);
}

/** The text inside one message, in the shapes the providers use. */
function texts(message: Message): string[] {
  const found: string[] = [];
  if (message.role === "system") {
    // The system prompt arrives as named sections; a provider that sends a plain string still works
    // through the `content` branch below.
    const sections = (message as { sections?: Record<string, unknown> }).sections;
    if (sections !== undefined && sections !== null) {
      for (const value of Object.values(sections)) if (typeof value === "string") found.push(value);
    }
  }
  const content = (message as { content?: unknown }).content;
  if (typeof content === "string") {
    found.push(content);
  } else if (Array.isArray(content)) {
    for (const part of content) {
      const block = part as { text?: unknown; thinking?: unknown; arguments?: unknown };
      if (typeof block.text === "string") found.push(block.text);
      else if (typeof block.thinking === "string") found.push(block.thinking);
      else if (block.arguments !== undefined) found.push(JSON.stringify(block.arguments) ?? "");
    }
  }
  return found;
}

/**
 * Replace the upstream estimate for one message.
 *
 * The upstream figure already covers structure (images, tool registrations, compaction summaries);
 * this adds back what it undercounts in the text itself rather than re-deriving the whole message.
 */
export function messageTokens(message: Message): number {
  const base = upstream(message);
  const corrected = texts(message).reduce((sum, text) => sum + shortfall(text), 0);
  return base + corrected;
}
