import { spawn } from "node:child_process";
import { readdir, readFile, rm } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import * as Lark from "@larksuiteoapi/node-sdk";

import type { JsonObject } from "@earendil-works/pi-durable";
import { config, requireFeishuCredentials, requireFeishuOwner } from "./config.ts";
import { markTree, touchedSince } from "./changes.ts";
import { gatewayClient, gatewayEndpoint, type FeishuIm, type ImResponse } from "./feishu-gw.ts";
import { ChatsToken, context, type AgentRuntime } from "./harness.ts";
import { runInFlight, watchLive, type LiveSnapshot } from "./live.ts";
import { advance, renderRunning, skillOf, stepCount, toolIcon, withIcons, type Trail } from "./steps.ts";
import { composeAnswer, type AnswerBlock } from "./answer.ts";
import {
  conversationForSurface,
  setApprovalDecider,
  setNotifier,
  setTurnStarter,
  type ApprovalRequest,
} from "./channel.ts";
import { log, logError } from "./log.ts";
import { record } from "./audit.ts";
import { rememberCardText, sentCardText } from "./card-text.ts";
import { collectKetchup, elapsed, formatKetchup } from "./ketchup.ts";
import { collectDigest } from "./digest.ts";
import { recordVerdict } from "./verdicts.ts";
import { note } from "./notes.ts";
import { lastRequestMessages, lastRequestTokens } from "./context-guard.ts";
import {
  dead as deadLetters,
  discard as discardDead,
  drain as drainOutbox,
  hold as holdForRetry,
  pending as pendingOutbox,
  redrive,
  releaseHeld,
  type OutboxEntry,
} from "./outbox.ts";

/**
 * Compact after the answer when the next request would carry more than this.
 *
 * Message count alone is a bad proxy: a chat with short messages reaches ~18 tokens each (1600
 * messages in 30k tokens), a chat full of tool output and quoted cards reaches ~235. The same 400
 * means 7k tokens in one and 94k in the other, so both limits stand — whichever trips first.
 */
const COMPACT_ABOVE_TOKENS = 30_000;

/** ...and the message count, for the chat whose messages are cheap. Long context is slow per step. */
const COMPACT_AFTER_MESSAGES = 400;
import { agentName } from "./identity.ts";
import { SurfaceMetaDoc, updateSurfaceMeta } from "./surface-meta.ts";
import { transcribeAudio } from "./asr.ts";
import {
  attachReaction,
  attachSubmission,
  clearCardPending,
  type CardPhase,
  leftoverCards,
  markCardPending,
  moveCard,
  noteCardAttempt,
} from "./cards.ts";
import { lookupMessage, topicTitle } from "./threads.ts";
import { readTurnDetails, saveTurnDetails, type TurnDetails } from "./details.ts";
import { formatDecision, noteDecision, takeDecisions } from "./decisions.ts";
import { callHints, shortRole, spanText, stageLine, subAgentStagesSince } from "./activity.ts";
import { doneVerdict, takeDone } from "./done.ts";
import {
  adoptChatLevels,
  chatAccess,
  chatTopicOf,
  levelOf,
  ownerChatId,
  rememberOwnerChat,
  setChatLevel,
  setChatTopic,
  setChatTurn,
  setIsolatedPolicy,
  proactiveOf,
  setProactive,
  type ChatLevel,
  type ProactiveMode,
} from "./access.ts";
import { dropAllApprovals, getApproval, pendingCount, registerApproval, resolveApproval } from "./approvals.ts";
import { BLANKET } from "./grants.ts";
import { addChatNote, chatNotes, clearChatNotes, removeChatNote } from "./memory.ts";

type IncomingMessage = {
  sender?: { sender_id?: { open_id?: string } };
  message?: {
    message_id?: string;
    chat_id?: string;
    chat_type?: string;
    message_type?: string;
    content?: string;
    /** Reply chain: `parent_id` is the answered message, `root_id` the chain start. */
    parent_id?: string;
    root_id?: string;
    /** 话题 groups give every message a thread id. */
    thread_id?: string;
    mentions?: Array<{ key?: string; name?: string; id?: { open_id?: string } }>;
  };
};

type CardAction = {
  operator?: { open_id?: string };
  action?: { value?: Record<string, unknown> };
  context?: { open_message_id?: string; open_chat_id?: string };
};

export const MAX_CARD_CHARS = 9_000;

/**
 * Whether a message came from this app — the bot's own answers included.
 *
 * Feishu reports a message we sent with `sender.id` set to the app id (`cli_...`) and the bot's
 * open_id in `open_bot_id`; a person arrives as `ou_...` with `sender_type: "user"`. Comparing `id`
 * with the bot's open_id is therefore never true, which quietly turned every quote of our answer in
 * a group into "not ours" and the turn into "you forgot to @ me".
 */
export function writtenByApp(
  sender: { id?: string; open_bot_id?: string; sender_type?: string } | undefined,
  appId: string,
  botOpenId: string | undefined,
): boolean {
  if (sender?.sender_type !== "app") return false;
  if (appId.length > 0 && sender.id === appId) return true;
  return botOpenId !== undefined && sender.open_bot_id === botOpenId;
}

/**
 * What the gateway poll should do next. Pure, because the interesting behaviour only shows up
 * after ten misses at thirty seconds each — five minutes of a silent bot — and a test should not
 * take five minutes.
 *
 * `alert` fires once on the way down and again every half hour; `recovered` fires exactly once,
 * on the first healthy answer after an alert.
 */
export function gatewayWatchStep(
  state: { misses: number; alertedAt: number },
  healthy: boolean,
  now: number,
  limits = { missesBeforeAlert: 10, alertAgainMs: 30 * 60_000 },
): { state: { misses: number; alertedAt: number }; act: "none" | "alert" | "recovered" } {
  if (healthy) {
    return { state: { misses: 0, alertedAt: 0 }, act: state.alertedAt !== 0 ? "recovered" : "none" };
  }
  const misses = state.misses + 1;
  const quiet = state.alertedAt !== 0 && now - state.alertedAt < limits.alertAgainMs;
  if (misses < limits.missesBeforeAlert || quiet) return { state: { misses, alertedAt: state.alertedAt }, act: "none" };
  return { state: { misses, alertedAt: now }, act: "alert" };
}

function isWithdrawnError(error: unknown): boolean {
  const code = (error as { code?: unknown } | undefined)?.code;
  if (code === 230011) return true;
  const message = error instanceof Error ? error.message : String(error ?? "");
  return /\b230011\b|withdrawn/i.test(message);
}

/**
 * Patches to one message run one at a time, in the order they were asked for.
 *
 * The live card repaints on a timer and on every step, and the finished answer is a patch of its
 * own. Those are independent fetches, so they can reach Feishu out of order — and then a card that
 * finished keeps the working header and a half-written body, while the log says "final card
 * patched" because the answer's patch was accepted too, just earlier. Serialising per message makes
 * "the last patch wins" true rather than likely: the answer is queued behind any paint already in
 * flight, and nothing new is queued once the run has settled.
 */
const patchQueues = new Map<string, Promise<unknown>>();

export function serializePatch<T>(key: string, task: () => Promise<T>): Promise<T> {
  const previous = patchQueues.get(key) ?? Promise.resolve();
  // `then(task, task)`: one failed patch must not hold back the patch behind it.
  const next = previous.then(task, task);
  patchQueues.set(key, next);
  const release = (): void => {
    if (patchQueues.get(key) === next) patchQueues.delete(key);
  };
  next.then(release, release);
  return next;
}

const HELP = [
  "**命令**",
  "- `/new` 换新上下文（历史不删，记忆保留）",
  "- `/stop` 中断当前任务",
  "- `/steer 内容` 插话：不排队，直接插进正在跑的那一轮（空闲时就是一条普通消息）",
  "- `/cost` 今日花费",
  "- `/memory` 看记忆内容",
  "- `/memo` 看本群专属记忆；`/memo add 内容` 添加；`/memo del 3` 删第 3 条；`/memo clear` 清空",
  "- `/project` 看/切换工作仓库（带参数即切换）",
  "- `/id` 当前 chat / conversation / 模型（话题里会显示该话题用的模型）",
  "- `/ketchup`（或 `/进度`）自上次以来的动静 + 等你决定的事",
  "- `/digest`（或 `/早报`）过夜那一轮汇总：哪些没达成、审批发生了什么",
  "- `/model` 换本会话模型（卡片按钮）；`/model <id>`、`/model thinking high`、`/model default <id>`",
  "",
  "直接说话就行：能查文件、跑命令、定时提醒、记长期记忆。",
].join("\n");

type CardButton = { label: string; value: JsonObject; type?: "primary" | "danger" | "default" };

/**
 * A failed turn leaves the card with nothing to press, so the only way on was typing "继续"
 * (from a phone, on a long answer). The button submits a continue for the same conversation.
 * `messageId` is the question the card answered, so a second failure stays pressable.
 */
function continueButton(messageId: string): CardButton[] {
  return [{ label: "继续", value: { kind: "continue", messageId }, type: "primary" }];
}

/**
 * The colour bar is the whole point: a green "答完了", an orange "到了步数上限" and a red
 * "出错了" sit in the same list, and the outcome has to be readable without opening it.
 */
type CardHeader = { template: "green" | "orange" | "grey" | "red" | "blue"; title: string };

/**
 * The band a card wears while the turn runs. A patch replaces the whole card, so every live patch
 * has to carry it again — otherwise the header exists only until the first update 15s later.
 */
const WORKING_HEADER: CardHeader = { template: "blue", title: "⏳ 处理中" };
/**
 * Feishu's own two status marks on the message a turn answers. They live outside the card on
 * purpose: a card patch is silent, so in a group these ticks are what a passer-by reads without
 * opening the thread.
 */
const REACTION_WORKING = "OnIt";
const REACTION_DONE = "DONE";

/** Why a run stopped, in words a person uses. The enum stays in the log and in the transcript. */
export function stopReason(reason: string | undefined): string {
  if (reason === undefined || reason === "unanswered") return "模型没给出答案";
  if (reason === "turn_limit") return `跑到 ${config.maxTurns} 步上限`;
  if (reason === "aborted") return "被叫停了";
  return reason;
}

/** The band on a card that is not the turn's own: a recovered answer arrives as a new message. */
function standaloneHeader(reason: string | undefined): CardHeader {
  return reason === "turn_limit"
    ? { template: "orange", title: `⏸️ 到了 ${config.maxTurns} 步上限` }
    : { template: "grey", title: "⚠️ 这轮没跑完" };
}

/**
 * The steps of a turn as a folded panel in the answer card itself. It used to be a 「查看详情」
 * button — only in 1:1 chats, and it answered with a second card, so a group never saw the work
 * at all and every answer looked like a form to fill in.
 */
/** `**bash** \`git status\``: the tool keeps its own name, the hint is what tells two rows apart.
 *  A backtick in the command would end the inline code span early, so it becomes a quote. */
export function stepHeadline(step: { name: string; hint?: string }): string {
  const hint = (skillOf(step.hint) ?? (step.hint ?? "").replace(/`/g, "'")).trim();
  return hint.length === 0 ? `${toolIcon(step.name)} **${step.name}**` : `${toolIcon(step.name, hint)} **${step.name}** \`${hint}\``;
}

/** The panel is a footnote, not a second copy of the answer: a 60-step turn shown in full is a
 *  card nobody scrolls. Past this much output the older steps keep their headline and lose their
 *  text; past this many steps the oldest ones collapse into a count. */
export const PANEL_PREVIEW_CHARS = 6_000;
export const PANEL_MAX_STEPS = 40;

/** `bash ×12、read ×3` — what happened before the part that is still listed. */
function toolTally(steps: Array<{ name: string }>): string {
  const counts = new Map<string, number>();
  for (const step of steps) counts.set(step.name, (counts.get(step.name) ?? 0) + 1);
  return [...counts].map(([name, count]) => `${name} ×${count}`).join("、");
}

export function detailPanel(details: Omit<TurnDetails, "at">): Record<string, unknown> {
  const shown = details.steps.slice(-PANEL_MAX_STEPS);
  const dropped = details.steps.length - shown.length;
  // Preview budget goes to the newest steps first: the end of a turn is what is being read.
  const withPreview = new Set<number>();
  let budget = PANEL_PREVIEW_CHARS;
  for (let index = shown.length - 1; index >= 0; index -= 1) {
    const size = (shown[index]?.preview ?? "").length;
    if (size > budget) continue;
    budget -= size;
    withPreview.add(index);
  }
  const lines = shown.map((step, index) => {
    const mark = step.ok ? "✅" : "⚠️";
    // A tool's output is whatever the tool printed: aligned columns and pipes included, which the
    // card markdown would flatten. The panel body goes through the same element conversion.
    const preview = withPreview.has(index) ? (step.preview.length > 0 ? step.preview : "（无输出）") : "（输出略）";
    return `${dropped + index + 1}. ${mark} ${stepHeadline(step)}\n\n${preview}`;
  });
  if (dropped > 0) lines.unshift(`更早的 ${dropped} 步：${toolTally(details.steps.slice(0, dropped))}`);
  // The trail goes first: "why it took two minutes" is read before "what it ran".
  const decisionLines = (details.decisions ?? []).map(formatDecision);
  const body = decisionLines.length > 0 ? ["**决策**", ...decisionLines, ...lines] : lines;
  // Steps and decisions only. The duration lives in the ping under the card — the one place a
  // reader can see it — and printing it here as well made the two messages stutter at each other.
  // Money never belonged in a panel header nobody opens it for.
  const summary = `${details.steps.length} 步${decisionLines.length > 0 ? ` · ${decisionLines.length} 次决策` : ""}`;
  const elements = bodyElements(body.join("\n\n"));
  if (details.seconds > 0 || details.costUsd > 0.00005) {
    const metaParts: string[] = [];
    if (details.seconds > 0) metaParts.push(`耗时 ${spanText(details.seconds * 1000)}`);
    if (details.costUsd > 0.00005) metaParts.push(`花费 $${details.costUsd.toFixed(4)}`);
    elements.push({ tag: "markdown", content: metaParts.join(" · "), text_size: "notation" });
  }
  return {
    tag: "collapsible_panel",
    expanded: false,
    header: {
      title: { tag: "markdown", content: `**本轮执行详情** · ${summary}` },
      vertical_align: "center",
      icon: { tag: "standard_icon", token: "down-small-ccm_outlined", size: "16px 16px" },
      icon_position: "right",
      icon_expanded_angle: -180,
    },
    border: { color: "grey", corner_radius: "5px" },
    vertical_spacing: "8px",
    padding: "8px 8px 8px 8px",
    elements,
  };
}

/** A markdown table, as a card needs it: Feishu's markdown has no table syntax, so a table in an
 *  answer turns into a wall of pipes unless it becomes a real `table` element. */
type TableBlock = { columns: string[]; rows: string[][] };

// `|---|:---:|---:|` — the row that turns the line above it into a header.
const TABLE_RULE = /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/;

function cells(line: string): string[] {
  const trimmed = line.trim().replace(/^\|/, "").replace(/\|$/, "");
  // A `\|` inside a cell is the author escaping a pipe, not a column break.
  return trimmed.split(/(?<!\\)\|/).map((cell) => cell.trim().replace(/\\\|/g, "|"));
}

/** Splits markdown into text chunks and tables, in order. Text keeps its own newlines. */
export function splitTables(markdown: string): Array<string | TableBlock> {
  const lines = markdown.split("\n");
  const parts: Array<string | TableBlock> = [];
  let text: string[] = [];
  const flush = (): void => {
    if (text.length > 0) parts.push(text.join("\n"));
    text = [];
  };
  for (let index = 0; index < lines.length; index += 1) {
    const header = lines[index] ?? "";
    const rule = lines[index + 1] ?? "";
    if (!header.includes("|") || !TABLE_RULE.test(rule)) {
      text.push(header);
      continue;
    }
    const columns = cells(header);
    const rows: string[][] = [];
    index += 2;
    for (; index < lines.length; index += 1) {
      const row = lines[index] ?? "";
      if (!row.includes("|")) {
        index -= 1;
        break;
      }
      // Pad short rows, drop extra cells: the table element needs every column filled.
      rows.push(Array.from({ length: columns.length }, (_, at) => cells(row)[at] ?? ""));
    }
    flush();
    parts.push({ columns, rows });
  }
  flush();
  return parts;
}

/** Feishu's card markdown draws `backticks` literally, so inline code loses the ticks rather than
 *  showing them. Fenced blocks are left alone: those do render. */
export function stripInlineCode(markdown: string): string {
  let fenced = false;
  return markdown
    .split("\n")
    .map((line) => {
      if (line.trim().startsWith("```")) {
        fenced = !fenced;
        return line;
      }
      return fenced ? line : line.replace(/`([^`\n]+)`/g, "$1");
    })
    .join("\n");
}

function tableElement(block: TableBlock): Record<string, unknown> {
  return {
    tag: "table",
    page_size: 10,
    row_height: "low",
    header_style: { text_align: "left", text_size: "normal", background_style: "grey", bold: true, lines: 1 },
    columns: block.columns.map((name, index) => ({
      name: `col_${index}`,
      display_name: name.length > 0 ? name : " ",
      data_type: "text",
      width: "auto",
      horizontal_align: "left",
    })),
    rows: block.rows.map((row) => Object.fromEntries(block.columns.map((_, index) => [`col_${index}`, row[index] ?? ""]))),
  };
}

// `---`, `***` and `___` on their own line: the card markdown has no rule either, so the line
// would arrive as three dashes.
const RULE_LINE = /^\s{0,3}([-*_])\s*(\1\s*){2,}$/;
// `## Title` — headings are not markdown to Feishu; a bold line reads the same at this size.
const HEADING_LINE = /^\s{0,3}#{1,6}\s+(.*?)\s*#*\s*$/;

/** One text chunk, split on rules, with headings turned into bold lines. */
function textBlockElements(chunk: string): Array<Record<string, unknown>> {
  const elements: Array<Record<string, unknown>> = [];
  let lines: string[] = [];
  const flush = (): void => {
    const content = lines.join("\n").trim();
    lines = [];
    // A markdown element stops at 3000 characters (11310); past that the client shows nothing, so
    // an answer written as one long paragraph has to arrive as several elements.
    if (content.length > 0) for (const piece of chunkMarkdown(content)) elements.push({ tag: "markdown", content: piece });
  };
  for (const line of chunk.split("\n")) {
    if (RULE_LINE.test(line)) {
      flush();
      elements.push({ tag: "hr" });
      continue;
    }
    const heading = HEADING_LINE.exec(line);
    lines.push(heading === null ? line : `**${heading[1] ?? ""}**`);
  }
  flush();
  return elements;
}

export const MAX_MARKDOWN_CHARS = 3_000;

/**
 * Cuts a block of markdown into elements the card will actually show whole.
 *
 * The cut lands on a line boundary wherever one is near; a cross-cut code fence is closed at the
 * end of one element and reopened in the next, because an unterminated fence turns the rest of the
 * answer into code on the client. A single line longer than the cap has nothing to break on and is
 * cut mid-line.
 */
export function chunkMarkdown(content: string, max = MAX_MARKDOWN_CHARS): string[] {
  if (content.length <= max) return [content];
  const pieces: string[] = [];
  let lines: string[] = [];
  let fence: string | undefined;
  const carried = (): number => (fence === undefined ? 0 : fence.length + 1);
  const size = (): number => carried() + lines.reduce((sum, line) => sum + line.length + 1, 0);
  const cut = (): void => {
    if (lines.length === 0) return;
    pieces.push(fence === undefined ? lines.join("\n") : `${lines.join("\n")}\n\`\`\``);
    lines = fence === undefined ? [] : [fence];
  };
  const add = (line: string): void => {
    if (lines.length > 0 && size() + line.length + 1 > max) cut();
    lines.push(line);
    if (/^\s*```/.test(line)) fence = fence === undefined ? line : undefined;
  };
  for (const line of content.split("\n")) {
    if (line.length + carried() + 2 <= max) {
      add(line);
      continue;
    }
    const room = Math.max(200, max - carried() - 2);
    for (let index = 0; index < line.length; index += room) add(line.slice(index, index + room));
  }
  cut();
  return pieces.length > 0 ? pieces : [content];
}

/** Feishu 2.0 cards reject too many native table components; excess tables fall back to code blocks. */
export const MAX_CARD_TABLES = 5;

function tableAsCode(block: TableBlock): Record<string, unknown> {
  const header = `| ${block.columns.join(" | ")} |`;
  const sep = `| ${block.columns.map(() => "---").join(" | ")} |`;
  const body = block.rows.map((row) => `| ${row.join(" | ")} |`);
  return { tag: "markdown", content: ["```", header, sep, ...body, "```"].join("\n") };
}

/** The body of a card: prose as markdown, tables as tables (capped at MAX_CARD_TABLES), rules as rules. */
export function bodyElements(markdown: string): Array<Record<string, unknown>> {
  const elements: Array<Record<string, unknown>> = [];
  let tables = 0;
  for (const part of splitTables(markdown)) {
    if (typeof part === "string") {
      elements.push(...textBlockElements(part));
    } else if (tables < MAX_CARD_TABLES) {
      tables += 1;
      elements.push(tableElement(part));
    } else {
      elements.push(tableAsCode(part));
    }
  }
  return elements.length > 0 ? elements : [{ tag: "markdown", content: markdown }];
}

/**
 * Card JSON **2.0**, and every card goes through here.
 *
 * 2.0 is what makes the detail block possible at all: `collapsible_panel` has no 1.0 form, and
 * without it the steps of a turn either vanished or turned every answer into a form to fill in.
 * 2.0 also *forbids* `note` and `action` (the API answers 200861), so the meta line is a
 * notation-size markdown and the buttons sit in a `column_set`.
 *
 * Do not read a card back with `im.message.get` to check how it looks: Feishu hands a 2.0 message
 * back in the legacy shape, and every element list becomes 「请升级至最新版本客户端，以查看内容」.
 * That placeholder is the read API talking, not the client — a card that renders perfectly still
 * reads back that way.
 */
export function card(
  markdown: string,
  meta?: string,
  buttons?: CardButton[],
  header?: CardHeader,
  details?: Omit<TurnDetails, "at">,
): Record<string, unknown> {
  const elementsFor = (body: string, trail?: Omit<TurnDetails, "at">): Array<Record<string, unknown>> => {
    const elements: Array<Record<string, unknown>> = bodyElements(stripInlineCode(body));
    if (trail !== undefined && trail.steps.length > 0) {
      elements.push(detailPanel(trail));
    }
    if (meta !== undefined && meta.trim().length > 0) {
      elements.push({ tag: "markdown", content: meta, text_size: "notation" });
    }
    if (buttons !== undefined && buttons.length > 0) {
      elements.push({
        tag: "column_set",
        flex_mode: "flow",
        horizontal_spacing: "8px",
        columns: buttons.map((button) => ({
          tag: "column",
          width: "auto",
          elements: [
            {
              tag: "button",
              text: { tag: "plain_text", content: button.label },
              type: button.type ?? "default",
              value: button.value,
            },
          ],
        })),
      });
    }
    return elements;
  };
  const summary = cardSummary(markdown, header);
  const envelope = (body: string, trail?: Omit<TurnDetails, "at">): Record<string, unknown> => ({
    schema: "2.0",
    config: {
      update_multi: true,
      width_mode: "fill",
      // What the chat list and a push notification show instead of the first line of the body.
      ...(summary.length === 0 ? {} : { summary: { content: summary } }),
    },
    ...(header === undefined
      ? {}
      : { header: { template: header.template, title: { tag: "plain_text", content: header.title } } }),
    body: { elements: elementsFor(body, trail) },
  });

  let draft = envelope(markdown, details);
  if (cardBytes(draft) <= CARD_BUDGET_BYTES) return draft;
  // The trail gives way first: the answer is why the message exists, the steps are footnotes.
  if (details !== undefined && details.steps.length > 0) {
    const total = details.steps.length;
    const slim = { ...details, steps: details.steps.slice(-CARD_PANEL_STEPS).map((step) => ({ ...step, preview: "" })) };
    draft = envelope(markdown, slim);
    if (cardBytes(draft) <= CARD_BUDGET_BYTES) {
      log("feishu", "card over budget, step panel trimmed", { steps: total, kept: CARD_PANEL_STEPS, bytes: cardBytes(draft) });
      return draft;
    }
    draft = envelope(`${markdown}\n\n> 这轮跑了 ${total} 步，卡片放不下就没列出来`, undefined);
    if (cardBytes(draft) <= CARD_BUDGET_BYTES) {
      log("feishu", "card over budget, step panel dropped", { steps: total, bytes: cardBytes(draft) });
      return draft;
    }
  }
  // Then the words, a slice at a time: an answer that got shorter still beats one that never
  // arrives, and 230099 is what a card that was too long used to answer with.
  let body = markdown;
  for (let guard = 0; guard < 12 && cardBytes(draft) > CARD_BUDGET_BYTES && body.length > 200; guard += 1) {
    body = `${body.slice(0, Math.max(200, Math.floor(body.length * 0.7)))}\n\n…（太长，后面截掉了）`;
    draft = envelope(body, undefined);
  }
  log("feishu", "card trimmed to fit", { bytes: cardBytes(draft), chars: body.length });
  return draft;
}

/**
 * Splits an oversized answer into multiple card bodies so nothing is silently truncated at the end.
 * Keeps `card()` itself responsible for single-card budget safety.
 */
export function splitAnswerForCards(markdown: string, maxChunkBytes = 18_000): string[] {
  const byteLength = Buffer.byteLength(markdown, "utf8");
  if (byteLength <= maxChunkBytes || markdown.length === 0) return [markdown];
  const avgBytes = byteLength / markdown.length;
  const charBudget = Math.max(1_500, Math.floor(maxChunkBytes / Math.max(1, avgBytes)));
  const raw = chunkMarkdown(markdown, charBudget);
  if (raw.length <= 1) return raw;
  return raw.map((piece, index) =>
    index === 0 ? `${piece}\n\n_（1/${raw.length}，下文续）_` : `**（续 ${index + 1}/${raw.length}）**\n\n${piece}`,
  );
}

/** True when `now` falls inside a `HH:MM-HH:MM` window in Asia/Shanghai; `"off"` or empty returns false. */
export function inQuietHours(spec: string, now = new Date()): boolean {
  const trimmed = spec.trim();
  if (trimmed === "" || trimmed.toLowerCase() === "off") return false;
  const match = /^(\d{1,2}):(\d{2})-(\d{1,2}):(\d{2})$/.exec(trimmed);
  if (match === null) return false;
  const start = Number(match[1]) * 60 + Number(match[2]);
  const end = Number(match[3]) * 60 + Number(match[4]);
  if (Number(match[1]) > 23 || Number(match[2]) > 59 || Number(match[3]) > 23 || Number(match[4]) > 59) return false;
  if (start === end) return false;
  const clockText = now.toLocaleTimeString("en-GB", {
    timeZone: "Asia/Shanghai",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
  const [hh, mm] = clockText.split(":").map(Number);
  const current = (hh ?? 0) * 60 + (mm ?? 0);
  return start < end ? current >= start && current < end : current >= start || current < end;
}

/**
 * A done-ping is news only when the run was long enough to have been waited on. `0` disables the
 * pings entirely — the recovery path used to compare against 0 and so pinged on every restart.
 */
export function shouldPingDone(donePingSeconds: number, elapsedMs: number): boolean {
  if (donePingSeconds <= 0) return false;
  return elapsedMs >= donePingSeconds * 1000;
}

/** Maps a notice's leading severity icon to a matching Feishu card header colour. */
export function noticeHeader(head: string): CardHeader {
  const clean = head.replace(/[*`#]/g, "").trim().slice(0, 80);
  const title = clean || "通知";
  if (/^[🔴❌]/.test(clean)) return { title, template: "red" };
  if (/^[⚠️⏸️⏳]/.test(clean)) return { title, template: "orange" };
  if (/^✅/.test(clean)) return { title, template: "green" };
  return { title, template: "blue" };
}

/** Feishu answers 230099 for a card whose JSON passes 30KB, and the envelope and escaping eat
 *  into that. A 9000-character CJK answer is 27KB before the step panel is added. */
export const CARD_BUDGET_BYTES = 28_000;

/** How many steps survive when the panel has to shrink: the newest ones say what went wrong. */
export const CARD_PANEL_STEPS = 6;

function cardBytes(value: unknown): number {
  return Buffer.byteLength(JSON.stringify(value), "utf8");
}

/** The one line a chat list shows. The header already says what this card is; without one, the
 *  first line of the answer is the best summary there is. */
function cardSummary(markdown: string, header?: CardHeader): string {
  if (header !== undefined) return header.title;
  const first =
    markdown
      .split("\n")
      .map((line) => line.replace(/[#>*_`|~\-\s]+/g, " ").trim())
      .find((line) => line.length > 0) ?? "";
  return first.length > 40 ? `${first.slice(0, 40)}…` : first;
}

function renderDetails(details: TurnDetails): string {
  const lines = details.steps.map((step, index) => {
    const mark = step.ok ? "✅" : "⚠️";
    // A tool's output is whatever the tool printed: aligned columns and pipes included, which the
    // card markdown would flatten. The panel body goes through the same element conversion.
    return `${index + 1}. ${mark} ${stepHeadline(step)}\n\n${step.preview.length > 0 ? step.preview : "（无输出）"}`;
  });
  const header = `本轮用了 **${details.steps.length}** 个工具，耗时 **${details.seconds}s**${
    details.costUsd > 0.00005 ? `，花费 **$${details.costUsd.toFixed(4)}**` : ""
  }`;
  return `${header}\n\n${lines.join("\n\n")}`;
}

/**
 * How a pending approval ended. A settled card keeps the request it was asking about and freezes
 * the answers instead of being replaced by one line: 「✅ 已允许」 alone tells a reader a week later
 * nothing about what they allowed, and in a chat history the line loses the call it referred to.
 * AskHuman's Feishu/DingTalk cards settle the same way — buttons disabled, the used one still
 * highlighted — and we had been doing the Telegram shape (whole card overwritten by a sentence).
 */
export type ApprovalOutcome = {
  ok: boolean;
  /** Which surface answered; `timeout` and `shutdown` mean nobody did. */
  by: "feishu" | "client" | "timeout" | "shutdown";
  /** How long the request waited before it was answered. */
  waitedMs?: number;
  /** Which answer was used: allow / session / all / deny. */
  pick?: string;
};

type ApprovalAnswer = {
  pick: "allow" | "session" | "all" | "deny";
  label: string;
  type: "primary" | "default" | "danger";
  value: Record<string, unknown>;
};

function approvalAnswers(request: ApprovalRequest): ApprovalAnswer[] {
  return [
    { pick: "allow", label: "允许", type: "primary", value: { approval: request.callId, ok: true } },
    // One chat deliberately writing outside the workspace should not raise this
    // card per file. Two answers: this class of call, or everything but the
    // classes that destroy data.
    ...(request.grantKey === undefined
      ? []
      : [
          {
            pick: "session" as const,
            label: "本会话允许这类",
            type: "default" as const,
            value: { approval: request.callId, ok: true, session: true, key: request.grantKey },
          },
          {
            pick: "all" as const,
            label: "本会话全部允许",
            type: "default" as const,
            value: { approval: request.callId, ok: true, session: true, all: true, key: request.grantKey },
          },
        ]),
    { pick: "deny", label: "拒绝", type: "danger", value: { approval: request.callId, ok: false } },
  ];
}

/** The verdict a settled card carries in place of the 「Xs 内不选则视为拒绝」 countdown. */
export function approvalVerdict(
  request: ApprovalRequest,
  outcome: ApprovalOutcome,
): { template: string; title: string; line: string } {
  if (outcome.by === "shutdown") {
    return { template: "grey", title: "⏹️ 未确认就已结束", line: "> 等待确认时服务重启了，这次调用没有执行" };
  }
  if (outcome.by === "timeout") {
    return {
      template: "grey",
      title: "⏹️ 超时未确认，已按拒绝处理",
      line: `> ${Math.round(request.timeoutMs / 1000)}s 内没人回答，${request.tool} 没有执行`,
    };
  }
  const answers = approvalAnswers(request);
  // A caller that only knows yes/no still gets the right label: the grant buttons say 「允许」 too,
  // and claiming 「本会话允许这类」 for a plain allow would overstate what is now permitted.
  const answer =
    answers.find((item) => item.pick === outcome.pick) ??
    answers.find((item) => item.pick === (outcome.ok ? "allow" : "deny"))!;
  const who = outcome.by === "client" ? "桌面客户端" : "飞书";
  const waited = outcome.waitedMs !== undefined && outcome.waitedMs > 0 ? `，等了 ${spanText(outcome.waitedMs)}` : "";
  return outcome.ok
    ? { template: "green", title: `✅ 已允许 · ${answer.label}`, line: `> ${who}选了「${answer.label}」${waited}` }
    : { template: "red", title: "🛑 已拒绝", line: `> ${who}选了「拒绝」${waited}` };
}

export function approvalCard(request: ApprovalRequest, outcome?: ApprovalOutcome): Record<string, unknown> {
  const settled = outcome !== undefined;
  const verdict = outcome === undefined ? undefined : approvalVerdict(request, outcome);
  // Nobody picked anything when it timed out or died: every button flattens, none of them is
  // shown as the answer that was used.
  const nobodyAnswered = settled && (outcome.by === "timeout" || outcome.by === "shutdown");
  const picked = settled && !nobodyAnswered ? (outcome.pick ?? (outcome.ok ? "allow" : "deny")) : undefined;
  const buttons = approvalAnswers(request).map((answer) => ({
    tag: "button",
    text: { tag: "plain_text", content: answer.label },
    // Every button is frozen once answered; the one that was used keeps its colour, so the settled
    // card still reads as 「this is what I picked」 instead of four identical grey rectangles.
    type: settled ? (answer.pick === picked ? answer.type : "default") : answer.type,
    ...(settled ? { disabled: true } : {}),
    value: answer.value,
  }));
  const sourceText =
    request.chatTitle !== undefined && request.chatTitle.trim().length > 0
      ? request.chatTitle.trim()
      : request.surfaceId.length > 0
        ? request.surfaceId
        : undefined;
  return {
    schema: "2.0",
    config: { update_multi: true, width_mode: "fill" },
    header: {
      template: verdict?.template ?? "orange",
      title: { tag: "plain_text", content: verdict?.title ?? "⚠️ 需要你确认" },
    },
    body: {
      elements: [
        { tag: "markdown", content: `${toolIcon(request.tool)} **${request.tool}** — ${request.reason}` },
        { tag: "markdown", content: "```\n" + request.summary.slice(0, 1200) + "\n```" },
        ...(sourceText === undefined
          ? []
          : [{ tag: "markdown", content: `来源：${sourceText}`, text_size: "notation" }]),
        {
          tag: "markdown",
          content: verdict?.line ?? `${Math.round(request.timeoutMs / 1000)}s 内不选则视为拒绝`,
          text_size: "notation",
        },
        {
          tag: "column_set",
          flex_mode: "flow",
          horizontal_spacing: "8px",
          columns: buttons.map((button) => ({ tag: "column", width: "auto", elements: [button] })),
        },
      ],
    },
  };
}

/** `HH:MM` in the owner's timezone, for "started at" lines. */
function clock(at: number): string {
  return new Date(at).toLocaleTimeString("zh-CN", { timeZone: "Asia/Shanghai", hour12: false });
}

/** "3m 12s" — duration as a human reads it. */
function duration(ms: number): string {
  return spanText(ms);
}

export function clip(text: string): string {
  if (text.length <= MAX_CARD_CHARS) return text;
  const cut = text.slice(0, MAX_CARD_CHARS);
  // An unclosed fence would swallow the rest of the card as code.
  const fences = cut.split("\n").filter((line) => line.trim().startsWith("```")).length;
  return `${cut}${fences % 2 === 1 ? "\n```" : ""}\n\n…（已截断）`;
}

/** Long enough for a second line to land, short enough that nobody sits waiting on it. */
const BURST_MS = 1_200;

type BurstItem = {
  message: NonNullable<IncomingMessage["message"]>;
  parsed: ParsedContent;
  senderId: string;
};

const bursts = new Map<number, BurstItem[]>();

/**
 * Two messages typed a few seconds apart are one thought: "看看 X", then "顺便查 Y". Each one
 * starting its own turn pays the system prompt twice, runs two cards for one answer, and the second
 * turn reads a transcript the first one just wrote. So the first message holds for a moment and
 * takes whatever arrived meanwhile.
 *
 * Returns the batch for the first message of a burst; `undefined` means this message was folded
 * into a batch that is already waiting, and the caller should stop where it is.
 */
export async function collectBurst(
  conversationId: number,
  item: BurstItem,
  windowMs = BURST_MS,
): Promise<BurstItem[] | undefined> {
  const live = bursts.get(conversationId);
  if (live !== undefined) {
    live.push(item);
    log("feishu", "message folded into the burst in flight", { conversationId, size: live.length });
    return undefined;
  }
  const batch = [item];
  bursts.set(conversationId, batch);
  await new Promise((resolve) => setTimeout(resolve, windowMs));
  bursts.delete(conversationId);
  return batch;
}

/** What `buildUserContent` hands the model: one string, or text/image parts. */
export type UserContent = string | Array<{ type: "text"; text: string } | { type: "image"; data: string; mimeType: string }>;

/**
 * Mark every message of a burst: two unrelated questions run together read as one rambling
 * request, and the answer then only covers the last one.
 */
export function mergeUserContent(items: UserContent[]): UserContent {
  if (items.length === 1) return items[0]!;
  const out: Exclude<UserContent, string> = [];
  items.forEach((item, index) => {
    out.push({ type: "text", text: `【第 ${index + 1} 条】` });
    if (typeof item === "string") out.push({ type: "text", text: item });
    else out.push(...item);
  });
  return out;
}

/**
 * Turns in one conversation share a working tree and a context window, so two at once would edit
 * the same files blind. Each turn waits for its predecessor; the card says it is waiting, because
 * silence looks like a lost message.
 */
type QueueWaiter = { cancelled: boolean };
const turnQueues = new Map<number, { tail: Promise<void>; waiting: QueueWaiter[] }>();

export type TurnSlot = {
  queued: boolean;
  wait: Promise<void>;
  done: () => void;
  /** True once the owner took this turn back out of the line before it started. */
  cancelled: () => boolean;
};

export function enqueueTurn(conversationId: number): TurnSlot {
  const queue = turnQueues.get(conversationId);
  const previous = queue?.tail;
  let release!: () => void;
  const current = new Promise<void>((resolve) => {
    release = resolve;
  });
  const tail = (previous ?? Promise.resolve()).then(() => current);
  const own: QueueWaiter = { cancelled: false };
  const waiting = queue?.waiting ?? [];
  if (queue === undefined) turnQueues.set(conversationId, { tail, waiting });
  else queue.tail = tail;
  // Only a turn that sits behind someone can be taken out of the line; the running one is stopped
  // the other way (the conversation's abort).
  if (previous !== undefined) waiting.push(own);
  void tail.then(() => {
    const index = waiting.indexOf(own);
    if (index >= 0) waiting.splice(index, 1);
    const live = turnQueues.get(conversationId);
    if (live !== undefined && live.tail === tail) turnQueues.delete(conversationId);
  });
  // `done` must run even when the turn throws, or every later message in this chat waits forever.
  return {
    queued: previous !== undefined,
    wait: previous ?? Promise.resolve(),
    done: release,
    cancelled: () => own.cancelled,
  };
}

/**
 * Takes every turn still waiting in this conversation out of the line, and leaves the running one
 * alone: a stop pressed while a message waits means "not this one", not "not the work". Aborting
 * the whole conversation instead killed the turn in front and then let the waiting one start — the
 * owner's stop landed on the wrong message.
 */
export function cancelQueuedTurns(conversationId: number): number {
  const queue = turnQueues.get(conversationId);
  if (queue === undefined) return 0;
  let cancelled = 0;
  for (const waiter of queue.waiting) {
    if (waiter.cancelled) continue;
    waiter.cancelled = true;
    cancelled += 1;
  }
  return cancelled;
}

/**
 * One stop button, two meanings, decided by what is actually in the line: a message still waiting is
 * what the owner wants gone, because that is the one they just sent and thought better of. Only
 * with nothing queued does the stop reach the running turn. Returns what happened, so the caller
 * says it instead of guessing.
 */
export async function stopConversation(
  runtime: AgentRuntime,
  conversationId: number,
  options: { all?: boolean } = {},
): Promise<{ cancelled: number; aborted: boolean }> {
  const cancelled = cancelQueuedTurns(conversationId);
  // One stop, two meanings, unless the caller asks for `all`: the desktop client's stop button is
  // the panic button, and the messages queued behind a run are more of what is being stopped.
  if (cancelled > 0 && options.all !== true) {
    log("feishu", "stop took queued turns out of the line", { conversationId, cancelled });
    return { cancelled, aborted: false };
  }
  const conversation = await runtime.harness.conversation(conversationId as never, context).catch(() => undefined);
  if (conversation === undefined) return { cancelled, aborted: false };
  await conversation.abort(context);
  if (cancelled > 0) log("feishu", "stop cleared the line and the run", { conversationId, cancelled });
  return { cancelled, aborted: true };
}

/**
 * The user's own line, one row, clipped: two cards side by side must be tellable apart.
 *
 * Cut on a word or a punctuation mark when one is near the limit. A hard cut lands mid-word often
 * enough to look broken — "看看 list conversationstates 接…" reads as damage, not as a title — and
 * Chinese has no spaces to cut on, so the punctuation carries it.
 */
const DIGEST_MAX = 28;
const DIGEST_FLOOR = Math.floor(DIGEST_MAX * 0.6);
const DIGEST_BREAK = /[\s，。、；：,.;:!?！？)）】」”"'…]/;

export function digestOf(text: string): string {
  const oneLine = text.replace(/\s+/g, " ").trim();
  if (oneLine.length <= DIGEST_MAX) return oneLine;
  for (let i = DIGEST_MAX; i >= DIGEST_FLOOR; i -= 1) {
    if (!DIGEST_BREAK.test(oneLine.charAt(i - 1))) continue;
    const cut = oneLine.slice(0, i).trimEnd();
    // A line that is all break characters would clip to nothing; the hard cut is the fallback.
    if (cut.length >= DIGEST_FLOOR) return `${cut}…`;
  }
  return `${oneLine.slice(0, DIGEST_MAX)}…`;
}

/** What a card needs to say more than "处理中": whose turn it is. */
const THINKING_TAIL = 280;

/**
 * How much of a thought fits on a card before it stops being a card.
 *
 * The newest end is the part worth keeping, but cut at a fixed offset it opened mid-sentence and
 * mid-markdown — a reader saw `◌ ...**, trigger 是 aichat 生成时带的…`. So: strip the emphasis the
 * card cannot render inline, then start on a sentence boundary.
 */
function thinkingTail(flat: string): string {
  if (flat.length <= THINKING_TAIL) return flat;
  const window = flat.slice(flat.length - THINKING_TAIL);
  // Only a hard stop counts: a full stop here is as often a version number or an abbreviation.
  const boundary = /[。！？；!?;]\s*/.exec(window);
  if (boundary !== null && boundary.index < THINKING_TAIL / 3) {
    const rest = window.slice(boundary.index + boundary[0].length).trim();
    if (rest.length > THINKING_TAIL / 2) return `…${rest}`;
  }
  return `…${window.trim()}`;
}

/**
 * The model's thinking, flattened to one italic line.
 *
 * Thinking is the only part of a run that says *why*, and it is worth showing while the card is the
 * only thing on screen. It is not worth letting it win: a long reasoning block carries its own
 * headings and lists, and pasted raw it shoves the answer off the card. So: one line, the newest
 * end of it, and the same clip the answer text gets.
 */
function thinkingLine(thinking: string | undefined): string {
  const flat = (thinking ?? "")
    .replace(/\s+/g, " ")
    .replace(/\*\*|__|`/g, "")
    .replace(/^[.\s]+/, "")
    .trim();
  if (flat.length === 0) return "";
  return `_💭 ${thinkingTail(flat)}_`;
}

/**
 * The live card's body: one line of the present, then the answer as it is being written.
 *
 * It used to carry five things at once — a quote of the message, up to five finished rounds, the
 * activity note, the thought and the streaming answer — which on a phone filled the screen without
 * saying what was happening *now*. The finished rounds are in the panel of the answer card; here
 * only the round that is running earns its line, and the subtitle counts the rest.
 */
export function renderLive(live: LiveSnapshot, trail?: Trail, stages?: string[]): string {
  const body = live.text.trim();
  const steps = trail === undefined ? "" : renderRunning(trail, Date.now());
  const thought = thinkingLine(live.thinking);
  // Once there is a round on screen, the thought and the activity note are what the model did
  // *before* it: printing them above the round spends the card saying what the round already says.
  const showThought = thought.length > 0 && steps.length === 0;
  const activity =
    steps.length === 0 && live.activity !== undefined && live.activity.length > 0 ? `_${live.activity}_` : "";
  const parts: string[] = [];
  // A pipeline's stage is the biggest thing happening, so it goes above the tool round.
  if (stages !== undefined && stages.length > 0) parts.push(stages.join("\n"));
  if (steps.length > 0) parts.push(steps);
  else if (body.length === 0 && thought.length === 0 && live.tools.length > 0) parts.push(withIcons(live.tools.join(" · ")));
  if (activity.length > 0) parts.push(activity);
  // Thought sits directly above the answer: the order the model produced them in.
  if (showThought) parts.push(thought);
  if (body.length > 0) parts.push(clip(body));
  else if (thought.length === 0 && (steps.length > 0 || live.tools.length === 0)) parts.push("🤔 思考中…");
  return parts.join("\n\n");
}

type PostElement = { tag?: string; text?: string; image_key?: string };
type ParsedContent = { text: string; imageKeys: string[]; audioKey?: string };

/**
 * `/steer 内容`（或 `/插话 内容`）→ 内容；`""` = 只有命令没有内容；`undefined` = 不是插话命令。
 * 这个分界决定了「插进在跑的这轮」还是「当普通消息走」，所以单独拎出来测。
 */
export function parseSteer(command: string): string | undefined {
  const match = /^\/(?:steer|插话)(?:\s+([\s\S]+))?$/i.exec(command.trim());
  if (match === null) return undefined;
  return (match[1] ?? "").trim();
}

/** The bot's own open_id, learned at startup, so an @ of itself can be dropped entirely. */
let botOpenId: string | undefined;

/**
 * Feishu renders mentions as "@_user_1" tokens. Addressing the bot is not part of the
 * sentence — leaving "@" behind meant "/help" arrived as "@ /help", so no command ever
 * matched in a group. Mentions of other people keep a readable "@name". Pure, for tests.
 */
export function stripMentionsWith(
  text: string,
  mentions: ReadonlyArray<{ key?: string; name?: string; id?: { open_id?: string } }>,
  bot: string | undefined,
): string {
  let out = text;
  for (const mention of mentions) {
    if (mention.key === undefined) continue;
    const isBot = bot !== undefined && mention.id?.open_id === bot;
    out = out.split(mention.key).join(isBot ? "" : `@${mention.name ?? ""}`);
  }
  return out.trim();
}

function stripMentions(message: NonNullable<IncomingMessage["message"]>, text: string): string {
  return stripMentionsWith(text, message.mentions ?? [], botOpenId);
}

/**
 * Feishu delivers a picture as `image`, but a picture with any text (or a screenshot
 * pasted on mobile) arrives as `post` rich text. Both must be read.
 */
function parseContent(message: NonNullable<IncomingMessage["message"]>): ParsedContent {
  let body: unknown;
  try {
    body = JSON.parse(message.content ?? "{}");
  } catch {
    return { text: "", imageKeys: [] };
  }

  if (message.message_type === "text") {
    const text = (body as { text?: string }).text ?? "";
    return { text: stripMentions(message, text), imageKeys: [] };
  }
  if (message.message_type === "image") {
    const key = (body as { image_key?: string }).image_key;
    return { text: "", imageKeys: key === undefined ? [] : [key] };
  }
  if (message.message_type === "audio") {
    const key = (body as { file_key?: string }).file_key;
    return { text: "", imageKeys: [], audioKey: key };
  }
  if (message.message_type === "post") {
    const rows = (body as { content?: PostElement[][] }).content ?? [];
    const lines: string[] = [];
    const imageKeys: string[] = [];
    for (const row of rows) {
      const parts: string[] = [];
      for (const element of row) {
        if (element.tag === "text" && typeof element.text === "string") parts.push(element.text);
        else if (element.tag === "a" && typeof element.text === "string") parts.push(element.text);
        else if (element.tag === "img" && typeof element.image_key === "string") imageKeys.push(element.image_key);
      }
      if (parts.length > 0) lines.push(parts.join(""));
    }
    return { text: stripMentions(message, lines.join("\n")), imageKeys };
  }
  return { text: "", imageKeys: [] };
}

/** `/model` forms: list, one id, `default <id>`, `thinking <level>`. */
type ModelCommand =
  | { kind: "list" }
  | { kind: "set"; modelId: string }
  | { kind: "default"; modelId: string }
  | { kind: "thinking"; level: string };

export function parseModelCommand(raw: string): ModelCommand {
  const arg = raw.trim();
  if (arg.length === 0) return { kind: "list" };
  const [head, ...rest] = arg.split(/\s+/);
  const tail = rest.join(" ").trim();
  if (head === "default" && tail.length > 0) return { kind: "default", modelId: tail };
  if (head === "thinking" && tail.length > 0) return { kind: "thinking", level: tail };
  return { kind: "set", modelId: arg };
}

export type ModelCatalogEntry = {
  provider: string;
  id: string;
  thinkingLevels: string[];
};

/**
 * Resolves a model query against the full multi-provider catalog.
 * Accepts either `provider/modelId` (when `provider` matches a real catalog provider, since
 * magpie model ids themselves contain slashes like `auto/glm-5.3`) or a bare `modelId`,
 * preferring `currentProvider` when multiple providers expose the same id.
 */
export function resolveModelPick<T extends ModelCatalogEntry>(
  catalog: readonly T[],
  query: string,
  currentProvider?: string,
): T | undefined {
  const trimmed = query.trim();
  if (trimmed.length === 0) return undefined;
  const slash = trimmed.indexOf("/");
  if (slash > 0) {
    const maybeProvider = trimmed.slice(0, slash);
    const maybeId = trimmed.slice(slash + 1);
    const exact = catalog.find((entry) => entry.provider === maybeProvider && entry.id === maybeId);
    if (exact !== undefined) return exact;
  }
  const byId = catalog.filter((entry) => entry.id === trimmed);
  if (byId.length === 0) return undefined;
  if (currentProvider !== undefined) {
    const sameProvider = byId.find((entry) => entry.provider === currentProvider);
    if (sameProvider !== undefined) return sameProvider;
  }
  return byId[0];
}

/** Picks up to 5 card button choices for `/model`, preferring the conversation's provider. */
export function pickModelChoices<T extends ModelCatalogEntry>(
  catalog: readonly T[],
  currentProvider: string,
  currentModelId: string,
  defaultModelId?: string,
): T[] {
  const scoped = catalog.filter((entry) => entry.provider === currentProvider);
  const pool = scoped.length > 0 ? scoped : catalog;
  const filtered = pool.filter(
    (entry) => entry.provider !== "magpie" || (!entry.id.startsWith("auto/") && !entry.id.startsWith("group/")),
  );
  const byId = new Map<string, T>();
  for (const entry of pool) {
    if (!byId.has(entry.id)) byId.set(entry.id, entry);
  }
  const picks: T[] = [];
  const seen = new Set<string>();
  const push = (entry: T | undefined): void => {
    if (entry === undefined) return;
    const key = `${entry.provider}/${entry.id}`;
    if (seen.has(key)) return;
    seen.add(key);
    picks.push(entry);
  };
  push(byId.get(currentModelId));
  if (defaultModelId !== undefined && defaultModelId.length > 0) push(byId.get(defaultModelId));
  for (const entry of filtered) {
    if (picks.length >= 5) break;
    push(entry);
  }
  return picks.slice(0, 5);
}

/**
 * What the finished card says about files: three names and a count. On a phone
 * this is the one fact the client would otherwise be opened for — "did it actually touch anything,
 * and what". A turn that only talked prints nothing at all.
 */
export function touchedLine(files: string[]): string {
  if (files.length === 0) return "";
  const shown = files.slice(0, 3).map((path) => `\`${path}\``).join(" · ");
  const rest = files.length - 3;
  return `📝 改了 ${files.length} 个文件：${shown}${rest > 0 ? ` …还有 ${rest} 个` : ""}`;
}

/** A quoted message, as the model needs it: who wrote it and what it said. */
type Quoted = { who: string; text: string };

/**
 * Reading a quoted message costs an API round trip, and people quote the same message while
 * answering it. Five minutes is longer than any conversation turn.
 */
const quotedCache = new Map<string, { at: number; value: Quoted | undefined }>();
const QUOTED_TTL_MS = 5 * 60_000;
const QUOTED_CACHE_MAX = 200;

/** The `title` of a merged-forward payload, when there is one. */
function titleOf(content: string): string {
  try {
    const body = JSON.parse(content) as { title?: unknown };
    return typeof body.title === "string" ? body.title.trim().slice(0, 120) : "";
  } catch {
    return "";
  }
}

/** What Feishu hands back instead of a 2.0 card's body when one is read by its message id. */
export const CARD_PLACEHOLDER = "请升级至最新版本客户端";

/** Every `text`/`content` string inside a card JSON, in order — cards are deeply nested. */
function cardText(content: string): string {
  const parts: string[] = [];
  const walk = (value: unknown, depth: number): void => {
    if (depth > 8 || parts.join(" ").length > 1_500) return;
    if (typeof value === "string") return;
    if (Array.isArray(value)) {
      for (const item of value) walk(item, depth + 1);
      return;
    }
    if (typeof value !== "object" || value === null) return;
    for (const [key, item] of Object.entries(value)) {
      if (typeof item === "string" && (key === "text" || key === "content" || key === "title")) {
        const trimmed = item.replace(/\s+/g, " ").trim();
        if (trimmed.length > 0 && !trimmed.includes(CARD_PLACEHOLDER)) parts.push(trimmed);
        continue;
      }
      walk(item, depth + 1);
    }
  };
  try {
    walk(JSON.parse(content), 0);
  } catch {
    return "";
  }
  return parts.join(" ");
}

/** The text of a quoted message, whatever kind it was: the model can act on words, not ids. */
export function quoteText(
  msgType: string,
  content: string,
  mentions: ReadonlyArray<{ key?: string; name?: string; id?: { open_id?: string } }> = [],
): string {
  if (msgType === "image") return "（一张图片）";
  if (msgType === "audio") return "（一条语音）";
  if (msgType === "file" || msgType === "media") return "（一个文件）";
  if (msgType === "sticker") return "（一个表情）";
  // A merged chat record (会话记录) carries a title and nothing readable: the open API hands out the
  // container, not its messages. Say so with the title, so the answer asks for the few lines that
  // matter instead of guessing from a stale topic.
  if (msgType === "merge_forward") {
    const title = titleOf(content);
    return `（合并转发的聊天记录${title.length > 0 ? `：${title}` : ""}。内容是很多条消息的打包，接口读不到，需要对方把关键几行贴成文字）`;
  }
  if (msgType === "interactive") {
    const own = cardText(content);
    // A card read back by id comes home as a placeholder, not as what it says. Saying "no readable
    // text" is honest; pasting "请升级至最新版本客户端" into the model is a red herring.
    return own.length > 0 ? own.slice(0, 1_200) : "（一张卡片，里面没有可读的文字）";
  }
  const parsed = parseContent({ message_type: msgType, content, mentions } as never);
  const text = parsed.text.replace(/\s+/g, " ").trim();
  if (text.length > 0) return text.slice(0, 1_200);
  if (parsed.imageKeys.length > 0) return `（${parsed.imageKeys.length} 张图片）`;
  return "（没有可读的文字）";
}

/**
 * The message a reply is really pointing at, or "" when there is nothing worth reading.
 *
 * Feishu sets `parent_id` to the topic's root on every message posted inside a topic (44 of 46 in
 * the log here), which is not a quote: that text is already in the fork's history, and re-reading
 * it would paste the same paragraph in front of every turn. A quote is a parent that is not the
 * topic root, or any parent of a message outside a topic.
 */
export function quotedParentId(message: {
  parent_id?: string;
  root_id?: string;
  thread_id?: string;
  message_id?: string;
}): string {
  const parent = message.parent_id ?? "";
  if (parent.length === 0 || parent === message.message_id) return "";
  const inTopic = (message.thread_id ?? "").length > 0;
  if (inTopic && parent === (message.root_id ?? "")) return "";
  return parent;
}

/**
 * A topic's human messages, oldest first, as one block. Feishu delivers no event for a message that
 * does not mention the bot, and a topic has no per-message quote to lean on — parent_id there is
 * always the thread root — so without fetching the thread, everything said in it between our turns
 * is invisible to the model.
 */
export function threadDigest(
  items: ReadonlyArray<{
    message_id?: string;
    msg_type?: string;
    create_time?: string;
    body?: { content?: string };
    sender?: { sender_type?: string; sender_name?: string };
    /** The API hands these over with the open_id as a bare string; quoteText wants it one level down. */
    mentions?: Array<{ key?: string; name?: string; id?: string }>;
  }>,
  options: { current: string; max?: number },
): string {
  const lines: string[] = [];
  for (const item of items) {
    const id = item.message_id ?? "";
    if (id.length === 0 || id === options.current) continue;
    // Our own answers are already in the conversation, in full.
    if (item.sender?.sender_type === "app") continue;
    // Without the mentions the text keeps Feishu's "@_user_1" tokens, which name nobody.
    const mentions = (item.mentions ?? []).map((mention) => ({
      key: mention.key,
      name: mention.name,
      id: { open_id: mention.id },
    }));
    const text = quoteText(item.msg_type ?? "", item.body?.content ?? "", mentions)
      .replace(/\s+/g, " ")
      .trim();
    if (text.length === 0) continue;
    const who = (item.sender?.sender_name ?? "").trim();
    lines.push(`${who.length === 0 ? "某人" : who}: ${text.length > 300 ? `${text.slice(0, 300)}…` : text}`);
  }
  const shown = lines.slice(-(options.max ?? 20));
  if (shown.length === 0) return "";
  return `[话题里最近的 ${shown.length} 条发言]\n${shown.join("\n")}\n[话题结束]\n\n`;
}

/** The block the model sees in front of the reply, so "这个" has something to point at. */
export function quoteBlock(quoted: Quoted): string {
  return `[引用 · ${quoted.who} 的消息]\n${quoted.text}\n[引用结束]\n\n`;
}

/**
 * Only the owner may authorize a chat, authorize a person, or answer an approval.
 * An empty allowlist is a misconfiguration, so it trusts nobody: the bridge refuses to start
 * without one, and this stays safe if that check is ever bypassed.
 */
export function isOwnerOperator(openId: string, owners: readonly string[] = config.feishu.allowedSenderIds): boolean {
  return owners.length > 0 && openId.length > 0 && owners.includes(openId);
}

/** The gate in front of every inbound message; explicit for tests, config by default. */
export type Gate = {
  mentionsOnly: boolean;
};

/**
 * Every group turn carries who is speaking, in the message itself. A claim in the instructions
 * ("this turn is the owner") does not survive a transcript of earlier refusals: the model
 * answers "I cannot verify your identity". A bracketed speaker on each message is evidence it
 * can read, turn by turn.
 */
export function speakerTag(isOwner: boolean, senderId: string, name?: string): string {
  const owner = config.feishu.ownerName;
  if (isOwner) return `[${owner} 本人]`;
  return `[群成员 ${name !== undefined && name.length > 0 ? name : senderId.slice(0, 8)}]`;
}

/**
 * Display names by open_id, so a card says 「张三」 rather than 「ou_89cc89」. Needs the
 * contact:user.base:readonly scope: without it Feishu answers 200 with no `name` field, which is
 * why a missing name is cached as a miss instead of retried on every message.
 */
const senderNames = new Map<string, string>();
const senderNamesMissing = new Set<string>();

/** The client the running bot uses; a name lookup must not build a second one per message. */
let sharedClient: Lark.Client | undefined;

function larkClient(): Lark.Client {
  sharedClient ??= new Lark.Client({ appId: config.feishu.appId, appSecret: config.feishu.appSecret });
  return sharedClient;
}

async function senderName(openId: string): Promise<string | undefined> {
  if (openId.length === 0) return undefined;
  const known = senderNames.get(openId);
  if (known !== undefined) return known;
  if (senderNamesMissing.has(openId)) return undefined;
  const response = await larkClient()
    .request({ method: "GET", url: `/open-apis/contact/v3/users/${openId}`, params: { user_id_type: "open_id" } })
    .catch((error: unknown) => {
      logError("feishu", "could not read a sender's name", error);
      return undefined;
    });
  const name = (response?.data as { user?: { name?: string } } | undefined)?.user?.name;
  if (name === undefined || name.length === 0) {
    senderNamesMissing.add(openId);
    return undefined;
  }
  senderNames.set(openId, name);
  return name;
}

/**
 * What a shared group is told. There is one text because there is one tool surface: everyone in
 * the group — owner or not — gets the same tools, and what a colleague is not allowed to see is
 * a memory rule the code enforces, not something a per-speaker prompt needs to re-describe.
 * The paragraph about the old rule stays: the model reads its own earlier refusals in the
 * transcript and repeats them, and saying the rule is dead is what unsticks it.
 */
/**
 * 群里的行为准则。写权限按人分，所以口径也按人分：同一条「群里只读、要改动就找 alex」的话，
 * 说给同事听是保护，说给 alex 听就是在让主人去找主人——他本人在群里发指令时，不该再被告知
 * 「这一步得 alex 来做」。
 */
export function groupInstructions(topic?: string, viewer: "owner" | "member" = "member"): string {
  const owner = config.feishu.ownerName;
  const isOwner = viewer === "owner";
  const lines = [
    "这是一个共享群（群里有其他人）。",
    `- 不要透露 ${owner} 的个人信息、记忆、日程、其他会话内容。`,
    `- 每条消息开头的方括号是网关标注的说话人：「${owner} 本人」就是 ${owner} 的账号，已经校验过，不需要你再判断身份；「群成员」不是。`,
    isOwner
      ? `- 这条消息是 ${owner} 本人发的：写操作照做，不用解释权限。但群里还有别人看着，不要把密钥、.env、记忆库内容贴进群。`
      : `- 群里是只读的：查代码、查 test/prod 的日志和库、跑只读命令都照做，不用解释权限。但改文件（write/edit）、` +
        "bash 里的写命令、看 .env 和密钥，会被当场拒绝——不是卡住等人点，是直接不让做。碰到就一句话说「这一步得 " +
        `${owner} 来做」，然后接着答能答的部分；不要让提问的人自己动手，也不要说「我没有权限」这种内部话。`,
    isOwner
      ? "- 回话群里所有人都看得见：第一句就给结论，不要「我这边的情况如下」这类垫话；步数、token、花费、模型、" +
        "审批这些内部账本也别往里写。"
      : "- 群里回话是给同事看的：第一句就给结论，不要「我这边的情况如下」「首先」这类垫话；不要提步数、token、花费、" +
        "模型、审批、权限这些内部账本和机制——同事看不懂，只会觉得在答非所问；缺信息就一句话说要什么，然后停下等他给。",
    "- 本群的记忆只属于本群：不要提别的会话里的内容，也不要因为「上次群里说过」就当成已知。",
    "- 旧规则已作废：本群以前被设成只读，那条不再适用。不要再回答「这轮是只读的」「bash 在这个群里用不了」，" +
      "也不要让提问的人自己去粘命令或开新会话。",
  ];
  if (topic !== undefined) lines.push(`- ${topic}`);
  return lines.join("\n");
}

/**
 * Is this group message addressed to us? A group talks to itself all day — "@郁斐凡 可以去掉" carries a
 * mention and still names nobody on this side, and answering it turned the bot into a chorus.
 */
export function mentionsUs(
  mentions: Array<{ id?: { open_id?: string } }> | undefined,
  botId: string | undefined = botOpenId,
): boolean {
  if ((mentions ?? []).length === 0) return false;
  // Identity unknown (the bot info call failed): fall back to "somebody was mentioned", which is
  // what this gate did before — silence would be the worse failure of the two.
  if (botId === undefined) return true;
  return (mentions ?? []).some((mention) => mention.id?.open_id === botId);
}

export function rejectionReason(
  event: IncomingMessage,
  parsedContent: ParsedContent,
  level: ChatLevel = "closed",
  isOwner = false,
  policy: Gate = config.feishu,
  botId: string | undefined = botOpenId,
): string | undefined {
  const message = event.message;
  const content = parsedContent;
  const supported =
    message?.message_type === "text" ||
    message?.message_type === "image" ||
    message?.message_type === "post" ||
    message?.message_type === "audio";
  if (!supported) return `unsupported-message-type:${message?.message_type ?? "?"}`;
  if (content.text.length === 0 && content.imageKeys.length === 0 && content.audioKey === undefined) {
    return "empty-content";
  }
  // One level decides everything: closed answers nobody, owner only the owner, group everyone —
  // and a group's members get the same tool surface as the owner.
  if (level === "closed") return "chat-not-allowed";
  if (level === "owner" && !isOwner) return "owner-only";
  if (policy.mentionsOnly && message?.chat_type === "group" && !mentionsUs(message.mentions, botId)) {
    return "group-message-without-mention";
  }
  return undefined;
}

/**
 * Answers belong in a topic: an @ in a group is a person asking one question, and the
 * answer should not push the conversation aside for everyone else. A plain message in a
 * group (only possible once the app may read all group messages) still answers inline.
 * Feishu collapses thread replies behind "N 条回复", which is the trade-off the owner
 * asked for; "always"/"never" override the whole rule.
 */
export function wantsThread(
  message: NonNullable<IncomingMessage["message"]>,
  topicGroup = false,
): boolean {
  const mode = config.feishu.replyInThread;
  if (message.chat_type !== "group" || mode === "never") return false;
  if (mode === "always" || topicGroup) return true;
  const alreadyInThread = message.thread_id !== undefined && message.thread_id !== message.message_id;
  const addressed = (message.mentions ?? []).length > 0;
  return alreadyInThread || addressed;
}

function mentionFor(
  event: IncomingMessage,
  message: NonNullable<IncomingMessage["message"]>,
): string | undefined {
  if (message.chat_type !== "group" || !config.feishu.mentionSender) return undefined;
  return event.sender?.sender_id?.open_id ?? undefined;
}

/** Liveness of the long connection, so a deaf bot is visible instead of silent. */
type FeishuStatus = {
  ready: boolean;
  /** Epoch ms of the last successful connect (or of process start). */
  since: number;
  /** Connect failures since start; a flapping link drops events while it is down. */
  flaps: number;
  lastError?: string;
};

/** One event forwarded by feishu-gw: the envelope's type, id and inner body. */
export type GatewayEvent = { type: "message" | "card"; eventId?: string; eventType?: string; event: unknown };

let gatewayIngress: ((payload: GatewayEvent) => Promise<unknown>) | undefined;

/**
 * One event handed over by feishu-gw, wrapped back into the shape the SDK's dispatcher expects: the
 * handlers, the dedupe and the card answers are untouched — only the transport differs. The answer is
 * returned rather than swallowed because a card action's answer is what Feishu shows.
 */
export async function handleGatewayEvent(payload: GatewayEvent): Promise<unknown> {
  if (gatewayIngress === undefined) {
    return {};
  }
  return await gatewayIngress(payload);
}

const FINAL_PATCH_BACKOFF = [500, 1_500, 4_000];

/**
 * How long to wait before patching the final card again, or `undefined` to give up.
 *
 * A frame lost mid-stream is redrawn a second later by the next one, but the final patch has no
 * follower: if it fails the card stays "thinking" for ever and the answer is only in the log. Most
 * such failures are the gateway being restarted underneath us, which takes seconds — worth waiting
 * out. A card Feishu itself refused ("This message is NOT a card", 230001) will be refused just as
 * firmly next time, so that path goes straight to the fallbacks.
 */
export function patchRetryDelay(attempt: number, error: unknown): number | undefined {
  const message = (error as Error)?.message ?? "";
  if (/^patch failed: \d/.test(message)) return undefined;
  return FINAL_PATCH_BACKOFF[attempt];
}

const feishuStatus: FeishuStatus = { ready: false, since: Date.now(), flaps: 0 };
let feishuStarted = false;

/** Snapshot for the client: how long the bot has been deaf, in ms (0 while connected). */
export const getFeishuStatus = (): FeishuStatus & { started: boolean; downMs: number } => ({
  ...feishuStatus,
  started: feishuStarted,
  downMs: feishuStatus.ready ? 0 : Date.now() - feishuStatus.since,
});

type FeishuBridge = {
  stop(): void;
  /** Boot-time news has no turn to answer, so it goes straight to the owner's chat. */
  notifyOwner(text: string): Promise<void>;
};

export async function startFeishuBridge(runtime: AgentRuntime): Promise<FeishuBridge> {
  const { appId, appSecret } = requireFeishuCredentials();
  // Also here, not only at the entry point: a bridge without an owner answers nobody and would
  // authorize anybody, which is worse than not starting.
  requireFeishuOwner();
  feishuStarted = true;
  // One-time adoption: chats from `.env`, and chats the previous model stored in its two docs.
  // Until this runs, `.env` is the only source, so a fresh process still answers its own chats.
  await adoptChatLevels(runtime.harness, context, config.feishu.allowedChatIds);
  const client = new Lark.Client({ appId, appSecret });
  sharedClient = client;
  // Every Feishu operation goes through feishu-gw over localhost; there is no second transport left
  // to switch to. `client` survives for the calls that were never transport: message history.
  const im: FeishuIm = gatewayClient(gatewayEndpoint());

  // Who we are, so an @ of the bot can be dropped from the text instead of leaving "@".
  await im
    .botOpenId()
    .then((openId) => {
      botOpenId = openId;
      log("feishu", "bot identity", { openId: botOpenId ?? "unknown" });
    })
    .catch((error: unknown) => logError("feishu", "could not read bot identity", error));

  const handled = new Set<string>();

  async function sendText(chatId: string, text: string): Promise<void> {
    await im.message.create({
      params: { receive_id_type: "chat_id" },
      data: { receive_id: chatId, msg_type: "text", content: JSON.stringify({ text }) },
    });
  }

  /**
   * Straight to Feishu, past feishu-gw. The gateway is the half that fails, so a delivery that must
   * survive it — the watchdog's outage notice, an answer whose every other route went through the
   * gateway — uses the SDK client directly.
   */
  async function sendTextDirect(chatId: string, text: string): Promise<void> {
    const response = await larkClient().im.message.create({
      params: { receive_id_type: "chat_id" },
      data: { receive_id: chatId, msg_type: "text", content: JSON.stringify({ text }) },
    });
    if ((response?.code ?? 0) !== 0) throw new Error(response?.msg ?? "direct send refused");
  }

  /**
   * A card when there is no message to reply to — text would show `**bold**` and `|` as they are.
   * Returns the message id: a routine created the card itself, so it has to name it to patch it.
   */
  async function sendMarkdown(
    chatId: string,
    markdown: string,
    meta?: string,
    header?: CardHeader,
    buttons?: CardButton[],
    details?: Omit<TurnDetails, "at">,
  ): Promise<string | undefined> {
    const response = await im.message.create({
      params: { receive_id_type: "chat_id" },
      data: {
        receive_id: chatId,
        msg_type: "interactive",
        content: JSON.stringify(card(markdown, meta, buttons, header, details)),
      },
    });
    const messageId = (response as { data?: { message_id?: unknown } } | undefined)?.data?.message_id;
    if (typeof messageId === "string") void rememberCardText(runtime.harness, context, messageId, markdown);
    return typeof messageId === "string" ? messageId : undefined;
  }

  /**
   * A message for a turn that may be over: reply to what was asked, inside its topic when it had one.
   * Posting to the chat instead put a recovered answer outside the topic, and as a second copy of an
   * answer the live turn had already delivered.
   */
  /** Replies with a card, not text: a recovered answer carries markdown, and as plain text the
   * `**bold**` and `| tables |` arrived unrendered. */
  async function replyOrSend(
    chatId: string,
    messageId: string | undefined,
    thread: boolean | undefined,
    markdown: string,
    meta?: string,
    header?: CardHeader,
    buttons?: CardButton[],
    details?: Omit<TurnDetails, "at">,
  ): Promise<void> {
    if (messageId === undefined) {
      await sendMarkdown(chatId, markdown, meta, header, buttons, details);
      return;
    }
    const response = await im.message.reply({
      path: { message_id: messageId },
      data: {
        msg_type: "interactive",
        content: JSON.stringify(card(markdown, meta, buttons, header, details)),
        ...(thread === true ? { reply_in_thread: true } : {}),
      },
    });
    const sentId = (response as { data?: { message_id?: unknown } } | undefined)?.data?.message_id;
    if (typeof sentId === "string") void rememberCardText(runtime.harness, context, sentId, markdown);
  }

  /** Patch a card to content the caller built itself (a settled approval card keeps its request
   *  body; rebuilding it from markdown would lose the tool block and the frozen buttons). */
  async function patchCardContent(messageId: string, content: string, text: string): Promise<void> {
    if (text.length > 0) void rememberCardText(runtime.harness, context, messageId, text);
    const response = await serializePatch(messageId, () =>
      im.message.patch({ path: { message_id: messageId }, data: { content } }),
    );
    if (response?.code !== 0) {
      throw new Error(`patch failed: ${response?.code} ${response?.msg ?? ""}`);
    }
  }

  async function patchCard(
    messageId: string,
    markdown: string,
    meta?: string,
    buttons?: CardButton[],
    header?: CardHeader,
    details?: Omit<TurnDetails, "at">,
    keepText = true,
  ): Promise<void> {
    const content = JSON.stringify(card(markdown, meta, buttons, header, details));
    // Streaming frames pass `keepText: false`: they are shown for seconds and a doc write per
    // frame is not worth it — the final patch is the one a quote would hit.
    await patchCardContent(messageId, content, keepText ? markdown : "");
  }

  /**
   * Final answer delivery: patch the streaming card, and if that fails for any
   * reason send the answer as a plain message so it is never lost behind a
   * stranded "thinking" card.
   */
  /**
   * A card patch raises no Feishu notification: someone who read "处理中" and put the
   * phone down would never learn the run finished. One short message does.
   */
  async function pingDone(
    chatId: string,
    messageId: string,
    markdown: string,
    options: { thread?: boolean; urgent?: boolean },
  ): Promise<void> {
    // A "finished" note can wait until morning; "the routine failed" cannot, or the owner learns
    // at breakfast that last night's job died. Severity decides unless the caller is explicit.
    const urgent = options.urgent ?? /^[🔴❌⚠️⏸️⏳]/.test(markdown.trim());
    if (!urgent && inQuietHours(config.feishu.quietHours)) {
      log("feishu", "done ping suppressed during quiet hours", { chatId });
      return;
    }
    try {
      await im.message.reply({
        path: { message_id: messageId },
        data: {
          msg_type: "text",
          content: JSON.stringify({ text: markdown }),
          ...(options.thread === true ? { reply_in_thread: true } : {}),
        },
      });
      log("feishu", "done ping sent", { chatId, chars: markdown.length });
    } catch (error) {
      if (isWithdrawnError(error)) {
        log("feishu", "done ping skipped on withdrawn message", { chatId, messageId });
        return;
      }
      logError("feishu", "could not send done ping", error);
    }
  }

  async function deliverAnswer(
    chatId: string,
    cardId: string,
    markdown: string,
    meta: string,
    buttons?: CardButton[],
    header?: CardHeader,
    details?: Omit<TurnDetails, "at">,
    replyContext?: { replyTo?: string; thread?: boolean },
  ): Promise<void> {
    const chunks = splitAnswerForCards(markdown);
    const primary = chunks[0] ?? markdown;
    const primaryButtons = chunks.length === 1 ? buttons : undefined;
    const primaryDetails = chunks.length === 1 ? details : undefined;
    const primaryMeta = chunks.length === 1 ? meta : "";
    let patched = false;
    for (let attempt = 0; ; attempt += 1) {
      try {
        await patchCard(cardId, primary, primaryMeta, primaryButtons, header, primaryDetails);
        log("feishu", "final card patched", { chatId, cardId, chars: markdown.length, chunks: chunks.length, attempts: attempt + 1 });
        patched = true;
        break;
      } catch (error) {
        const delay = patchRetryDelay(attempt, error);
        if (delay === undefined) {
          logError("feishu", `final card patch failed after ${attempt + 1} attempt(s), falling back`, error);
          break;
        }
        log("feishu", "final card patch failed, retrying", {
          attempt: attempt + 1,
          inMs: delay,
          error: (error as Error)?.message ?? String(error),
        });
        await new Promise((resolve) => setTimeout(resolve, delay));
      }
    }
    if (patched) {
      for (let index = 1; index < chunks.length; index += 1) {
        const isLast = index === chunks.length - 1;
        await replyOrSend(
          chatId,
          replyContext?.replyTo ?? cardId,
          replyContext?.thread,
          chunks[index]!,
          isLast ? meta : undefined,
          header,
          isLast ? buttons : undefined,
          isLast ? details : undefined,
        ).catch((error) => logError("feishu", "continuation card failed", error));
      }
      return;
    }
    try {
      for (let index = 0; index < chunks.length; index += 1) {
        const isLast = index === chunks.length - 1;
        await sendMarkdown(
          chatId,
          chunks[index]!,
          isLast ? meta : undefined,
          header,
          isLast ? buttons : undefined,
          isLast ? details : undefined,
        );
      }
      log("feishu", "answer sent as its own card after a failed patch");
    } catch (error) {
      logError("feishu", "card fallback failed, sending text", error);
      try {
        await sendText(chatId, stripInlineCode(markdown));
      } catch (inner) {
        logError("feishu", "plain text fallback failed, sending direct", inner);
        await sendTextDirect(chatId, stripInlineCode(markdown)).catch(async (last: unknown) => {
          logError("feishu", "direct send failed too, holding the answer for retry", last);
          await holdForRetry(runtime.harness, context, {
            chatId,
            text: markdown,
            plain: false,
            meta,
            header,
            ...(buttons === undefined ? {} : { buttons }),
          }).catch(
            (inner) => logError("feishu", "could not hold the answer either", inner),
          );
        });
      }
    }
  }

  async function replyCard(
    messageId: string,
    markdown: string,
    meta?: string,
    options: { thread?: boolean; mention?: string; buttons?: CardButton[]; header?: CardHeader } = {},
  ): Promise<string | undefined> {
    const body = options.mention === undefined ? markdown : `<at id=${options.mention}></at> ${markdown}`;
    const response = await im.message.reply({
      path: { message_id: messageId },
      data: {
        msg_type: "interactive",
        content: JSON.stringify(card(body, meta ?? "▶️ 处理中", options.buttons, options.header)),
        // Group replies stay in one thread: the timeline stays readable and the
        // thread itself becomes the conversation (our fork logic keys on thread_id).
        ...(options.thread === true ? { reply_in_thread: true } : {}),
      },
    });
    return response?.data?.message_id;
  }

  /** A plain-text reply: same thread rule as cards, so a command answer lands where it was typed. */
  async function replyText(
    messageId: string,
    text: string,
    options: { thread?: boolean; mention?: string } = {},
  ): Promise<void> {
    const body = options.mention === undefined ? text : `<at id=${options.mention}></at> ${text}`;
    await im.message.reply({
      path: { message_id: messageId },
      data: {
        msg_type: "text",
        content: JSON.stringify({ text: body }),
        ...(options.thread === true ? { reply_in_thread: true } : {}),
      },
    });
  }

  /**
   * The 👀 mark, added the moment a turn takes the message and removed (or replaced by ✅) when it
   * settles. Best effort by design: a missing scope or a deleted message must not fail a turn that
   * has nothing to do with emoji.
   */
  async function markWorking(messageId: string): Promise<string | undefined> {
    try {
      const response = await im.messageReaction.create({
        path: { message_id: messageId },
        data: { reaction_type: { emoji_type: REACTION_WORKING } },
      });
      return response?.data?.reaction_id as string | undefined;
    } catch (error) {
      if (isWithdrawnError(error)) {
        log("feishu", "could not stamp withdrawn message", { messageId });
        return undefined;
      }
      logError("feishu", "could not stamp the message", error);
      return undefined;
    }
  }

  /**
   * Settling: the tick replaces 👀 only when the turn answered. A failed turn keeps no mark at all —
   * its card already says what went wrong, and a ✅ over a failure is the one lie nobody forgives.
   */
  async function settleMark(messageId: string, reactionId: string, answered: boolean): Promise<void> {
    try {
      await im.messageReaction.delete({ path: { message_id: messageId, reaction_id: reactionId } });
      if (!answered) return;
      await im.messageReaction.create({
        path: { message_id: messageId },
        data: { reaction_type: { emoji_type: REACTION_DONE } },
      });
    } catch (error) {
      if (isWithdrawnError(error)) {
        log("feishu", "could not settle mark on withdrawn message", { messageId });
        return;
      }
      logError("feishu", "could not settle the mark", { messageId, answered, error });
    }
  }

  async function downloadResource(
    messageId: string,
    fileKey: string,
    type: "image" | "file",
  ): Promise<Buffer | undefined> {
    try {
      const resource = (await im.messageResource.get({
        path: { message_id: messageId, file_key: fileKey },
        params: { type },
      })) as unknown as {
        writeFile?: (path: string) => Promise<void>;
        getReadableStream?: () => NodeJS.ReadableStream;
      };
      if (typeof resource?.writeFile === "function") {
        const file = join(tmpdir(), `feishu-${fileKey}`);
        await resource.writeFile(file);
        const bytes = await readFile(file);
        await rm(file, { force: true });
        return bytes;
      }
      if (typeof resource?.getReadableStream === "function") {
        const chunks: Buffer[] = [];
        for await (const chunk of resource.getReadableStream()) chunks.push(Buffer.from(chunk as Buffer));
        return Buffer.concat(chunks);
      }
      logError("feishu", "unexpected resource shape", Object.keys(resource ?? {}).join(","));
    } catch (error) {
      logError("feishu", `resource download failed (${type})`, error);
    }
    return undefined;
  }

  async function downloadImage(
    messageId: string,
    fileKey: string,
  ): Promise<{ data: string; mimeType: string } | undefined> {
    const bytes = await downloadResource(messageId, fileKey, "image");
    if (bytes === undefined) return undefined;
    return { data: bytes.toString("base64"), mimeType: sniff(bytes) };
  }

  /** Does this text carry formatting that plain text would show as literal asterisks and pipes? */
  function noticeIsRich(text: string): boolean {
    return /(\*\*|`|^#|\|)/m.test(text);
  }

  /**
   * Extensions notify with the prose they were written in — a background pipeline reports in
   * markdown, with bold stage names and `code`. Sent as plain text the asterisks show up literally
   * and the structure is gone, so the first line becomes the header and the rest a card body.
   */
  /**
   * A notice is nobody's answer, so how loud it is depends on the chat: `off` drops it, `observe`
   * keeps it out of sight until `/outbox flush`, and `auto` (the default) sends it now. A notice that
   * cannot be delivered waits in the outbox instead of failing a background task that has already
   * done its work.
   */
  async function sendNotice(chatId: string, text: string): Promise<void> {
    const mode = await noticeMode(chatId);
    if (mode === "off") {
      log("feishu", "notice dropped by chat policy", { chatId, chars: text.length });
      return;
    }
    if (mode === "observe") {
      await holdForRetry(runtime.harness, context, { chatId, text, plain: true, paused: true }).catch((inner) =>
        logError("feishu", "could not hold the notice either", inner),
      );
      log("feishu", "notice held by chat policy", { chatId });
      return;
    }
    try {
      await sendNoticeNow(chatId, text);
    } catch (error) {
      logError("feishu", "notice could not be sent, holding it", error);
      await holdForRetry(runtime.harness, context, { chatId, text, plain: true }).catch((inner) =>
        logError("feishu", "could not hold the notice either", inner),
      );
    }
  }

  /** `auto` unless this chat said otherwise; a chat with no conversation yet is `auto`. */
  async function noticeMode(chatId: string): Promise<ProactiveMode> {
    const conversationId = conversationForSurface(chatId);
    if (conversationId === undefined) return "auto";
    return proactiveOf(runtime.harness, context, conversationId).catch(() => "auto" as ProactiveMode);
  }

  async function sendNoticeNow(chatId: string, text: string): Promise<void> {
    const [head = "", ...rest] = text.split("\n");
    const body = rest.join("\n").trim();
    const urgent = /^[🔴⚠️❌]/.test(head.trim());
    if ((body.length === 0 && !urgent) || (!noticeIsRich(body) && !urgent)) {
      await sendText(chatId, text);
      return;
    }
    await im.message.create({
      params: { receive_id_type: "chat_id" },
      data: {
        receive_id: chatId,
        msg_type: "interactive",
        content: JSON.stringify(
          card(
            body.length > 0 ? body : head,
            undefined,
            undefined,
            noticeHeader(head),
          ),
        ),
      },
    });
  }

  /**
   * A routine's turn. Same skeleton as a message turn — card, queue slot, submission, answer in the
   * card — minus everything that only makes sense for a message that arrived: no reply chain, no
   * quote, no thread, no mention, no images, no read-only correction. A routine fires with nobody
   * watching, so the card *is* the answer; a failure has to be visible on it, never only in a log.
   */
  async function runRoutineTurn(chatId: string, instruction: string, key: string): Promise<boolean> {
    if (!chatId.startsWith("oc_") && !chatId.startsWith("on_")) {
      log("feishu", "routine skipped a surface that is not a chat", { surfaceId: chatId });
      return false;
    }
    const conversation = await runtime.conversationFor(chatId);
    const budget = await runtime.budget(chatId);
    if (!budget.allowed) {
      await sendNotice(
        chatId,
        `⏰ 定时任务没跑：今日额度已用完（$${budget.spentToday.toFixed(2)} / $${budget.limit.toFixed(2)}）。`,
      ).catch((error) => logError("feishu", "could not report an exhausted budget", error));
      return false;
    }

    const startedAt = Date.now();
    const digest =
      instruction
        .split("\n")
        .map((line) => line.trim())
        .find((line) => line.length > 0 && !line.startsWith("⏰")) ?? "定时任务";
    const cardId = await sendMarkdown(
      chatId,
      `> ${digest.slice(0, 240)}\n\n⏳ 定时任务开跑…`,
      undefined,
      { title: "⏰ 定时任务", template: "blue" },
    );
    if (cardId === undefined) {
      log("feishu", "routine card creation failed, skipping the turn", { chatId });
      return false;
    }
    log("feishu", "routine card created", { chatId, cardId, key });

    const card = liveCard(conversation, cardId, digest, startedAt);
    card.stall(300_000);
    const slot = enqueueTurn(conversation.id as unknown as number);
    if (slot.queued) {
      card.setStatus("⏸️ 排队中 · 上一条还在跑");
      await slot.wait;
      if (slot.cancelled()) {
        // Dropped on purpose: a routine has a next period, so skipping this fire loses nothing.
        slot.done();
        await card.finish();
        await deliverAnswer(
          chatId,
          cardId,
          "这一轮定时任务在排队时被撤掉了，下个周期照常。",
          "🗑️ 撤销",
          undefined,
          { template: "grey", title: "🗑️ 已撤销" },
        ).catch(() => undefined);
        return false;
      }
      card.clearStatus();
    }

    try {
      // The key carries the scheduled instant, so a fire retried after a crash dedupes instead of
      // running the same routine twice.
      const turnAgent = await conversation.agent(context).catch(() => undefined);
      const workDir = turnAgent?.cwd ?? config.workspace;
      const treeMark = await markTree(workDir).catch(() => undefined);
      const submission = await conversation.submit({ type: "input", content: instruction, requestId: key }, context);
      await card.start();
      const settled = await submission.wait(context);
      const seconds = Math.round((Date.now() - startedAt) / 100) / 10;
      log("feishu", "routine turn settled", {
        chatId,
        status: settled.status,
        reason: settled.reason ?? null,
        seconds,
      });
      const decisions = takeDecisions(String(conversation.id));
      const claim = takeDone(String(conversation.id));
      if (claim !== undefined) {
        decisions.push({
          at: claim.at,
          kind: "stop",
          text: `声明：${claim.met ? "目标达成" : "目标未达成"} — ${claim.why}`,
        });
        // A routine's outcome belongs in the morning digest like any other turn: it is the only
        // place the owner finds out an unattended job has been quietly failing.
        await recordVerdict(runtime.harness, context, {
          chat: chatId,
          met: claim.met,
          why: claim.why,
          seconds,
          ...(settled.reason === undefined ? {} : { reason: settled.reason }),
        });
      }
      if (settled.reason !== undefined) {
        decisions.push({ at: Date.now(), kind: "stop", text: `结束：${settled.reason}` });
      }
      await card.finish();

      if (settled.status === "done" && settled.answer !== undefined) {
        const answer = await runtime.answerText(conversation, settled.answer, settled.entry);
        const blocks = await runtime
          .answerBlocks(conversation, settled.answer, settled.entry)
          .catch(() => [] as AnswerBlock[]);
        const text = blocks.length > 0 ? composeAnswer(blocks) : answer || "（模型没有返回文本）";
        const touched = treeMark === undefined ? [] : await touchedSince(workDir, treeMark).catch(() => []);
        // One line under the card, and only when there is something to say: what changed on disk.
        // The tool count and the clock said nothing the reader could act on.
        const meta = touchedLine(touched);
        await deliverAnswer(
          chatId,
          cardId,
          text,
          meta,
          undefined,
          claim === undefined ? { template: "green", title: "✅ 定时任务跑完了" } : doneVerdict(claim),
          decisions.length === 0 ? undefined : { steps: [], seconds, costUsd: 0, decisions },
        );
        if (claim?.met === false) {
          await pingDone(chatId, cardId, `⚠️ 定时任务目标未达成 · ${seconds}s`, {
            thread: topicChats.get(chatId) === true,
          });
        } else if (shouldPingDone(config.feishu.donePingSeconds, seconds * 1000)) {
          await pingDone(chatId, cardId, `✅ 定时任务跑完了 · ${seconds}s`, {
            thread: topicChats.get(chatId) === true,
          });
        }
        return true;
      }
      await deliverAnswer(
        chatId,
        cardId,
        `这轮没跑完：${stopReason(settled.reason)}`,
        "",
        undefined,
        { template: "orange", title: "⚠️ 定时任务没跑完" },
        decisions.length === 0 ? undefined : { steps: [], seconds, costUsd: 0, decisions },
      );
      await pingDone(chatId, cardId, `⚠️ 定时任务没跑完（${stopReason(settled.reason)}） · ${seconds}s`, {
        thread: topicChats.get(chatId) === true,
      });
      return false;
    } catch (error) {
      logError("feishu", "routine turn failed", error);
      await deliverAnswer(
        chatId,
        cardId,
        `定时任务出错：${(error as Error)?.message ?? String(error)}`,
        "",
        undefined,
        { template: "red", title: "❌ 定时任务失败" },
      ).catch(() => undefined);
      await pingDone(chatId, cardId, `❌ 定时任务失败：${(error as Error)?.message ?? String(error)}`.slice(0, 140), {
        thread: topicChats.get(chatId) === true,
      });
      return false;
    } finally {
      slot.done();
    }
  }

  /**
   * One outbox entry on its way. A card by default: the held text is markdown, and as plain text its
   * tables and bold arrive as characters.
   */
  async function sendHeld(entry: OutboxEntry): Promise<void> {
    if (entry.patchCardId !== undefined) {
      await patchCardContent(entry.patchCardId, entry.text, entry.meta ?? "");
      log("feishu", "held card patch delivered", { chatId: entry.chatId, cardId: entry.patchCardId, attempts: entry.attempts });
      return;
    }
    const content = entry.plain
      ? JSON.stringify({ text: entry.text })
      : JSON.stringify(card(entry.text, entry.meta, entry.buttons, entry.header));
    const msgType = entry.plain ? "text" : "interactive";
    const response =
      entry.replyTo === undefined
        ? await im.message.create({
            params: { receive_id_type: "chat_id" },
            data: { receive_id: entry.chatId, msg_type: msgType, content },
          })
        : await im.message.reply({
            path: { message_id: entry.replyTo },
            data: { msg_type: msgType, content, ...(entry.thread === true ? { reply_in_thread: true } : {}) },
          });
    if ((response?.code ?? 0) !== 0) {
      throw new Error(`outbox send refused: ${response?.code} ${response?.msg ?? ""}`);
    }
    if (!entry.plain) log("feishu", "held message delivered", { chatId: entry.chatId, attempts: entry.attempts });
  }

  // The outbox keeps what the fallback chain could not deliver. Fifteen seconds, because the usual
  // cause is a gateway restart and nobody wants to wait a minute for a retried answer.
  let draining = false;
  const outboxTimer = setInterval(() => {
    if (draining) return;
    draining = true;
    void drainOutbox(runtime.harness, context, sendHeld)
      .then((report) => {
        if (report.sent > 0) log("feishu", "outbox drained", { sent: report.sent, left: report.left });
        for (const entry of report.gaveUp) {
          // Kept in the dead letters, so this is a pointer, not the only record of the loss.
          logError(
            "feishu",
            "outbox gave up on a message",
            new Error(
              `${entry.chatId} after ${entry.attempts} attempt(s): ${entry.lastError ?? "unknown"} — kept as ${entry.id} (durable outbox retry ${entry.id})`,
            ),
          );
        }
      })
      .catch((error) => logError("feishu", "outbox drain failed", error))
      .finally(() => {
        draining = false;
      });
  }, 15_000);
  outboxTimer.unref?.();

  // The extensions reach back through these two hooks.
  setNotifier(async (surfaceId, text) => {
    // Only a Feishu chat can receive a message here; other surfaces (cli, client) are
    // handled by their own notifier, and asking Feishu for one returns 230001.
    if (!surfaceId.startsWith("oc_") && !surfaceId.startsWith("on_")) {
      log("feishu", "notifier skipped a surface that is not a chat", { surfaceId });
      return;
    }
    await sendNotice(surfaceId, text);
  });
  /** Active live cards keyed by conversationId so approvals can show waiting status on the main card. */
  const activeLiveCards = new Map<number, { setStatus(text: string): void; clearStatus(): void }>();

  // A routine knocks on the same door a message does; it just has no message behind it.
  setTurnStarter(runRoutineTurn);
  setApprovalDecider(async (request) => {
    // Include the source chat/group title on the DM card so parallel chats are easy to tell apart.
    const surfaceInfo =
      request.chatTitle ??
      (request.surfaceId.length > 0
        ? (
            await runtime.harness
              .snapshot(SurfaceMetaDoc, context)
              .catch((error) => {
                logError("feishu", "could not read surface meta for approval", error);
                return undefined;
              })
          )?.surfaces[request.surfaceId]?.title
        : undefined);
    const enriched: ApprovalRequest = surfaceInfo === undefined ? request : { ...request, chatTitle: surfaceInfo };
    // The owner answers in their own DM. A card in the group would show everyone what is about
    // to be deleted, and a group member could not answer it anyway.
    const ownerChat = await ownerChatId(runtime.harness, context).catch(() => undefined);
    const target = ownerChat ?? request.surfaceId;
    const sent = target.startsWith("oc_")
      ? await im.message
          .create({
            params: { receive_id_type: "chat_id" },
            data: {
              receive_id: target,
              msg_type: "interactive",
              content: JSON.stringify(approvalCard(enriched)),
            },
          })
          // A surface that is not a Feishu chat (cli, client) gets no card: the
          // approval still shows up in the registry for the client to answer.
          .catch((error) => {
            logError("feishu", "could not send approval card", error);
            return undefined;
          })
      : undefined;
    if (ownerChat === undefined) {
      // The owner's DM is learned from their first message; until then the card has to go where
      // the call came from, and the owner needs to know how to fix that.
      await sendText(
        request.surfaceId,
        `⚠️ 需要 ${config.feishu.ownerName} 确认才能继续（${request.tool}）。我还没记住 ${config.feishu.ownerName} 的私聊会话：请 ${config.feishu.ownerName} 在私聊里给我发一句任何话。`,
      ).catch(() => {});
    }
    const cardId = sent?.data?.message_id;
    if (target.startsWith("oc_") && cardId === undefined) {
      // A card that never arrived is silence: the call sits here until the timeout denies it and
      // nobody ever knew there was a question. Say it in plain text instead — AskHuman falls back
      // the same way when Feishu answers Internal Error.
      await sendText(
        target,
        `⚠️ 需要你确认（确认卡片没发出去）：${request.tool} — ${request.reason}\n${Math.round(request.timeoutMs / 1000)}s 内没处理就按拒绝。可以在桌面客户端回答。`,
      ).catch((error) => logError("feishu", "could not send the approval fallback either", error));
    }
    log("feishu", "approval card sent", { cardId, callId: request.callId.slice(-12), tool: request.tool });
    activeLiveCards.get(request.conversationId)?.setStatus(`⏸️ 等待 ${config.feishu.ownerName} 确认（${request.tool}）…`);
    // The turn's card should say it stopped to ask, not just lose two minutes with no explanation.
    const askedAt = Date.now();
    const trailKey = String(request.conversationId);
    noteDecision(trailKey, { kind: "approval", text: `问你：${request.tool} — ${request.summary}` }, askedAt);
    return new Promise<boolean>((resolve) => {
      // The registry owns the pending state and the timeout; this surface only says what its card
      // should look like once the decision lands, whoever made it. Keeping a second map here is
      // what let "still pending" be true in one place and false in the other.
      registerApproval({
        callId: request.callId,
        conversationId: request.conversationId,
        grantKey: request.grantKey,
        tool: request.tool,
        summary: request.summary,
        reason: request.reason,
        surfaceId: request.surfaceId,
        timeoutMs: request.timeoutMs,
        onSettled: ({ approved, by, pick }) => {
          activeLiveCards.get(request.conversationId)?.clearStatus();
          noteDecision(trailKey, {
            kind: "approval",
            text:
              by === "timeout"
                ? `超时未确认，${request.tool} 已拒绝 · 等了 ${elapsed(request.timeoutMs)}`
                : `你${approved ? "允许" : "拒绝"}了 ${request.tool}（${by === "client" ? "客户端" : "飞书"}）· 等了 ${elapsed(Date.now() - askedAt)}`,
          });
          if (cardId === undefined) return;
          const who: ApprovalOutcome["by"] =
            by === "client" ? "client" : by === "timeout" ? "timeout" : by === "shutdown" ? "shutdown" : "feishu";
          const outcome: ApprovalOutcome = {
            ok: approved,
            by: who,
            pick,
            waitedMs: who === "timeout" || who === "shutdown" ? request.timeoutMs : Date.now() - askedAt,
          };
          const verdict = approvalVerdict(enriched, outcome);
          // The card is frozen, not rewritten: the request it asked about, the answer that was
          // used, and four disabled buttons. A reader scrolling back still sees what was allowed.
          const frozenPayload = JSON.stringify(approvalCard(enriched, outcome));
          const frozenText = `${verdict.title}\n\n${request.tool} — ${request.reason}\n\n${verdict.line}`;
          void patchCardContent(cardId, frozenPayload, frozenText).catch((error) => {
            logError("feishu", "could not patch approval card, queuing outbox retry", error);
            if (target.startsWith("oc_")) {
              void holdForRetry(runtime.harness, context, {
                chatId: target,
                patchCardId: cardId,
                text: frozenPayload,
                meta: frozenText,
                plain: false,
              }).catch((inner) => logError("feishu", "could not queue approval patch retry", inner));
            }
          });
        },
        resolve: (approved) => resolve(approved),
      });
    });
  });

  /**
   * The living half of a card a run is filling: the step trail, the clock in the subtitle, and the
   * patches that carry them. Both the message path and the continue button own one — the continue
   * button used to patch "继续中…" once and then sit blank for seven minutes, because the only
   * thing that knew how to draw progress was inlined in the other path.
   */
  function liveCard(
    conversation: Awaited<ReturnType<AgentRuntime["conversationFor"]>>,
    cardId: string,
    digest: string,
    startedAt: number,
  ) {
    // The card's clock: seconds since the turn started, the same number /status would show.
    const age = (): string => `${Math.round((Date.now() - startedAt) / 1000)}s`;
    let phase: CardPhase = "live";
    let last: LiveSnapshot | undefined;
    let trail: Trail = { rounds: [], since: Date.now() };
    // A subtitle someone else is writing: queued, stalled. Empty means "show the working line".
    let status = "";
    let timer: ReturnType<typeof setInterval> | undefined;
    let watchdog: ReturnType<typeof setTimeout> | undefined;
    let watcher: Awaited<ReturnType<typeof watchLive>> | undefined;
    let streamErrors = 0;
    let finished = false;
    const tools = new Set<string>();

    // Which stage of a pipeline is running, if one is. The work happens in child conversations, so
    // the card asks the store — but not on every patch: a token-by-token stream would open a
    // connection per token.
    let stages: string[] = [];
    let stagesAt = 0;
    // `bash` five times in a row is what a turn here actually looks like, and the live document
    // names the tool but not what it ran: the arguments are in the transcript, keyed by call id.
    // One lookup per call id, remembered for the turn — the card is painted far more often than
    // calls are made.
    const hints = new Map<string, string>();
    const hintFor = (live: LiveSnapshot): string | undefined => {
      const callIds = live.callIds ?? [];
      const missing = callIds.filter((id) => !hints.has(id));
      if (missing.length > 0) {
        let found: Record<string, string> = {};
        try {
          found = callHints(config.dbPath, conversation.id as unknown as number, missing);
        } catch {
          // A card that cannot read the arguments is still a card; it just says `bash`.
        }
        for (const id of missing) {
          // Remember the miss too: a call id whose arguments are not in the transcript yet must
          // not be looked up on every patch.
          hints.set(id, found[id] ?? "");
        }
      }
      // Only when the round is one call: two calls in a round are two commands, and putting the
      // first one's arguments next to both names would mislabel the second.
      if (callIds.length !== 1) return undefined;
      const hint = hints.get(callIds[0] as string) ?? "";
      return hint.length === 0 ? undefined : hint;
    };
    const refreshStages = (): void => {
      const now = Date.now();
      if (now - stagesAt < 2_000) return;
      stagesAt = now;
      stages = subAgentStagesSince(config.dbPath, conversation.id as unknown as number, startedAt)
        .filter((agent) => agent.state !== "pending")
        .slice(-5)
        .map((agent) => stageLine(agent, now));
    };
    const body = (): string => {
      if (last !== undefined) return renderLive(last, trail, stages);
      return digest.length === 0 ? "🤔 思考中…" : `> ${digest}\n\n🤔 思考中…`;
    };
    // How far the turn got, in the subtitle: "第 3 步 · 2m18s" beats a bare clock, and it replaces
    // the rows of finished steps the body used to stack. The header already says 处理中, so the
    // subtitle does not say it a second time.
    const working = (): string => {
      const { done, running } = stepCount(trail);
      if (running) return `▶️ 第 ${done + 1} 步 · ${age()}`;
      if (done === 0) return `▶️ ${age()}`;
      return `▶️ ${done} 步 · ${age()}`;
    };
    // Stopping used to mean finding the input box and typing /stop, on a phone, while the run kept
    // going. The card is where the progress lives, so the way to stop lives there too.
    const stopButtons = (): CardButton[] => [
      { label: "停止", value: { kind: "stop", conversationId: conversation.id as unknown as number }, type: "danger" },
    ];
    // Feishu redraws the whole card on every patch and a token is not a frame: a fast model used to
    // re-upload the card several times a second, which is where the `2200` stream errors came from.
    // One frame per 300ms looks the same to a reader, and the frame that is queued is always the
    // newest one — the last thing said still ends up on the card.
    const PAINT_MIN_MS = 300;
    let paintedAt = 0;
    let lastFrame = "";
    let pendingFrame: ReturnType<typeof setTimeout> | undefined;
    const flushFrame = (): void => {
      pendingFrame = undefined;
      if (phase !== "live") return;
      refreshStages();
      const text = body();
      const subtitle = status === "" ? working() : status;
      const frame = `${text}\u0000${subtitle}`;
      // Nothing new: the 20s tick and a commit that changed no visible word do not need a round trip.
      if (frame === lastFrame) return;
      lastFrame = frame;
      paintedAt = Date.now();
      void patchCard(cardId, text, subtitle, stopButtons(), WORKING_HEADER, undefined, false).catch((error) => {
        if (streamErrors < 3) {
          streamErrors += 1;
          logError("feishu", "stream patch failed", error);
        }
      });
    };
    const paint = (): void => {
      if (phase !== "live") return;
      const wait = PAINT_MIN_MS - (Date.now() - paintedAt);
      if (wait <= 0) {
        flushFrame();
        return;
      }
      if (pendingFrame === undefined) {
        pendingFrame = setTimeout(flushFrame, wait);
        pendingFrame.unref?.();
      }
    };

    const controls = {
      setStatus(text: string): void {
        status = text;
        paint();
      },
      clearStatus(): void {
        status = "";
        paint();
      },
    };
    const conversationIdNum = conversation.id as unknown as number;

    return {
      /** Open the tap: steps land as they happen, and the clock ticks even when nothing does. */
      async start(): Promise<void> {
        activeLiveCards.set(conversationIdNum, controls);
        watcher = await watchLive(conversation, (live) => {
          if (phase !== "live") return;
          last = live;
          trail = advance(trail, live, Date.now(), hintFor(live));
          for (const tool of live.tools) tools.add(tool);
          paint();
        });
        // A silent model or a long tool round writes no commits, so tick the card ourselves; the
        // body is re-rendered from the last snapshot because the running step's clock lives there.
        timer = setInterval(paint, 20_000);
      },
      /** Say something instead of the working line — queued, stalled — until it is cleared. */
      setStatus: controls.setStatus,
      clearStatus: controls.clearStatus,
      /** Warn once if the run looks stuck, without cutting it. */
      stall(afterMs: number): void {
        watchdog = setTimeout(() => {
          status = `⏳ ${age()} · 超过 ${Math.round(afterMs / 60_000)} 分钟，点「停止」中断`;
          paint();
        }, afterMs);
      },
      tools,
      /** The run settled: freeze the card so nothing rewrites it under the answer. */
      async finish(): Promise<void> {
        if (finished) return;
        finished = true;
        phase = "delivering";
        if (activeLiveCards.get(conversationIdNum) === controls) activeLiveCards.delete(conversationIdNum);
        if (timer !== undefined) clearInterval(timer);
        if (watchdog !== undefined) clearTimeout(watchdog);
        if (pendingFrame !== undefined) clearTimeout(pendingFrame);
        await watcher?.stop();
      },
    };
  }

/** Cards with a continue already running, so a double tap cannot start two turns at once. */
const continuing = new Set<string>();

  /**
   * The 「继续」 button under a card that did not finish. Submitted with no new user message, so it
   * replays the same context, and the answer lands in the card that asked for it: the turn reads as
   * one answer instead of a second card after the first.
   */
  /** Renders the answer via structured blocks when available so tool steps stay folded consistently. */
  async function renderAnswerBody(
    conversation: Awaited<ReturnType<AgentRuntime["conversationFor"]>>,
    answerId: Parameters<AgentRuntime["answerBlocks"]>[1],
    fromId?: Parameters<AgentRuntime["answerBlocks"]>[2],
  ): Promise<string> {
    const blocks = await runtime.answerBlocks(conversation, answerId, fromId).catch(() => [] as AnswerBlock[]);
    if (blocks.length > 0) return composeAnswer(blocks);
    return (await runtime.answerText(conversation, answerId, fromId)) || "（模型没有返回文本）";
  }

  async function continueFromCard(chatId: string, cardId: string, messageId: string): Promise<void> {
    if (continuing.has(cardId)) return;
    continuing.add(cardId);
    const startedAt = Date.now();
    let card: ReturnType<typeof liveCard> | undefined;
    try {
      // The index knows which conversation answered this message; a topic's fork is not the chat's
      // own conversation, so a missing entry is the only case that falls back to the chat.
      const hit = await lookupMessage(runtime.harness, context, messageId).catch(() => undefined);
      const indexed =
        hit === undefined ? undefined : await runtime.harness.conversation(hit.conversationId as never, context);
      const conversation = indexed ?? (await runtime.conversationFor(chatId));
      // Same living card as a fresh turn: resuming an 80-step run behind a frozen "继续中…" hides the
      // seven minutes it takes, which is exactly when someone is watching for progress.
      card = liveCard(conversation, cardId, "", startedAt);
      card.stall(300_000);
      await card.start();
      const submission = await conversation.submit(
        { type: "continue", requestId: `continue-card:${cardId}:${startedAt}` },
        context,
      );
      const settled = await submission.wait(context);
      await card.finish();
      const seconds = Math.round((Date.now() - startedAt) / 100) / 10;
      log("feishu", "card continue settled", { status: settled.status, seconds });
      const threads = await runtime.threads().catch(() => ({} as Record<string, { key?: string }>));
      const inThread = topicChats.get(chatId) === true || threads[String(conversation.id)] !== undefined;
      if (settled.status === "done" && settled.answer !== undefined) {
        const answer = await renderAnswerBody(conversation, settled.answer, settled.entry);
        await deliverAnswer(
          chatId,
          cardId,
          answer,
          `✅ 接着答完了 · ${seconds}s`,
          undefined,
          { template: "green", title: "✅ 答完了" },
          undefined,
          { replyTo: messageId || cardId, thread: inThread },
        );
        if (shouldPingDone(config.feishu.donePingSeconds, seconds * 1000)) {
          await pingDone(chatId, messageId || cardId, `✅ 接着答完了 · ${seconds}s`, { thread: inThread });
        }
      } else {
        await deliverAnswer(
          chatId,
          cardId,
          `⚠️ 接着跑也没收尾：${settled.reason ?? "unanswered"}`,
          `⏸️ ${seconds}s`,
          continueButton(messageId),
          { template: "orange", title: "⏸️ 还是没收尾" },
          undefined,
          { replyTo: messageId || cardId, thread: inThread },
        );
        if (shouldPingDone(config.feishu.donePingSeconds, seconds * 1000)) {
          await pingDone(chatId, messageId || cardId, `⏸️ 还是没收尾 · ${seconds}s`, { thread: inThread });
        }
      }
    } catch (error) {
      logError("feishu", "card continue failed", error);
      await deliverAnswer(
        chatId,
        cardId,
        `❌ 继续失败：${(error as Error).message}`,
        "⏹️ 失败",
        continueButton(messageId),
        { template: "red", title: "❌ 出错了" },
      );
    } finally {
      // An error still has to stop the clock: a live card keeps patching and would paint over
      // the failure answer a moment after it lands.
      await card?.finish();
      continuing.delete(cardId);
    }
  }

  async function handleCardAction(payload: CardAction): Promise<Record<string, unknown>> {
    const receivedAt = Date.now();
    const value = payload.action?.value ?? {};

    const ownerName = config.feishu.ownerName;
    if (value["kind"] === "set-model") {
      const operator = payload.operator?.open_id ?? "";
      if (!isOwnerOperator(operator)) return { toast: { type: "warning", content: `只有 ${ownerName} 能换模型` } };
      const target = Number(value["conversationId"]);
      const provider = typeof value["provider"] === "string" ? value["provider"] : config.model.provider;
      const modelId = typeof value["modelId"] === "string" ? value["modelId"] : "";
      if (!Number.isFinite(target) || modelId.length === 0) return { toast: { type: "error", content: "缺少模型或会话" } };
      const conversation = await runtime.harness
        .conversation(target as never, context)
        .catch(() => undefined);
      if (conversation === undefined) return { toast: { type: "error", content: "会话不在了" } };
      await runtime.setModel(conversation, { provider, modelId });
      log("feishu", "model switched from a card", { conversationId: target, provider, modelId });
      return { toast: { type: "success", content: `已切到 ${modelId}（下一轮生效）` } };
    }

    if (value["kind"] === "open-chat") {
      const operator = payload.operator?.open_id ?? "";
      if (!isOwnerOperator(operator)) {
        return { toast: { type: "warning", content: `只有 ${ownerName} 可以授权` } };
      }
      const chatId = typeof value["chatId"] === "string" ? value["chatId"] : "";
      if (chatId.length === 0) return { toast: { type: "error", content: "缺少会话 id" } };
      // One button, one level: the chat becomes a group — everyone in it gets the same tools as
      // the owner — and isolation keeps the owner's memory out of it.
      await setChatLevel(runtime.harness, context, chatId, "group");
      const conversation = await runtime.conversationFor(chatId);
      await setIsolatedPolicy(runtime.harness, context, conversation.id as unknown as number).catch(() => {});
      // A shared group does not need the owner's 16k of headroom: compacting earlier keeps every
      // group turn cheaper, and the transcript it summarizes is public to the group anyway.
      await conversation
        .configure({ compaction: { reserveTokens: 8_192 } }, context)
        .catch((error: unknown) => logError("feishu", "could not set group compaction", error));
      void sendText(chatId, `已开通：可以问我了。查代码、查库、查日志都能做；改文件、写库、看密钥只有 ${ownerName} 能做，会被直接拒。`).catch(() => {});
      const messageId = payload.context?.open_message_id;
      if (messageId !== undefined) {
        void patchCard(messageId, `✅ 已开放 ${chatId.slice(-6)}`, `所有人都能用：查代码 / 库 / 日志；写操作只有 ${ownerName} 能做`).catch(
          () => {},
        );
      }
      return { toast: { type: "success", content: "已开放，并通知了对方" } };
    }

    if (value["kind"] === "stop") {
      const operator = payload.operator?.open_id ?? "";
      if (!isOwnerOperator(operator)) return { toast: { type: "warning", content: `只有 ${ownerName} 能停` } };
      const target = Number(value["conversationId"]);
      if (!Number.isFinite(target)) return { toast: { type: "error", content: "缺少会话" } };
      const stopped = await runtime.harness.conversation(target as never, context).catch(() => undefined);
      if (stopped === undefined) return { toast: { type: "error", content: "会话不在了" } };
      // Abort is a request, not an outcome: the run unwinds on its own clock and the settle path
      // repaints the card as 已中断. A message still in line is taken out instead — see
      // stopConversation for why the stop lands on the waiting one.
      const outcome = await stopConversation(runtime, target).catch((error: unknown) => {
        logError("feishu", "card stop failed", error);
        return { cancelled: 0, aborted: false };
      });
      log("feishu", "stop pressed on a card", { conversationId: target, ...outcome });
      return {
        toast: {
          type: "success",
          content:
            outcome.cancelled > 0
              ? `已撤销排队的 ${outcome.cancelled} 条，前面那轮继续跑`
              : "已按停止，正在收尾",
        },
      };
    }

    if (value["kind"] === "continue") {
      const messageId = typeof value["messageId"] === "string" ? value["messageId"] : "";
      const chatId = payload.context?.open_chat_id ?? "";
      const cardId = payload.context?.open_message_id ?? "";
      if (chatId.length === 0 || cardId.length === 0) {
        return { toast: { type: "error", content: "缺少会话信息" } };
      }
      // A continue runs for minutes and Feishu drops a callback after ~3s: answer now, work later.
      void continueFromCard(chatId, cardId, messageId);
      return { toast: { type: "success", content: "继续中…" } };
    }

    // Answers now carry the panel themselves; this only serves buttons on cards sent before that.
    if (value["kind"] === "detail") {
      const messageId = typeof value["messageId"] === "string" ? value["messageId"] : "";
      const chatId = payload.context?.open_chat_id ?? "";
      const details = await readTurnDetails(runtime.harness, context, messageId);
      if (details === undefined) return { toast: { type: "info", content: "这轮没有工具调用记录" } };
      if (chatId.length > 0) {
        // Fire and forget: the callback response has a ~3s budget.
        void im.message
          .create({
            params: { receive_id_type: "chat_id" },
            data: {
              receive_id: chatId,
              msg_type: "interactive",
              content: JSON.stringify(card(renderDetails(details), "🔍 本轮执行详情")),
            },
          })
          .catch((error) => logError("feishu", "could not send detail card", error));
      }
      return { toast: { type: "success", content: "详情已发送" } };
    }

    const callId = typeof value["approval"] === "string" ? value["approval"] : "";
    const operator = payload.operator?.open_id ?? "";
    const entry = getApproval(callId);
    log("feishu", "card action received", {
      callId: callId.slice(-12),
      operator: operator.slice(-6),
      ok: value["ok"],
      pending: entry !== undefined,
      pendingIds: pendingCount(),
      messageId: payload.context?.open_message_id,
    });
    if (entry === undefined) return { toast: { type: "info", content: "已处理" } };

    if (!isOwnerOperator(operator)) return { toast: { type: "warning", content: `只有 ${ownerName} 可以确认` } };

    const approved = value["ok"] === true;
    // `all` is the blanket answer: one grant that covers every risk class in this
    // conversation except the ones that still ask (sudo, rm -rf, …).
    const blanket = value["all"] === true;
    const grantedKey = blanket ? BLANKET : value["key"];
    if (approved && (blanket || value["session"] === true) && typeof grantedKey === "string" && entry.conversationId !== undefined) {
      void runtime.grantSession(entry.conversationId, grantedKey).catch((error) =>
        logError("feishu", "could not store session grant", error),
      );
    }
    // The registry decides and tells every surface — this card included, through onSettled. A false
    // therefore means another surface answered in between: this click was simply late.
    if (!resolveApproval(callId, approved, `feishu:${operator.slice(-6)}`, approved ? (blanket ? "all" : value["session"] === true ? "session" : "allow") : "deny")) {
      return { toast: { type: "info", content: "已经处理过了" } };
    }
    log("feishu", "approval answered", { approved, callId: callId.slice(-12), ms: Date.now() - receivedAt });
    void record(runtime.harness, context, {
      kind: "approve",
      who: operator.slice(-6),
      tool: entry.tool,
      chat: entry.surfaceId,
      detail: approved ? "approved" : "denied",
    });
    return { toast: { type: "success", content: approved ? "已允许，继续执行" : "已拒绝" } };
  }

  /**
   * Someone the bot has never met is talking to it. Silence would look like a broken bot, so
   * the owner gets one card with one button: open this chat to the people in it.
   */
  async function notifyOwnerOfStranger(
    event: IncomingMessage,
    message: NonNullable<IncomingMessage["message"]>,
    text: string,
  ): Promise<void> {
    const ownerChat = await ownerChatId(runtime.harness, context).catch(() => undefined);
    if (ownerChat === undefined) return;
    const senderId = event.sender?.sender_id?.open_id ?? "?";
    const where = message.chat_type === "p2p" ? "私聊" : "群聊";
    const headline = message.chat_type === "p2p" ? "有人私聊 bot" : "有未授权的群在 @ bot";
    const body = text.replace(/[\n`]/g, " ").slice(0, 200);
    const cardBody =
      `**${headline}**\n` +
      `- 来源：${where}\n` +
      `- 会话：\`${message.chat_id}\`\n` +
      `- 发送者：\`${senderId}\`\n` +
      `- 内容：${body.length > 0 ? body : "（非文字消息）"}\n`;
    const buttons: CardButton[] = [
      { label: "开放此会话", value: { kind: "open-chat", chatId: message.chat_id ?? "" }, type: "primary" },
    ];
    try {
      await im.message.create({
        params: { receive_id_type: "chat_id" },
        data: {
          receive_id: ownerChat,
          msg_type: "interactive",
          content: JSON.stringify(card(cardBody, "对方没有收到回复", buttons)),
        },
      });
      log("feishu", "stranger reported to owner", { chat: message.chat_id, chatType: message.chat_type });
      void record(runtime.harness, context, {
        kind: "report",
        who: senderId.slice(-6),
        chat: message.chat_id,
        detail: message.chat_type === "p2p" ? "p2p stranger" : "unopened group",
      });
    } catch (error) {
      logError("feishu", "could not report stranger to owner", error);
    }
  }

  /** Every step a shared group needs: remember it is open, and give its conversation the rules. */
  async function openGroup(chatId: string, instructions: string): Promise<void> {
    // One level replaces the old pair of lists: the chat is a group, which is also what the gate
    // and the memory isolation read.
    await setChatLevel(runtime.harness, context, chatId, "group");
    const conv = await runtime.conversationFor(chatId);
    await setIsolatedPolicy(runtime.harness, context, conv.id as unknown as number);
    await conv.configure({ instructions }, context);
    log("feishu", "group opened", { chatId });
  }

  /** Replies in a topic group belong inside the topic, whoever started it. */
  const threadFor = (message: NonNullable<IncomingMessage["message"]>): boolean =>
    wantsThread(message, topicChats.get(message.chat_id ?? "") === true);

  /** Parent messages already checked for "was this a reply to us", keyed by message id. */
  const quotedFromUs = new Map<string, boolean>();
  /**
   * A reply to one of our own messages is somebody talking to us; in a group that is how people
   * answer without re-typing an @. Only a real quote reply has a parent (in a topic the parent is
   * the topic root), so this stays a cheap check.
   */
  async function quoteIsOurs(parentId: string | undefined): Promise<boolean> {
    if (parentId === undefined) return false;
    const known = quotedFromUs.get(parentId);
    if (known !== undefined) return known;
    let ours = false;
    try {
      const response = await im.message.get({ path: { message_id: parentId } });
      const sender = response?.data?.items?.[0]?.sender as
        | { id?: string; open_bot_id?: string; sender_type?: string }
        | undefined;
      ours = writtenByApp(sender, config.feishu.appId, botOpenId);
    } catch {
      return false;
    }
    if (quotedFromUs.size > 200) quotedFromUs.clear();
    quotedFromUs.set(parentId, ours);
    return ours;
  }

  async function handle(event: IncomingMessage): Promise<void> {
    const message = event.message;
    if (message?.message_id === undefined || message.chat_id === undefined) return;
    // First thing: the same message must never start two turns, however often it is delivered.
    if (handled.has(message.message_id)) {
      console.log(`[feishu] duplicate delivery msg=${message.message_id}, ignored`);
      return;
    }
    handled.add(message.message_id);
    if (handled.size > 1_000) handled.delete(handled.values().next().value as string);
    /** Command answers belong in the topic the command was typed in, not at the group's root. */
    const say = (text: string): Promise<void> =>
      replyText(message.message_id as string, text, { thread: threadFor(message) });
    const parsed = parseContent(message);
    const text = parsed.text;
    const owner = config.feishu.allowedSenderIds;
    const senderId = event.sender?.sender_id?.open_id ?? "";
    // The bridge refuses to start without an owner, so an empty list here means nobody is one.
    const isOwner = owner.length > 0 && owner.includes(senderId);
    const access = await chatAccess(runtime.harness, context).catch(() => ({}) as Record<string, ChatLevel>);
    const level = levelOf(message.chat_id, access, config.feishu.allowedChatIds);
    // A group's own name belongs in the rail; the event never carries it, so ask once.
    if (level !== "closed" && message.chat_id.startsWith("oc_")) {
      // Awaited: whether this is a topic group decides where the answer goes, and the lookup is
      // cached after the chat's first message.
      await rememberChatInfo(message.chat_id).catch(() => {});
    }
    const isGroupChat = level === "group";
    let reason = rejectionReason(event, parsed, level, isOwner);
    // Quoting our answer is a person addressing us, in a group too — no second @ needed.
    if (reason === "group-message-without-mention" && (await quoteIsOurs(message.parent_id))) {
      reason = undefined;
    }
    // Where to send "someone is asking" — the stranger's chat cannot be the address. The owner's
    // own DM is level owner from its first message, so a card always has somewhere to go.
    if (isOwner && message.chat_type === "p2p") {
      void rememberOwnerChat(runtime.harness, context, message.chat_id).catch(() => {});
      if (level !== "owner") {
        void setChatLevel(runtime.harness, context, message.chat_id, "owner").catch(() => {});
      }
    }
    // A chat the bot has never been opened in gets one card to the owner, with one button.
    if (level === "closed" && !isOwner) {
      await notifyOwnerOfStranger(event, message, text);
    }
    const ownerName = config.feishu.ownerName;
    // A chat pinned to the owner: say so once, instead of carding every message.
    if (reason === "owner-only") {
      await sendText(message.chat_id, `这个会话只对 ${ownerName} 开放。发一句 /allow 可以让这里所有人都能用。`).catch(
        () => {},
      );
    }

    // Self-service onboarding: the owner authorizes a new group from the group. A group is
    // shared, so /allow opens it in one step — its members then need no per-person card, and
    // none of them sees the owner's memory.
    if (text.trim() === "/allow" && isOwner) {
      if (message.chat_type === "p2p") {
        // A p2p chat has exactly one other person in it, so opening it opens it to them.
        await setChatLevel(runtime.harness, context, message.chat_id, "owner");
        await sendText(message.chat_id, "已把本会话加入白名单。");
        return;
      }
      await openGroup(message.chat_id, groupInstructions());
      await sendText(
        message.chat_id,
        `已把本群开放：群里所有人都能用，查代码、查 test/prod 的库和日志都能做；改文件、写库、看密钥只有 ${ownerName} 能做，会被直接拒。`,
      );
      return;
    }
    if (text.trim() === "/allow") {
      // Only the owner authorizes a chat; a closed group is told why, an open one stays quiet.
      if (reason !== undefined) await sendText(message.chat_id, `只有 ${ownerName} 能授权本群。`).catch(() => {});
      return;
    }
    if (text.trim() === "/close" && isOwner) {
      // The level goes back to closed, so an opened group can be shut without editing .env.
      await setChatLevel(runtime.harness, context, message.chat_id, "closed");
      await sendText(message.chat_id, "已关闭本群的访问：之后这里 @我 不会有回应。再发 /allow 可以重新开放。");
      return;
    }
    const openMatch = /^\/open(?:\s+([\s\S]+))?$/i.exec(text.trim());
    if (openMatch !== null && isOwner) {
      const topic = (openMatch[1] ?? "").trim();
      const conversation = await runtime.conversationFor(message.chat_id);
      await setChatTopic(runtime.harness, context, conversation.id as unknown as number, topic.length > 0 ? topic : undefined);
      await openGroup(message.chat_id, groupInstructions(topic.length > 0 ? topic : undefined));
      await sendText(
        message.chat_id,
        `已把本群开放给所有成员${topic.length > 0 ? `（主题：${topic}）` : ""}：查代码、查 test/prod 的库和日志都能做；改文件、写库、看密钥只有 ${ownerName} 能做，会被直接拒。`,
      );
      return;
    }
    if (isOwner && text.trim().startsWith("/proactive")) {
      const arg = text.trim().split(/\s+/)[1] ?? "";
      const conversation = await runtime.conversationFor(message.chat_id);
      const conversationId = conversation.id as unknown as number;
      if (arg === "") {
        const mode = await proactiveOf(runtime.harness, context, conversationId);
        await sendText(
          message.chat_id,
          `主动消息：${
            mode === "auto" ? "auto（照常发）" : mode === "observe" ? "observe（先攒着，/outbox flush 才发）" : "off（不发，直接丢）"
          }\n改：/proactive off | observe | auto`,
        );
        return;
      }
      if (arg !== "off" && arg !== "observe" && arg !== "auto") {
        await sendText(message.chat_id, "用法：/proactive off | observe | auto");
        return;
      }
      await setProactive(runtime.harness, context, conversationId, arg);
      await sendText(
        message.chat_id,
        arg === "auto"
          ? "主动消息恢复：后台跑完、提醒、日报照常发到这里。"
          : arg === "observe"
            ? "主动消息先攒着不发：/outbox 看，/outbox flush 发。"
            : "主动消息关掉了：后台跑完、提醒、日报都不发到这里，只记日志。",
      );
      return;
    }
    const outboxText = text.trim();
    if (isOwner && (outboxText === "/outbox" || outboxText.startsWith("/outbox "))) {
      const [, verb, target] = outboxText.split(/\s+/);
      if (verb === "flush") {
        const released = await releaseHeld(runtime.harness, context);
        await drainOutbox(runtime.harness, context, sendHeld).catch((error) =>
          logError("feishu", "outbox flush failed", error),
        );
        await sendText(message.chat_id, released === 0 ? "没有攒着的消息。" : `放出了 ${released} 条，正在发。`);
        return;
      }
      if (verb === "retry" || verb === "drop") {
        const id = target === undefined || target === "all" ? undefined : target;
        if (verb === "retry" && id === undefined && target === undefined) {
          await sendText(message.chat_id, "用法：/outbox retry <id|all>");
          return;
        }
        const moved =
          verb === "retry"
            ? await redrive(runtime.harness, context, id)
            : await discardDead(runtime.harness, context, id);
        await sendText(
          message.chat_id,
          moved === 0
            ? `没有这条死信：${target}`
            : verb === "retry"
              ? `放回队列 ${moved} 条，15 秒内会重发。`
              : `丢掉 ${moved} 条死信。`,
        );
        return;
      }
      if (verb !== undefined) {
        await sendText(message.chat_id, "用法：/outbox [flush|retry <id|all>|drop <id|all>]");
        return;
      }
      const waiting = await pendingOutbox(runtime.harness, context);
      const dead = await deadLetters(runtime.harness, context);
      const mine = waiting.filter((entry) => entry.chatId === message.chat_id);
      const mineDead = dead.filter((entry) => entry.chatId === message.chat_id);
      const others = waiting.length - mine.length;
      if (mine.length === 0 && mineDead.length === 0) {
        await sendText(
          message.chat_id,
          others === 0 ? "outbox 是空的：没有等着的消息。" : `本会话没有，别的会话还有 ${others} 条。`,
        );
        return;
      }
      const lines = mine.slice(0, 5).map((entry) => {
        const when = new Date(entry.createdAt).toLocaleString("zh-CN", { timeZone: "Asia/Shanghai" });
        const state = entry.paused === true ? "攒着" : `重试 ${entry.attempts} 次`;
        return `· ${when}（${state}）${entry.text.replace(/\s+/g, " ").slice(0, 60)}`;
      });
      const deadLines = mineDead.slice(0, 3).map((entry) => {
        const why = entry.reason === "age" ? "超过 24h" : `失败 ${entry.attempts} 次`;
        return `· 死信 ${entry.id}（${why}）${entry.text.replace(/\s+/g, " ").slice(0, 40)}`;
      });
      await sendText(
        message.chat_id,
        [
          `outbox 有 ${mine.length} 条${others > 0 ? `（别的会话还有 ${others} 条）` : ""}：`,
          ...lines,
          ...(mine.length > 5 ? [`…还有 ${mine.length - 5} 条`] : []),
          ...(mineDead.length > 0
            ? [
                "",
                `发不出去的 ${mineDead.length} 条（死信）：`,
                ...deadLines,
                ...(mineDead.length > 3 ? [`…还有 ${mineDead.length - 3} 条`] : []),
                "重发 /outbox retry <id|all>，丢掉 /outbox drop <id|all>。",
              ]
            : []),
          ...(mine.some((entry) => entry.paused === true) ? ["", "攒着的用 /outbox flush 发出去。"] : []),
        ].join("\n"),
      );
      return;
    }
    if (isGroupChat && !isOwner) {
      log("feishu", "member turn", { chatId: message.chat_id, sender: senderId.slice(-6) });
    }
    if (reason === "chat-not-allowed" && isOwner) {
      // Log the text: if /allow did not match, seeing what arrived is the whole diagnosis.
      console.log(
        `[feishu] unknown chat ${message.chat_id}, asking owner to authorize text=${JSON.stringify(text.slice(0, 40))} type=${message.message_type}`,
      );
      await sendText(
        message.chat_id,
        `这个群还没授权（${message.chat_id}）。\n发 /allow 授权本群，或把它加进 .env 的 FEISHU_ALLOWED_CHAT_IDS。`,
      ).catch(() => {});
      return;
    }
    const summary = `chat=${message.chat_id} chat_type=${message.chat_type ?? "?"} sender=${
      event.sender?.sender_id?.open_id ?? "?"
    }`;
    if (reason !== undefined) {
      log("feishu", `ignored (${reason})`, summary);
      return;
    }
    console.log(
      `[feishu] accepted ${summary} msg=${message.message_id} type=${message.message_type} images=${
        parsed.imageKeys.length
      } thread=${message.thread_id ?? "-"} root=${message.root_id ?? "-"} parent=${message.parent_id ?? "-"} text=${JSON.stringify(
        text.slice(0, 60),
      )}`,
    );
    const chatId = message.chat_id;
    const startedAt = Date.now();
    /** Seconds since the turn started, for progress lines. */
    const elapsed = (): string => `${Math.round((Date.now() - startedAt) / 1000)}s`;
    // Quoting a message is how alex asks a follow-up; it is not a request for a new branch.
    // Forking on `parent_id`/`root_id` scattered one chat across dozens of conversations, each
    // looking empty from the client. Only a real Feishu topic (thread_id) starts a new one.
    const threadKey =
      message.thread_id !== undefined && message.thread_id !== message.message_id
        ? message.thread_id
        : undefined;
    const parentMessageId = message.parent_id ?? message.root_id;
    // The message that opens a topic names it, so the rail reads like the question.
    const title = topicTitle(text);
    const conversation =
      threadKey === undefined
        ? await runtime.conversationFor(chatId)
        : await runtime.conversationForThread(chatId, {
            threadKey,
            parentMessageId,
            title: title.length === 0 ? undefined : title,
          });
    if (isGroupChat) {
      // A group is shared, so its memory is its own for the whole conversation; who is asking
      // only changes the audit label now, not the tool surface. Reapplying it every turn also
      // replaces the old "heal an opened chat" step: nothing can go stale.
      const topic = await chatTopicOf(runtime.harness, context, conversation.id as unknown as number).catch(
        () => undefined,
      );
      await setIsolatedPolicy(runtime.harness, context, conversation.id as unknown as number).catch(() => {});
      // A shared group does not need the owner's 16k of headroom: compacting earlier keeps every
      // group turn cheaper, and the transcript it summarizes is public to the group anyway.
      await conversation
        .configure({ compaction: { reserveTokens: 8_192 } }, context)
        .catch((error: unknown) => logError("feishu", "could not set group compaction", error));
      await conversation
        .configure({ instructions: groupInstructions(topic, isOwner ? "owner" : "member") }, context)
        .catch((error: unknown) => logError("feishu", "could not set group instructions", error));
      // The audit trail and the memory rule read this; the tool surface no longer does.
      await setChatTurn(runtime.harness, context, conversation.id as unknown as number, isOwner ? "owner" : "member").catch(
        (error: unknown) => logError("feishu", "could not mark the turn", error),
      );
    }
    // A conversation remembers the turn limit it was created with, so a raised default never
    // reached the chats that already exist. This is a Feishu-driven conversation, its limit is
    // the global one, not a subagent role's.
    await conversation
      .configure({ maxTurns: config.maxTurns }, context)
      .catch((error: unknown) => logError("feishu", "could not set the turn limit", error));
    // Commands must survive a mention that could not be resolved to the bot: "@agent /stop"
    // and a leftover "@ /stop" both have to reach the /stop branch.
    const command = text.trim().replace(/^@\S*\s*/, "");

    if (/^\/(help|帮助)$/i.test(command)) {
      await replyCard(message.message_id, HELP, "💡 命令一览", {
        thread: threadFor(message),
        mention: mentionFor(event, message),
      });
      return;
    }
    // `/new 交接内容` starts the new context with that line as its user message, so the next turn
    // picks up where the old one left off instead of starting blank.
    const resetMatch = /^\/(?:new|reset|新会话|清空)(?:\s+([\s\S]*))?$/i.exec(command);
    if (resetMatch !== null) {
      const handoff = (resetMatch[1] ?? "").trim();
      await conversation.reset(handoff.length === 0 ? undefined : handoff, context);
      await sendText(
        chatId,
        handoff.length === 0
          ? "已开新会话。历史还在存储里，长期记忆也保留。"
          : `已开新会话，交接：「${handoff.slice(0, 80)}${handoff.length > 80 ? "…" : ""}」。下一句话我接着这个做。`,
      );
      return;
    }
    if (/^\/(compact|压缩)$/i.test(command) && isOwner) {
      // A durable task: it may take a while, so the card is not held open for it.
      const started = await conversation.compact(undefined, context).catch((error: unknown) => {
        logError("feishu", "could not start compaction", error);
        return undefined;
      });
      await sendText(
        chatId,
        started === undefined ? "压缩没起来，看日志。" : "已开始压缩这段对话（后台任务），好了这里会多一条记录。",
      );
      return;
    }
    // A digest, not a log: what moved since the owner last looked, and what waits on them.
    // Owner only, because the approval lines name the owner's own tool calls.
    if (/^\/(ketchup|进度|怎么样了)$/i.test(command)) {
      if (!isOwner) {
        await say(`只有 ${ownerName} 能看这个。`).catch(() => {});
        return;
      }
      const digest = await collectKetchup(runtime.harness, context);
      await replyCard(message.message_id, formatKetchup(digest), "📋 回顾", {
        thread: threadFor(message),
        mention: mentionFor(event, message),
      });
      return;
    }
    if (/^\/(digest|早报|昨晚)$/i.test(command)) {
      if (!isOwner) {
        await say(`只有 ${ownerName} 能看这个。`).catch(() => {});
        return;
      }
      const body = await collectDigest(runtime.harness, context);
      await replyCard(message.message_id, body, "🌅 早报", {
        thread: threadFor(message),
        mention: mentionFor(event, message),
      });
      return;
    }
    if (/^\/(stop|abort|停止|中断)$/i.test(command)) {
      const outcome = await stopConversation(runtime, conversation.id as unknown as number);
      await say(
        outcome.cancelled > 0
          ? `已撤销排队的 ${outcome.cancelled} 条消息，前面那轮继续跑。`
          : "已中断当前任务。",
      );
      return;
    }
    const modelAsk = /^\/(?:model|模型)(?:\s+([\s\S]*))?$/i.exec(command);
    if (modelAsk !== null) {
      if (!isOwner) {
        await say(`只有 ${ownerName} 能换模型。`).catch(() => {});
        return;
      }
      const agent = await conversation.agent(context).catch(() => undefined);
      const provider = agent?.model?.provider ?? config.model.provider;
      const modelId = agent?.model?.modelId ?? config.model.modelId;
      const thinking = agent?.thinkingLevel ?? config.thinkingLevel;
      const catalog = runtime.models();
      const ask = parseModelCommand(modelAsk[1] ?? "");

      if (ask.kind === "list") {
        const defaultModel = await runtime.defaultModel().catch(() => undefined);
        const defaultId = defaultModel?.modelId;
        // Five buttons fit a card; the rest is `/model <id>`.
        const picks = pickModelChoices(catalog, provider, modelId, defaultId);
        const lines = [
          `当前：\`${provider}/${modelId}\` · thinking **${thinking}**`,
          defaultId === undefined || defaultId.length === 0 ? "" : `默认：\`${defaultId}\`（新会话用它）`,
          "",
          "点一个换本会话，或 `/model <id>`、`/model thinking high`、`/model default <id>`",
        ].filter((line) => line.length > 0);
        await replyCard(
          message.message_id,
          lines.join("\n"),
          "🤖 模型",
          {
            thread: threadFor(message),
            mention: mentionFor(event, message),
            buttons: picks.map((entry) => ({
              // The gateway routes by prefix (auto/, sitin-deepseek/, sitin-glm/) and the same model
              // name exists under several of them, so the whole id goes on the button.
              label: entry.provider === provider ? entry.id : `${entry.provider}/${entry.id}`,
              value: { kind: "set-model", conversationId: conversation.id as unknown as number, provider: entry.provider, modelId: entry.id },
              type: entry.provider === provider && entry.id === modelId ? ("primary" as const) : ("default" as const),
            })),
          },
        );
        return;
      }

      if (ask.kind === "default") {
        const entry = resolveModelPick(catalog, ask.modelId, provider);
        if (entry === undefined) {
          await say(`没有这个模型：${ask.modelId}\n可用的有：${catalog.slice(0, 6).map((item) => item.id).join("、")}`).catch(() => {});
          return;
        }
        await runtime.setDefaultModel({ provider: entry.provider, modelId: entry.id });
        await say(`默认模型已改为 ${entry.provider}/${entry.id}（新会话生效，当前会话不变）`);
        return;
      }

      if (ask.kind === "thinking") {
        const currentEntry = resolveModelPick(catalog, `${provider}/${modelId}`, provider) ?? resolveModelPick(catalog, modelId, provider);
        const levels = currentEntry?.thinkingLevels ?? [];
        if (levels.length > 0 && !levels.includes(ask.level)) {
          await say(`思考档位只能是：${levels.join(" / ")}`).catch(() => {});
          return;
        }
        await runtime.setModel(conversation, { provider, modelId, thinkingLevel: ask.level });
        await say(`本会话 thinking 改为 ${ask.level}（下一轮生效）`);
        return;
      }

      const entry = resolveModelPick(catalog, ask.modelId, provider);
      if (entry === undefined) {
        await say(`没有这个模型：${ask.modelId}\n可用的有：${catalog.slice(0, 6).map((item) => item.id).join("、")}`).catch(() => {});
        return;
      }
      await runtime.setModel(conversation, { provider: entry.provider, modelId: entry.id, thinkingLevel: thinking });
      await say(`本会话已切到 ${entry.provider}/${entry.id}（下一轮生效）`);
      return;
    }

    if (/^\/(cost|花费|额度)$/i.test(command)) {
      const budget = await runtime.budget(chatId);
      const limit = budget.limit <= 0 ? "未设上限" : `$${budget.limit.toFixed(2)}`;
      await say(`今日花费 $${budget.spentToday.toFixed(4)} / ${limit}`);
      return;
    }

    // Steering bends a run that is already going, which is why it is a command and not what a
    // message does by default: a stray "对了…" sent mid-run should wait its turn, not redirect work
    // in flight. Queued messages are the normal path and they cost nothing to correct.
    const steerText = parseSteer(command);
    if (steerText === "") {
      await say("用法：/steer 要它改的地方（这条会插进正在跑的那一轮）");
      return;
    }
    if (steerText !== undefined) {
      // Nothing running: the words are just a message. Starting a run from a steer would need its
      // own card and its own answer delivery, which is the path below — so take that path.
      if (await runInFlight(conversation).catch(() => false)) {
        const speaker =
          message.chat_type === "p2p"
            ? undefined
            : speakerTag(isOwner, senderId, isOwner ? undefined : await senderName(senderId).catch(() => undefined));
        const content = await buildUserContent(message, { ...parsed, text: steerText }, speaker);
        // The request id is the message id, so a redelivered event steers once, not twice.
        const steered = await conversation.submit(
          { type: "input", content, whenBusy: "steer", requestId: `steer:${message.message_id}` },
          context,
        );
        log("feishu", "steered a running turn", {
          chatId,
          conversationId: conversation.id,
          submissionId: steered.id,
          status: steered.status,
        });
        await say("已插话：这一轮在下个工具调用边界看到它（要是它已经收尾了，就并进收尾）。");
        return;
      }
      parsed.text = steerText;
    }
    if (/^\/(memory|记忆)$/i.test(command)) {
      const memory = await runtime.memories(conversation);
      const render = (notes: Array<{ at: string; text: string }>, label: string): string =>
        notes.length === 0
          ? `${label}：空`
          : `${label}（${notes.length}）\n${notes
              .slice(-10)
              .map((note) => `- ${note.at.slice(0, 10)} ${note.text}`)
              .join("\n")}`;
      await replyCard(
        message.message_id,
        `${render(memory.global, "全局记忆")}\n\n${render(memory.chat, "当前对话")}`,
        "🧠 记忆",
      );
      return;
    }
    if (/^\/(project|项目|projects?)(\s|$)/i.test(command)) {
      const wanted = command.replace(/^\/(project|项目|projects?)\s*/i, "").trim();
      const conversationId = conversation.id as unknown as number;
      const projects = await runtime.projects();
      const currentId = await runtime.projectOfConversation(conversationId);

      if (wanted.length === 0) {
        const lines = projects.map(
          (project) => `- ${project.name}${project.id === currentId ? " ← 当前" : ""}\n  \`${project.path}\``,
        );
        await replyCard(
          message.message_id,
          `当前仓库：${projects.find((p) => p.id === currentId)?.name ?? "workspace"}\n\n${lines.join("\n")}\n\n切换：\`/project ${projects[0]?.name ?? "名字"}\``,
          "📦 项目",
          { thread: threadFor(message), mention: mentionFor(event, message) },
        );
        return;
      }
      if (!isOwner) {
        await say("只有 owner 能切仓库。");
        return;
      }
      const result = await runtime.switchProject(conversationId, wanted);
      if ("error" in result) {
        await say(`${result.error}\n可选：${result.candidates.join("、") || "（无）"}`);
        return;
      }
      await sendText(
        chatId,
        `已切到 ${result.project.name}\n目录：${result.dir}\n分支：${result.branch}\n上下文保留，接着说就行。`,
      );
      return;
    }
    if (/^\/(memo|群记忆|本群记忆)(\s|$)/i.test(command)) {
      const arg = command.replace(/^\/(memo|群记忆|本群记忆)\s*/i, "").trim();
      const conversationId = conversation.id as unknown as number;

      if (arg.length === 0) {
        const notes = await chatNotes(runtime.harness, context, conversationId);
        const body =
          notes.length === 0
            ? "本群还没有专属记忆。\n\n`/memo add 这个群只讨论 Android 客户端` 这样添加，模型每次回答都会看到。"
            : `本群专属记忆（${notes.length}）\n${notes
                .map((note, index) => `${index + 1}. ${note.text}`)
                .join("\n")}\n\n\`/memo add 内容\` · \`/memo del 序号\` · \`/memo clear\``;
        await replyCard(message.message_id, body, "📌 本群记忆", {
          thread: threadFor(message),
          mention: mentionFor(event, message),
        });
        return;
      }
      if (!isOwner) {
        await say("只有 owner 能改本群记忆。");
        return;
      }
      if (/^clear$/i.test(arg)) {
        const removed = await clearChatNotes(runtime.harness, context, conversationId);
        await say(`已清空本群记忆（删除 ${removed} 条）。`);
        return;
      }
      const del = arg.match(/^(?:del|delete|remove|删)\s+(\d+)$/i);
      if (del !== null) {
        const result = await removeChatNote(runtime.harness, context, conversationId, Number(del[1]));
        await sendText(
          chatId,
          result.removed === undefined
            ? `没有第 ${del[1]} 条（当前 ${result.total} 条）`
            : `已删除：${result.removed}\n还剩 ${result.total} 条`,
        );
        return;
      }
      const text = arg.replace(/^(?:add|set|加|记)\s+/i, "").trim();
      const result = await addChatNote(runtime.harness, context, conversationId, text);
      await sendText(
        chatId,
        result.added
          ? `已记入本群记忆（共 ${result.total} 条）：${text}`
          : `这条已经在记忆里了（共 ${result.total} 条）`,
      );
      return;
    }
    if (/^\/(id|会话)$/i.test(command)) {
      const currentProjectId = await runtime.projectOfConversation(conversation.id as unknown as number);
      // The model belongs on this line: in a topic fork it can differ from the chat's, and
      // `fork: "asOf"` keeps it that way, so "which model answered here" needs an answer.
      const chatMeta = (await runtime.harness.snapshot(SurfaceMetaDoc, context).catch(() => undefined))?.surfaces[chatId];
      const selfName = await agentName(runtime.harness, context).catch(() => undefined);
      const plan = await conversation.agent(context).catch(() => undefined);
      const model = plan?.model === undefined ? `${config.model.provider}/${config.model.modelId}` : `${plan.model.provider}/${plan.model.modelId}`;
      const thinking = plan?.thinkingLevel ?? config.thinkingLevel;
      await sendText(
        chatId,
        [
          `chat: ${chatId}`,
          ...(chatMeta?.chatName === undefined
            ? []
            : [
                `name: ${chatMeta.chatName}${
                  chatMeta.chatOwner === undefined ? "" : ` · 群主 …${chatMeta.chatOwner.slice(-6)}`
                }${chatMeta.members === undefined ? "" : ` · ${chatMeta.members} 人`}`,
              ]),
          `conversation: ${conversation.id}${threadKey === undefined ? "" : `\nthread: ${threadKey}（已 fork）`}`,
          ...(selfName === undefined ? [] : [`agent: ${selfName}`]),
          `model: ${model} · thinking ${thinking}`,
          `project: ${(await runtime.projects()).find((project) => project.id === currentProjectId)?.name ?? "workspace"}`,
        ].join("\n"),
      );
      return;
    }

    const budget = await runtime.budget(chatId);
    if (!budget.allowed) {
      await sendText(
        chatId,
        `今日额度已用完（$${budget.spentToday.toFixed(2)} / $${budget.limit.toFixed(2)}）。明天自动重置，或调大 AGENT_DAILY_BUDGET_USD。`,
      );
      return;
    }

    // Feishu redelivers an event whose ack was lost (a long-connection reconnection, a restart),
    // and a replay carries the original message id. That message already has its transcript entry,
    // so the turn is over: without this check the replay finds the finished submission, settles in
    // a second, and posts the old answer again as a brand-new card.
    if ((await lookupMessage(runtime.harness, context, message.message_id).catch(() => undefined)) !== undefined) {
      log("feishu", "duplicate event ignored", { chatId, messageId: message.message_id });
      return;
    }

    // One thought split over two messages is one turn (see collectBurst).
    const burst = await collectBurst(conversation.id as unknown as number, { message, parsed, senderId });
    if (burst === undefined) return;

    log("feishu", "turn start", { chatId, conversationId: conversation.id, type: message.message_type, batched: burst.length });
    const inGroup = message.chat_type === "group";
    const replyOptions = { thread: threadFor(message), mention: mentionFor(event, message) };
    const digest = digestOf(burst.length === 1 ? parsed.text : burst.map((entry) => entry.parsed.text).join(" / "));
    const opening =
      parsed.audioKey !== undefined
        ? "🎧 听语音…"
        : parsed.imageKeys.length > 0
          ? "🖼️ 看图…"
          : "🤔 收到，处理中…";
    const cardId = await replyCard(
      message.message_id,
      digest.length === 0 ? opening : `> ${digest}\n\n${opening}`,
      undefined,
      // The card carries its status from the first frame, so it never starts as a bare message and
      // then grows a header when it settles.
      { ...replyOptions, header: WORKING_HEADER },
    );
    if (cardId === undefined) {
      log("feishu", "reply card creation failed, continuing without streaming");
      // Silence used to be the answer here. With a burst, silence now covers the other messages
      // folded into this turn as well, and nobody can tell a lost message from a slow one.
      await replyText(
        message.message_id,
        `卡片没建成功，这条${burst.length > 1 ? `（和后面 ${burst.length - 1} 条）` : ""}没跑，再发一次试试。`,
        { thread: threadFor(message) },
      ).catch(() => {});
      return;
    }
    log("feishu", "card created", cardId);
    const requestId = `feishu:${message.message_id}`;
    await markCardPending(runtime.harness, context, requestId, {
      cardId,
      chatId,
      conversationId: conversation.id as unknown as number,
      messageId: message.message_id,
      thread: threadFor(message),
      chatType: message.chat_type,
    }).catch((error) =>
      logError("feishu", "could not record pending card", error),
    );

    // If the run stalls (provider hang, restart mid-run), say so instead of spinning forever.
    const card = liveCard(conversation, cardId, digest, startedAt);
    card.stall(300_000);

    // The card's own life, as one value instead of a flag per concern: only a live card may be
    // patched, and settling is a transition rather than "set finished, then remember to check it in
    // every callback that can still fire". A late watcher or heartbeat therefore cannot put
    // "思考中…" back on a finished card, and a new patch site cannot forget the rule.
    const slot = enqueueTurn(conversation.id as unknown as number);
    if (slot.queued) {
      // `pi.live` describes one run per conversation — whatever is running now. A card that starts
      // watching while its own turn is still in line therefore renders the *previous* turn's steps,
      // and two cards show the same work. A queued card only says it is in line; it starts watching
      // when its own turn starts.
      card.setStatus(`⏸️ 排队中 · 上一条还在跑`);
      await slot.wait;
      if (slot.cancelled()) {
        // Stopped lands on the message it was pressed for. This one never ran, so there is nothing
        // to recover: the card says so and the turn does not touch the conversation.
        log("feishu", "queued turn cancelled", { chatId, requestId });
        // Released before painting: the card patch can fail, and a lost patch must not leave every
        // later message in this chat waiting forever.
        slot.done();
        await card.finish();
        await clearCardPending(runtime.harness, context, requestId).catch(() => {});
        await deliverAnswer(
          chatId,
          cardId,
          "这条排队时被撤掉了，前面那轮继续跑。要它的话，再发一次。",
          "🗑️ 撤销",
          undefined,
          { template: "grey", title: "🗑️ 已撤销" },
        );
        return;
      }
      card.clearStatus();
    }

    // The two marks on the message this turn answers: 👀 while it runs, ✅ once it lands. Declared
    // outside the try so the finally can settle them on every exit, including a throw.
    let answered = false;
    let reactionId: string | undefined;

    try {
      reactionId = await markWorking(message.message_id);
      // Written down before the turn can die: the mark has to come off the message even when this
      // process never reaches its own finally. Recovery reads it from the same card record.
      if (reactionId !== undefined) {
        await attachReaction(runtime.harness, context, requestId, reactionId).catch((error) =>
          logError("feishu", "could not record the mark", error),
        );
      }
      const contents: UserContent[] = [];
      for (const entry of burst) {
        const entryOwner = owner.includes(entry.senderId);
        const speaker =
          entry.message.chat_type === "p2p"
            ? undefined
            : speakerTag(entryOwner, entry.senderId, entryOwner ? undefined : await senderName(entry.senderId).catch(() => undefined));
        contents.push(await buildUserContent(entry.message, entry.parsed, speaker));
      }
      const userContent = mergeUserContent(contents);
      // Marked before the turn's first write, so the finished card can name what this turn changed
      // and not the uncommitted mess that was already in the tree when it started.
      const turnAgent = await conversation.agent(context).catch(() => undefined);
      const workDir = turnAgent?.cwd ?? config.workspace;
      const treeMark = await markTree(workDir).catch(() => undefined);
      const submission = await conversation.submit(
        { type: "input", content: userContent, requestId },
        context,
      );
      await attachSubmission(runtime.harness, context, requestId, submission.id as unknown as number).catch(
        (error) => logError("feishu", "could not attach submission", error),
      );
      log("feishu", "submitted", requestId);
      // Started here, not at card creation: the first snapshot a card sees must belong to its own
      // submission, otherwise a restart-recovered run leaves its steps on the next card.
      await card.start();
      const settled = await submission.wait(context);
      const seconds = Math.round((Date.now() - startedAt) / 100) / 10;
      log("feishu", "settled", { status: settled.status, reason: settled.reason ?? null, seconds });
      // Read once, on every path: the trail belongs to this turn, and leaving it behind would make
      // the next turn's card claim a decision it never made.
      const decisions = takeDecisions(String(conversation.id));
      // A verdict the model filed during the turn: the card repeats it rather than guessing.
      const claim = takeDone(String(conversation.id));
      if (claim !== undefined) {
        decisions.push({
          at: claim.at,
          kind: "stop",
          text: `声明：${claim.met ? "目标达成" : "目标未达成"} — ${claim.why}`,
        });
      }
      if (settled.reason !== undefined) {
        decisions.push({ at: Date.now(), kind: "stop", text: `结束：${settled.reason}` });
      }
      // The card keeps the verdict in memory; the ring keeps it for `/digest`. A turn the system
      // ended has no claim, and still has to appear in the morning — as a shortfall, not a silence.
      const turnSeconds = Math.round((Date.now() - startedAt) / 100) / 10;
      if (claim !== undefined) {
        await recordVerdict(runtime.harness, context, {
          chat: chatId,
          met: claim.met,
          why: claim.why,
          seconds: turnSeconds,
          ...(settled.reason === undefined ? {} : { reason: settled.reason }),
        });
      } else if (settled.reason !== undefined) {
        await recordVerdict(runtime.harness, context, {
          chat: chatId,
          met: false,
          why: "",
          seconds: turnSeconds,
          reason: settled.reason,
        });
      }

      // Freeze live output first, then write the answer.
      await card.finish();
      // The ping is the only notification Feishu gives: a card patch is silent, and "✅ 答完了 ·
      // 62s" tells a locked phone that something finished. It used to carry the head of the answer
      // too, which in a group read as the same answer twice — and the head is the first line, so
      // "我这边的情况如下：" was all it ever said. A group's root message is a broadcast, not
      // somebody waiting, so it stays silent; a topic thread pings even inside a group.
      const pings =
        shouldPingDone(config.feishu.donePingSeconds, seconds * 1000) &&
        (replyOptions.thread === true || message.chat_type !== "group");
      const ping = async (text: string): Promise<void> => {
        if (!pings) return;
        await pingDone(chatId, message.message_id ?? "", text, { thread: replyOptions.thread === true });
      };
      // One builder for all four exits. The failure paths used to hand over a card with a reason
      // and nothing to look at, exactly when "which step broke, and what did it print" is the
      // question the owner is about to ask.
      const turnPanel = async (spentUsd: number): Promise<Omit<TurnDetails, "at"> | undefined> => {
        if (settled.entry === undefined) return undefined;
        const taken = await runtime
          .turnSteps(conversation, settled.entry as unknown as number, settled.answer as unknown as number)
          .catch(() => []);
        if (taken.length === 0 && decisions.length === 0) return undefined;
        return { steps: taken, seconds, costUsd: spentUsd, decisions };
      };
      if (settled.entry !== undefined) {
        // Remember where this message landed so a later reply can fork at it. The card is
        // indexed too: people reply to the answer, not to their own question, and a thread
        // rooted at the answer is what has to become its own conversation.
        const entryId = settled.entry as unknown as number;
        await runtime
          .recordMessage(message.message_id, conversation, entryId)
          .catch((error) => logError("feishu", "could not index message", error));
        await runtime
          .recordMessage(cardId, conversation, entryId)
          .catch((error) => logError("feishu", "could not index answer card", error));
      }

      if (settled.status === "done" && settled.answer !== undefined) {
        const answer = await runtime.answerText(conversation, settled.answer, settled.entry);
        log("feishu", "answer read", { chars: answer.length });
        const after = await runtime.budget(chatId).catch(() => undefined);
        const cost = after === undefined ? 0 : Math.max(0, after.spentToday - budget.spentToday);
        const touched = treeMark === undefined ? [] : await touchedSince(workDir, treeMark).catch(() => []);
        // One line under the card, and only when it has something to say: what changed on disk.
        // "12 个工具" and "$0.31" were readable but not actionable, and the ping under the card
        // already says how long the turn took.
        const meta = touchedLine(touched);
        // The steps are always recorded (the client reads them) and now ride along in the answer
        // card as a folded panel, so a group sees the work too and nothing has to be pressed.
        const steps = await turnPanel(cost);
        if (steps !== undefined) {
          await saveTurnDetails(runtime.harness, context, message.message_id, steps).catch((error) =>
            logError("feishu", "could not save turn details", error),
          );
        }
        const blocks = await runtime
          .answerBlocks(conversation, settled.answer, settled.entry)
          .catch(() => [] as AnswerBlock[]);
        const text = blocks.length > 0 ? composeAnswer(blocks) : answer || "（模型没有返回文本）";
        const replyCtx = { replyTo: message.message_id, thread: replyOptions.thread === true };
        await deliverAnswer(
          chatId,
          cardId,
          text,
          meta,
          undefined,
          claim === undefined ? { template: "green", title: "✅ 答完了" } : doneVerdict(claim),
          steps,
          replyCtx,
        );
        answered = true;
        await ping(`✅ 答完了 · ${seconds}s`);
        // A conversation this long makes every step cost about a minute, and the tokens still fit the
        // window, so threshold compaction never fires. Compact on message count instead, after the
        // answer is out, so the user is not left waiting for the next turn to be slow.
        const convId = conversation.id as unknown as number;
        const carried = lastRequestMessages(convId);
        const carriedTokens = lastRequestTokens(convId);
        if (carriedTokens > COMPACT_ABOVE_TOKENS || carried > COMPACT_AFTER_MESSAGES) {
          log("feishu", "conversation too long, compacting", { chatId, messages: carried, tokens: carriedTokens });
          await conversation.compact(undefined, context).catch((error: unknown) =>
            logError("feishu", "could not compact a long conversation", error),
          );
        }
      } else if (settled.reason === "turn_limit") {
        // The work sits in the transcript, so ask for the rest instead of making the owner type
        // "继续": a `continue` submission runs the same context with no new user message.
        log("feishu", "turn limit reached, continuing once", { chatId });
        const continued = await conversation
          .submit({ type: "continue", requestId: `continue:${message.message_id}` }, context)
          .catch((error: unknown) => {
            logError("feishu", "could not continue after the turn limit", error);
            return undefined;
          });
        const rest = await continued?.wait(context).catch(() => undefined);
        if (rest?.status === "done" && rest.answer !== undefined) {
          // `continue` places no entry of its own, but the run began at the owner's message: read the
          // range so the text written before the turn limit is part of the answer, not lost.
          const answer = await renderAnswerBody(conversation, rest.answer, settled.entry);
          const total = Math.round((Date.now() - startedAt) / 100) / 10;
          // Nothing under the answer: the header says 续跑一次 and the ping says how long. The line
          // that used to sit here repeated both in smaller type.
          await deliverAnswer(
            chatId,
            cardId,
            answer,
            "",
            undefined,
            claim === undefined ? { template: "green", title: "✅ 答完了（续跑一次）" } : doneVerdict(claim),
            undefined,
            { replyTo: message.message_id, thread: replyOptions.thread === true },
          );
          answered = true;
          await ping(`✅ 答完了（自动续跑一次） · ${total}s`);
          await note(conversation, context, `跑到 ${config.maxTurns} 步上限，自动续跑了一次`);
          return;
        }
        const spent = await runtime.budget(chatId).catch(() => undefined);
        await deliverAnswer(
          chatId,
          cardId,
          `续跑一次仍没收尾（活比较大，不是出错）。\n\n进度都在，点「继续」接着做。`,
          "",
          continueButton(message.message_id),
          { template: "orange", title: `⏸️ 到了 ${config.maxTurns} 步上限` },
          await turnPanel(spent === undefined ? 0 : Math.max(0, spent.spentToday - budget.spentToday)),
        );
        await ping(`⏸️ 这轮到了 ${config.maxTurns} 步上限，续跑后仍未收尾 · ${seconds}s`);
      } else if (settled.reason === "aborted") {
        // A turn the owner stopped is not a turn that broke. Same grey card — the work did not
        // finish — but it should read as the stop working, not as a fault.
        const spent = await runtime.budget(chatId).catch(() => undefined);
        await deliverAnswer(
          chatId,
          cardId,
          "已经停在这里了。进度都在，点「继续」接着做。",
          "",
          continueButton(message.message_id),
          { template: "grey", title: "⏹️ 已中断" },
          await turnPanel(spent === undefined ? 0 : Math.max(0, spent.spentToday - budget.spentToday)),
        );
        await ping(`⏹️ 已中断 · ${seconds}s`);
      } else {
        // The header says only that the run did not finish; the body says where it stopped, in
        // words. The enum used to sit in the title (`⚠️ 这次没答出来 · turn_limit`), which read as
        // a stack trace on a phone.
        const reason = settled.reason ?? "unanswered";
        const text = settled.detail === undefined ? "" : `${String(settled.detail)}\n\n`;
        const spent = await runtime.budget(chatId).catch(() => undefined);
        await deliverAnswer(
          chatId,
          cardId,
          `${text}卡在：${stopReason(reason)}。进度都在，点「继续」接着做。`,
          "",
          continueButton(message.message_id),
          { template: "grey", title: "⚠️ 这轮没跑完" },
          await turnPanel(spent === undefined ? 0 : Math.max(0, spent.spentToday - budget.spentToday)),
        );
        await ping(`⚠️ 这轮没跑完（${stopReason(reason)}）· ${seconds}s`);
      }
    } catch (error) {
      logError("feishu", "turn failed", error);
      await deliverAnswer(
        chatId,
        cardId,
        `原因：${(error as Error).message}\n\n进度都在，点「继续」接着做。`,
        "",
        continueButton(message.message_id),
        { template: "red", title: "❌ 出错了" },
      );
    } finally {
      slot.done();
      await card.finish();
      await clearCardPending(runtime.harness, context, requestId).catch(() => {});
      if (reactionId !== undefined) await settleMark(message.message_id, reactionId, answered);
    }
  }

/** Feishu's own name for a chat, and whose it is. */
async function chatInfo(
  chatId: string,
): Promise<{ name?: string; owner?: string; members?: number; topic?: boolean; chatType?: "p2p" | "group" } | undefined> {
  const response = await client
    .request({ method: "GET", url: `/open-apis/im/v1/chats/${chatId}` })
    .catch((error: unknown) => {
      logError("feishu", "could not read the chat info", error);
      return undefined;
    });
  const data = (
    response as {
      data?: { name?: string; owner_id?: string; user_count?: number; chat_mode?: string; group_message_type?: string };
    }
  )?.data;
  if (data === undefined) return undefined;
  return {
    ...(data.name === undefined || data.name.length === 0 ? {} : { name: data.name }),
    ...(data.owner_id === undefined ? {} : { owner: data.owner_id }),
    ...(typeof data.user_count === "number" ? { members: data.user_count } : {}),
    // A topic group: `group_message_type` is "thread", and every message is its own topic, so an
    // inline answer would start a new topic instead of continuing the conversation.
    ...(data.group_message_type === "thread" || data.chat_mode === "topic" ? { topic: true } : {}),
    // p2p or group, so the rail can file a chat under 私聊 or 群聊 without opening it.
    ...(data.chat_mode === "p2p" ? { chatType: "p2p" as const } : {}),
    ...(data.chat_mode === "group" || data.chat_mode === "topic" ? { chatType: "group" as const } : {}),
  };
}

/** Names this process already asked for, so a busy group costs one API call, not one per message. */
const chatNamesKnown = new Set<string>();

/** Topic groups by chat id: known as soon as the chat's info is fetched once. */
const topicChats = new Map<string, boolean>();

/** Once per chat: store Feishu's name (a title the owner set wins) and whose group it is. */
async function rememberChatInfo(chatId: string): Promise<void> {
  if (chatNamesKnown.has(chatId)) return;
  chatNamesKnown.add(chatId);
  const info = await chatInfo(chatId);
  if (info === undefined) return;
  const meta = (await runtime.harness.snapshot(SurfaceMetaDoc, context).catch(() => undefined))?.surfaces[chatId];
  topicChats.set(chatId, info.topic === true);
  await updateSurfaceMeta(runtime.harness, context, chatId, {
    ...(info.topic === undefined ? {} : { topic: info.topic }),
    ...(info.chatType === undefined ? {} : { chatType: info.chatType }),
    ...(info.name === undefined ? {} : { chatName: info.name }),
    ...(info.owner === undefined ? {} : { chatOwner: info.owner }),
    ...(info.members === undefined ? {} : { members: info.members }),
    // A name the owner set (or an earlier title) is never overwritten; an empty one takes the
    // group's own name, which reads better in the rail than a fragment of the first message.
    ...(info.name !== undefined && (meta?.title === undefined || meta.title.length === 0)
      ? { title: info.name }
      : {}),
  }).catch((error: unknown) => logError("feishu", "could not store the chat info", error));
}

/** At boot: fill in names and chat kinds for chats that have a conversation but no name yet. */
async function backfillChatNames(): Promise<void> {
  const mapping = await runtime.harness.snapshot(ChatsToken, context).catch(() => undefined);
  const known = Object.keys(mapping?.chats ?? {}).filter((chatId) => chatId.startsWith("oc_"));
  const meta = (await runtime.harness.snapshot(SurfaceMetaDoc, context).catch(() => undefined))?.surfaces ?? {};
  // A chat stored before chatType existed is still nameless in the rail's terms, and one API
  // call settles both: the name and whether it is a group, a topic group, or a single chat.
  const missing = known.filter(
    (chatId) => (meta[chatId]?.chatName ?? "").length === 0 || meta[chatId]?.chatType === undefined,
  );
  for (const chatId of missing) await rememberChatInfo(chatId);
  if (missing.length > 0) log("feishu", "chat names filled in", { chats: missing.length });
}

  /** One quoted message by id; undefined when it cannot be read (deleted, no scope, gone). */
  /** "unsupported" once the API tells us the app may not read group messages at all. */
  let threadRead: "unknown" | "unsupported" = "unknown";
  /** Newest create_time we have already shown the model, per thread, so a turn does not re-send them. */
  const threadSeen = new Map<string, number>();

  /**
   * What was said in this topic since we last looked. Cursor is in memory: after a restart the last
   * few lines ride along once more, which beats losing a topic's context to a redeploy.
   */
  async function readThreadDigest(
    message: NonNullable<IncomingMessage["message"]>,
  ): Promise<string> {
    const threadId = message.thread_id ?? "";
    if (threadId.length === 0) return "";
    if (threadRead === "unsupported") return "";
    // Not one of the five operations the gateway owns: this is a read-only history page for the
    // digest, and it stays on the SDK even in gateway mode.
    const response = await client.im.message
      .list({
        params: {
          container_id_type: "thread",
          container_id: threadId,
          sort_type: "ByCreateTimeAsc",
          page_size: 50,
          with_sender_name: true,
        },
      })
      .catch((error: unknown) => {
        // Reading a group's messages needs its own scope; without it every turn would log the same
        // failure, so say it once and stop asking.
        if (JSON.stringify(error).includes("im:message.group_msg")) {
          threadRead = "unsupported";
          log("feishu", "cannot read topic history: the app is missing the im:message.group_msg scope", {});
        } else {
          logError("feishu", "could not read the topic", error);
        }
        return undefined;
      });
    const items = response?.data?.items ?? [];
    if (items.length === 0) return "";
    let newest = threadSeen.get(threadId) ?? 0;
    for (const item of items) newest = Math.max(newest, Number(item?.create_time ?? 0));
    const seen = threadSeen.get(threadId) ?? 0;
    threadSeen.set(threadId, newest);
    if (threadSeen.size > 64) threadSeen.delete(threadSeen.keys().next().value as string);
    // create_time is a millisecond epoch string; take what is newer than the last look.
    const fresh = items.filter((item) => Number(item?.create_time ?? 0) > seen);
    const digest = threadDigest(fresh, { current: message.message_id ?? "" });
    if (digest.length > 0) {
      log("feishu", "topic context attached", { lines: items.filter((item) => Number(item?.create_time ?? 0) > seen).length });
    }
    return digest;
  }

  async function readQuoted(parentId: string): Promise<Quoted | undefined> {
    const hit = quotedCache.get(parentId);
    if (hit !== undefined && Date.now() - hit.at < QUOTED_TTL_MS) return hit.value;
    // `im` goes through feishu-gw, which answers with the sender only — no type, no body — so every
    // quote used to arrive as "（没有可读的文字）". Message history is the one call the SDK keeps.
    const response = await client.im.message
      .get({ path: { message_id: parentId } })
      .catch((error: unknown) => {
        logError("feishu", "could not read the quoted message", error);
        return undefined;
      });
    const item = response?.data?.items?.[0];
    // Our own cards read back as a placeholder, so the text we sent is the copy that counts.
    const stored = await sentCardText(runtime.harness, context, parentId);
    const value: Quoted | undefined =
      item !== undefined && item.deleted === true
        ? undefined
        : item === undefined && stored === undefined
          ? undefined
          : {
              who:
                stored !== undefined || item?.sender?.sender_type === "app"
                  ? "bot"
                  : ((await senderName(String(item?.sender?.id ?? "")).catch(() => undefined)) ??
                    `ou_${String(item?.sender?.id ?? "?").slice(-6)}`),
              text:
                stored !== undefined
                  ? stored.slice(0, 1_200)
                  : quoteText(
                      item?.msg_type ?? "",
                      item?.body?.content ?? "",
                      // The API's mentions carry the open_id as a string; ours carry it one level down.
                      (item?.mentions ?? []).map((mention: { key?: string; name?: string; id?: string }) => ({
                        key: mention.key,
                        name: mention.name,
                        id: { open_id: mention.id },
                      })),
                    ),
            };
    quotedCache.set(parentId, { at: Date.now(), value });
    if (quotedCache.size > QUOTED_CACHE_MAX) {
      const oldest = quotedCache.keys().next().value;
      if (oldest !== undefined) quotedCache.delete(oldest);
    }
    return value;
  }

  async function buildUserContent(
    message: NonNullable<IncomingMessage["message"]>,
    parsed: ParsedContent,
    speaker?: string,
  ): Promise<UserContent> {
    /** Who is talking, on its own line, so a long turn does not read as the assistant's own. */
    const tag = (text: string): string => (speaker === undefined ? text : `${speaker} ${text}`);
    // A reply carries the id of the message it answers, never its text: "这个怎么改" needs the quote.
    const quotedId = quotedParentId(message);
    const quoted = quotedId.length > 0 ? await readQuoted(quotedId) : undefined;
    if (quoted !== undefined) log("feishu", "quoted message attached", { id: quotedId.slice(-8), chars: quoted.text.length });
    // A topic's back-and-forth never reaches us as events, so pull it in alongside the reply.
    const topic = await readThreadDigest(message);
    const withQuote = (text: string): string =>
      tag(`${topic}${quoted === undefined ? "" : quoteBlock(quoted)}${text}`);
    if (parsed.audioKey !== undefined) {
      const bytes = await downloadResource(message.message_id ?? "", parsed.audioKey, "file");
      if (bytes === undefined) return withQuote(`${parsed.text}\n（语音下载失败）`.trim());
      const asr = await transcribeAudio(bytes, "opus");
      if (!asr.ok) return withQuote(`${parsed.text}\n（语音转写失败：${asr.error}）`.trim());
      if (asr.text.length === 0) return withQuote(`${parsed.text}\n（语音里没听出内容）`.trim());
      return withQuote(`[语音转写] ${asr.text}${parsed.text.length > 0 ? `\n（随语音附带的文字）${parsed.text}` : ""}`);
    }
    if (parsed.imageKeys.length === 0) return withQuote(parsed.text);
    const images: Array<{ type: "image"; data: string; mimeType: string }> = [];
    for (const key of parsed.imageKeys.slice(0, 3)) {
      const image = await downloadImage(message.message_id ?? "", key);
      if (image !== undefined) images.push({ type: "image" as const, ...image });
      else console.warn(`[feishu] image ${key} could not be downloaded`);
    }
    if (images.length === 0) {
      return withQuote(`${parsed.text}\n（图片下载失败，只看到文字）`.trim());
    }
    // Anchor the model on the picture: without this it tends to keep talking about
    // whatever the previous turns were about and treat the image as decoration.
    const who = speaker === undefined ? config.feishu.ownerName : speaker;
    const hint = `${who} 发来 ${images.length} 张图片，先依据图片内容回答，图片是这条消息的主体`;
    const caption = parsed.text.length > 0 ? `${hint}\n${parsed.text}` : `${hint}\n看看这张图，告诉我重点。`;
    return [{ type: "text" as const, text: withQuote(caption) }, ...images];
  }

  const eventDispatcher = new Lark.EventDispatcher({}).register({
    "im.message.receive_v1": (data: IncomingMessage) => {
      // Ack before the turn, not after it. The SDK answers Feishu only once this handler resolves,
      // and a turn takes minutes — every event that waited for one was redelivered by Feishu, so
      // nearly every message arrived twice and the second copy was dropped by the in-memory set.
      // Never let a bad event take down the long connection either.
      void handle(data).catch((error) => {
        if (isWithdrawnError(error)) {
          log("feishu", "message withdrawn before reply", { messageId: data.message?.message_id });
          return;
        }
        console.error("[feishu] handler failed", error);
      });
    },
    "card.action.trigger": async (data: CardAction) => {
      try {
        return await handleCardAction(data);
      } catch (error) {
        console.error("[feishu] card action failed", error);
        return {};
      }
    },
  });

  if (config.feishu.allowedChatIds.length === 0 && config.feishu.allowedSenderIds.length === 0) {
    console.warn(
      "[feishu] no allowlist configured: anyone who can reach this bot can run read/write/edit/bash. " +
        "Send one message, then copy the logged chat/sender ids into FEISHU_ALLOWED_CHAT_IDS / FEISHU_ALLOWED_SENDER_IDS.",
    );
  }

  // Nothing is listening on a socket any more, so stop() has only the interval to clear.
  let stopTransport = (): void => {};

  // feishu-gw owns the long connection and forwards each event here. The answer is returned
  // synchronously because a card action has to be: the gateway hands our reply straight back to
  // Feishu, which shows its own error if that takes more than about three seconds.
  gatewayIngress = async (payload) =>
    await eventDispatcher.invoke(
      {
        schema: "2.0",
        header: {
          event_id: payload.eventId ?? "",
          event_type: payload.eventType ?? (payload.type === "card" ? "card.action.trigger" : "im.message.receive_v1"),
          create_time: String(Date.now()),
        },
        event: payload.event,
      },
      { needCheck: false },
    );
  // Only the url: the endpoint object also carries the shared token, and a token in the log is a
  // token in every screenshot of the log.
  const endpoint = gatewayEndpoint();
  log("feishu", "transport go: events arrive from feishu-gw", { url: endpoint.url });

  /**
   * A message that reaches the owner even when the gateway cannot: the SDK's HTTP client is a
   * different path to the same app, so it keeps working while the gateway is the broken thing.
   */
  const tellOwnerDirect = async (text: string): Promise<void> => {
    const chat = await ownerChatId(runtime.harness, context).catch(() => undefined);
    if (chat === undefined) return;
    await sendTextDirect(chat, text);
  };

  /**
   * Restart the gateway process without knowing its label: the plist in LaunchAgents is the
   * record of what it was installed as. A gateway can hold a connection Feishu routes nothing
   * to (seen while bringing this up), and only a fresh process re-subscribes.
   */
  const kickGateway = async (): Promise<string> => {
    const dir = join(homedir(), "Library", "LaunchAgents");
    const plist = (await readdir(dir).catch(() => [] as string[])).find((name) => name.endsWith("feishu-gw.plist"));
    if (plist === undefined) return "no launchd job found";
    const label = plist.replace(/\.plist$/, "");
    return await new Promise<string>((resolve) => {
      const child = spawn("launchctl", ["kickstart", "-k", `gui/${process.getuid?.() ?? 501}/${label}`], {
        stdio: "ignore",
      });
      child.on("error", (error) => resolve(`kickstart failed: ${error.message}`));
      child.on("exit", (code) => resolve(code === 0 ? `kickstart ${label}` : `kickstart exited ${code}`));
    });
  };

  let watch = { misses: 0, alertedAt: 0 };
  /**
   * The watchdog watches the *connection*, not the traffic. A gateway that is connected and quiet
   * is what a normal evening looks like — and after every restart it is quiet until the next
   * message arrives, which is exactly when an alert would be wrong. `ready` (connected *and* has
   * received something) still drives status; silence beyond that is reported, never alerted on.
   */
  const pollGateway = async (): Promise<void> => {
    let healthy = false;
    let why = "";
    try {
      const response = await fetch(`${endpoint.url}/healthz`, { signal: AbortSignal.timeout(5_000) });
      const health = (await response.json()) as { connected?: boolean; ready?: boolean; dropped?: number };
      healthy = health.connected === true;
      why = "网关在跑，但连不上飞书";
      const ready = health.ready === true;
      if (ready && !feishuStatus.ready) feishuStatus.since = Date.now();
      if (!ready && feishuStatus.ready) {
        feishuStatus.flaps += 1;
        feishuStatus.since = Date.now();
      }
      feishuStatus.ready = ready;
      if (!healthy) feishuStatus.lastError = "feishu-gw is up but not connected to Feishu";
      else if (!ready) feishuStatus.lastError = "feishu-gw connected, nothing received yet";
      if ((health.dropped ?? 0) > 0) {
        feishuStatus.lastError = `feishu-gw dropped ${health.dropped} event(s)`;
      }
    } catch (error) {
      why = `网关进程没应答（${endpoint.url}）`;
      if (feishuStatus.ready) feishuStatus.since = Date.now();
      feishuStatus.ready = false;
      feishuStatus.lastError = (error as Error)?.message ?? String(error);
    }
    const step = gatewayWatchStep(watch, healthy, Date.now());
    watch = step.state;
    if (step.act === "alert") {
      const minutes = Math.max(1, Math.round((Date.now() - feishuStatus.since) / 60_000));
      logError("feishu", `feishu-gw not ready for ${minutes}min (${why})`, undefined);
      const outcome = await kickGateway().catch((error: unknown) => `kickstart failed: ${String(error)}`);
      log("feishu", `gateway restart attempted: ${outcome}`);
      await tellOwnerDirect(
        `⚠️ 飞书网关失联约 ${minutes} 分钟\n` +
          `收不到新消息，也发不出去（${why}）。\n` +
          `已尝试重启网关：${outcome}。若这条之后仍无动静，手动跑 ./scripts/agentctl status。`,
      ).catch((error: unknown) => logError("feishu", "could not report gateway outage", error));
    } else if (step.act === "recovered") {
      log("feishu", "feishu-gw is ready again");
      await tellOwnerDirect("✅ 飞书网关恢复了，消息收发正常。").catch(() => {});
    }
  };
  const watchTimer = setInterval(() => void pollGateway(), 30_000);
  watchTimer.unref();
  stopTransport = () => clearInterval(watchTimer);
  // A crash or redeploy leaves the conversation resuming in this process while the
  // old card has no owner. Reacquire the submission, wait for it, deliver the answer
  // as a message, and close the card — the durable equivalent of "it kept working".
  void (async () => {
    // Only cards from before this process started. A turn that begins while this loop runs creates a
    // placeholder card too, and treating it as a leftover delivered the same finished answer twice.
    const bootedAt = Date.now();
    const leftovers = await leftoverCards(runtime.harness, context, bootedAt).catch(() => []);
    for (const card of leftovers) {
      const attempt = await noteCardAttempt(runtime.harness, context, card.requestId).catch(() => 0);
      if (attempt > 3) {
        // Feishu rejects the card or the chat (a placeholder written while testing, a group
        // the bot left). Retrying every boot only produced two failed calls per restart.
        logError("feishu", `giving up on a card after ${attempt - 1} recovery attempts`, {
          cardId: card.cardId,
          chatId: card.chatId,
        });
        await clearCardPending(runtime.harness, context, card.requestId).catch(() => {});
        continue;
      }
      log("feishu", "unfinished card from previous process", { cardId: card.cardId, submissionId: card.submissionId });
      const startedAt = clock(card.at);
      const elapsed = duration(Date.now() - card.at);
      // A card patch raises no Feishu notification: the live path pings for exactly that reason and
      // this one used to stay silent, so an answer recovered after a restart sat in a card nobody
      // was told about. Same rule as the live turn — a topic thread pings, a group's root message
      // stays quiet, and a run too short to be news stays quiet too.
      const ping = async (text: string): Promise<void> => {
        if (card.messageId === undefined) return;
        if (card.chatType === "group" && card.thread !== true) return;
        if (!shouldPingDone(config.feishu.donePingSeconds, Date.now() - card.at)) return;
        await pingDone(card.chatId, card.messageId, text, { thread: card.thread === true });
      };
      // The 👀 the dead process stamped stays on the message otherwise: the chat would keep showing
      // work in progress for a turn that finished hours ago. Same lie the answered flag prevents.
      const settle = async (answered: boolean): Promise<void> => {
        if (card.messageId === undefined || card.reactionId === undefined) return;
        await settleMark(card.messageId, card.reactionId, answered);
      };
      if (card.submissionId === undefined) {
        await patchCard(
          card.cardId,
          `🔁 进程重启过，这条消息没来得及提交（${startedAt} 发出）。麻烦再发一次。`,
          `🔁 未提交 · ${elapsed} 前`,
        ).catch(() => {});
        await settle(false);
        await clearCardPending(runtime.harness, context, card.requestId).catch(() => {});
        continue;
      }
      await patchCard(
        card.cardId,
        `🔁 进程重启过，这条任务已在后台恢复。\n\n开始于 ${startedAt}，已跑 ${elapsed}，结果会写进这张卡片。`,
        `🔁 已恢复 · ${startedAt} 起`,
      ).catch(() => {});
      void (async () => {
        try {
          const submission = await runtime.harness.submission(
            card.submissionId as unknown as Parameters<typeof runtime.harness.submission>[0],
            context,
          );
          if (submission === undefined) throw new Error("submission gone");
          const settled = await submission.wait(context);
          const conversation = await runtime.harness.conversation(
            card.conversationId as unknown as Parameters<typeof runtime.harness.conversation>[0],
            context,
          );
          const total = duration(Date.now() - card.at);
          // The run may have been resumed by the new process's own turn handler, which delivers the
          // answer and clears the mark. Delivering it here as well is what posted the same message
          // twice — once inside the topic as a card, once in the chat as text.
          // One transition replaces two guards: whichever process moves the card live → delivering
          // posts the answer, and the other finds it taken. `gone` means the live turn already
          // delivered and cleared its mark.
          const taken = await moveCard(
            runtime.harness,
            context,
            card.requestId,
            "live",
            "delivering",
            `recover:${process.pid}`,
          );
          if (!taken.moved) {
            log("feishu", "recovered answer already taken", { cardId: card.cardId, phase: taken.found });
            return;
          }
          log("feishu", "recovered delivery footer", `${startedAt} 起 · ${total}`);
          if (settled.status === "done" && settled.answer !== undefined && conversation !== undefined) {
            const answer = await runtime.answerText(conversation, settled.answer, settled.entry);
            // Into the card that asked, not a second card beside it: one message, one card. Posting
            // a fresh card left the interrupted one frozen at 处理中 and made the same work appear
            // twice in the chat.
            await deliverAnswer(
              card.chatId,
              card.cardId,
              answer.length > 0 ? answer : "（模型没有返回文本）",
              `✅ ${total} · ${startedAt} 起，中途进程重启过`,
              undefined,
              { template: "green", title: "✅ 答完了（重启后补发）" },
            );
            log("feishu", "recovered answer delivered", { chatId: card.chatId, chars: answer.length, total });
            await ping(`✅ 答完了（重启后补发） · ${total}`);
            await settle(true);
            await note(
              conversation,
              context,
              `进程重启过，把上次那条答完了（${startedAt} 起，${total}）`,
            );
          } else {
            await deliverAnswer(
              card.chatId,
              card.cardId,
              settled.reason === "turn_limit"
                ? `上次那条跑到 ${config.maxTurns} 步上限就停了（工作是接着跑的，进度都在）。回一句「继续」我接着做。`
                : `上次那条没答出来（${settled.reason ?? "unanswered"}）。回一句「继续」我接着做。`,
              `${startedAt} 起 · ${total}`,
              undefined,
              standaloneHeader(settled.reason),
            );
            await ping(`❌ 没答完（重启后补发） · ${total}`);
            await settle(false);
          }
        } catch (error) {
          logError("feishu", "could not recover answer", error);
          await deliverAnswer(
            card.chatId,
            card.cardId,
            "上次那条恢复失败，麻烦再发一次。",
            `❌ 恢复失败`,
            undefined,
            { template: "red", title: "❌ 恢复失败" },
          ).catch(() => {});
          await settle(false);
        } finally {
          await clearCardPending(runtime.harness, context, card.requestId).catch(() => {});
        }
      })();
    }
  })();

  return {
    async notifyOwner(text: string): Promise<void> {
      const chat = await ownerChatId(runtime.harness, context).catch(() => undefined);
      if (chat === undefined) {
        // Before the owner has ever written, there is no chat to send to; the log is the only trace.
        log("feishu", "boot notice dropped: owner chat unknown");
        return;
      }
      await sendNotice(chat, text);
    },
    stop() {
      // Every waiter is told no and no timer survives; the registry is the only place that knows
      // what was still pending.
      dropAllApprovals();
      stopTransport();
    },
  };
}

function sniff(bytes: Buffer): string {
  if (bytes.length > 8 && bytes[0] === 0x89 && bytes[1] === 0x50) return "image/png";
  if (bytes.length > 3 && bytes[0] === 0xff && bytes[1] === 0xd8) return "image/jpeg";
  if (bytes.length > 12 && bytes.subarray(8, 12).toString() === "WEBP") return "image/webp";
  if (bytes.length > 6 && bytes.subarray(0, 3).toString() === "GIF") return "image/gif";
  return "image/png";
}
