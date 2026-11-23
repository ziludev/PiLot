/**
 * What a running turn has finished, and which step it is on.
 *
 * The live snapshot shows one tool round at a time, so a card that only renders the snapshot
 * forgets every round that ended: a five-minute job reads "bash · 0s" and nothing else. This keeps
 * the trail instead — one line per finished round, plus the one running now — and it is pure, so
 * the card copy can be read in a test rather than by waiting five minutes.
 */

import type { LiveSnapshot } from "./live.ts";

export type Round = { label: string; ms: number; tail?: string; hint?: string };

export type Trail = {
  /** Rounds that ended, oldest first. */
  rounds: Round[];
  /** Identity of the round running now: call ids, so two `bash` rounds in a row stay two. */
  key?: string;
  /** What that round is called on the card. */
  label?: string;
  /** The running round's first argument line ("git log --oneline -3"), the only way two `bash` differ. */
  hint?: string;
  since: number;
  /** Last activity line seen in this round; the only thing a hanging round says. */
  tail?: string;
  turn?: number;
};

/** Enough history to answer "what did it do" without turning the card into a log. */
const MAX_ROUNDS = 40;

function identity(live: LiveSnapshot): string {
  if (live.callIds !== undefined && live.callIds.length > 0) return live.callIds.join(",");
  return live.tools.join(" · ");
}

/**
 * The round's name on the card is the tool's own name. Translating `bash` into `跑 …` read nicer
 * and told less: the arguments were reworded, clipped and sometimes guessed, so the card no longer
 * showed what was actually called. The bare name is what the model asked for.
 */
function labelOf(live: LiveSnapshot): string {
  return live.tools.join(" · ");
}

/** Folds one snapshot into the trail: a changed identity ends the previous round. */
export function advance(trail: Trail, live: LiveSnapshot, now: number, hint?: string): Trail {
  const key = identity(live);
  const next: Trail = { ...trail, key, label: labelOf(live), turn: live.turns ?? trail.turn };
  if (hint !== undefined && hint.length > 0) next.hint = hint;
  else delete next.hint;
  if (key !== trail.key) {
    const finished = trail.key !== undefined && trail.key.length > 0 && trail.label !== undefined;
    if (finished) {
      next.rounds = [
        ...trail.rounds,
        {
          label: trail.label ?? "",
          ms: now - trail.since,
          ...(trail.tail === undefined ? {} : { tail: trail.tail }),
          ...(trail.hint === undefined ? {} : { hint: trail.hint }),
        },
      ];
      if (next.rounds.length > MAX_ROUNDS) next.rounds = next.rounds.slice(next.rounds.length - MAX_ROUNDS);
    }
    next.since = now;
    delete next.tail;
  }
  if (live.activity !== undefined && live.activity.length > 0) next.tail = live.activity;
  return next;
}

/**
 * The tool's icon, for the card's eye only.
 *
 * The trail keeps `label` as the bare name — that record is what a test asserts and what an audit
 * reads — and the icon is added where the line is drawn. Its job is the one emoji does well on a
 * five-line list: a category the eye catches before it reads any word. One per tool, never two, and
 * an unknown tool gets the neutral gear rather than a guess.
 */
const TOOL_ICONS: Record<string, string> = {
  bash: "⌨️",
  shell: "⌨️",
  exec: "⌨️",
  read: "📄",
  cat: "📄",
  view: "📄",
  write: "✍️",
  edit: "✍️",
  patch: "✍️",
  apply_patch: "✍️",
  grep: "🔍",
  glob: "🔍",
  find: "🔍",
  search: "🔍",
  project_search: "🔍",
  tool_search: "🔍",
  logs_query: "📊",
  db_query: "🗄️",
  web_search: "🌐",
  web_fetch: "🌐",
  curl: "🌐",
  skill: "🧩",
  recall: "🧠",
  remember: "🧠",
  forget: "🧠",
  delegate: "🤖",
  delegate_parallel: "🤖",
  build_feature: "🤖",
  task_status: "🤖",
  cancel_task: "🤖",
  report_done: "🏁",
  remind: "⏰",
  routine: "⏰",
  routines: "⏰",
  open_project: "📂",
};

/**
 * The skill a `bash` call is running, if it is one.
 *
 * A skill is a folder of scripts: the agent opens with
 * `export SKILL_DIR="/Users/…/.pi/agent/skills/lark-doc"` and then runs what is inside it. The call
 * is a shell call honestly, but the skill is what is doing the work, and `⌨️ bash export SKILL_DIR=…`
 * told a reader neither of those things.
 */
export function skillOf(hint: string | undefined): string | undefined {
  if (hint === undefined) return undefined;
  // The name has to end on its own: a hint is cut at 90 characters, and half a skill name is
  // worse than no name at all.
  return /\.pi\/agent\/skills\/([\w.-]+)(?=[/"'\s])/.exec(hint)?.[1];
}

/**
 * What a shell command actually was, when the tool name says nothing.
 *
 * `bash` is not one thing: it is the web (curl), the repo (git), the tests (go test), the database
 * (psql), a file (cat). On this agent nearly every call is `bash`, so an icon that only looked at
 * the tool name put a keyboard on every line and the emoji explained nothing.
 */
/** `cd /repo && go test ./...` → `go test ./...`: the directory is bookkeeping, the verb is the work. */
function stripCd(command: string): string {
  let head = command.trim();
  for (let i = 0; i < 3; i += 1) head = head.replace(/^cd\s+[^&;|]+(&&|;)\s*/, "");
  return head;
}

function commandIcon(command: string): string | undefined {
  // Leading `cd … &&` is bookkeeping; the verb that follows is the thing being done.
  const head = stripCd(command);
  const verb = head.split(/[\s|;&<>]+/)[0]?.toLowerCase() ?? "";
  if (verb === "") return undefined;
  const table: Record<string, string> = {
    curl: "🌐", wget: "🌐", http: "🌐", https: "🌐", ping: "🌐",
    git: "🌿",
    go: "🧪", npm: "🧪", pnpm: "🧪", yarn: "🧪", cargo: "🧪", make: "🧪", pytest: "🧪", vitest: "🧪", jest: "🧪",
    npx: "🧪", tsc: "🧪", bun: "🧪",
    psql: "🗄️", mysql: "🗄️", sqlite3: "🗄️", "redis-cli": "🗄️",
    grep: "🔍", rg: "🔍", ag: "🔍", awk: "🔍", jq: "🔍",
    cat: "📄", head: "📄", tail: "📄", sed: "📄", ls: "📄", wc: "📄", less: "📄",
    find: "📂", tree: "📂", open: "📂",
    lark: "💬", feishu: "💬",
    docker: "📦", kubectl: "📦",
    python: "🐍", python3: "🐍", node: "🟩",
  };
  // `npm test`, `go build`: the sub-verb is what the icon should say.
  const rest = head.split(/\s+/).slice(1);
  for (const word of rest.slice(0, 3)) {
    const icon = table[word.toLowerCase()];
    if (icon !== undefined && word.startsWith("-") === false) return icon;
  }
  return table[verb];
}

export function toolIcon(name: string, hint?: string): string {
  const key = name.toLowerCase();
  const icon = TOOL_ICONS[key];
  // The shell's category is in its command, not in its name.
  if ((key === "bash" || key === "shell" || key === "exec") && hint !== undefined) {
    if (skillOf(hint) !== undefined) return "🧩";
    const fromCommand = commandIcon(hint);
    if (fromCommand !== undefined) return fromCommand;
  }
  if (icon !== undefined) return icon;
  // The owner's tools speak Feishu and talk to MCP servers: chat, whatever the integration.
  if (key.startsWith("feishu_") || key.startsWith("lark_") || key.startsWith("mcp_")) return "💬";
  return "⚙️";
}

/**
 * `bash · read` → `⌨️ bash · 📄 read`: the same names, with a category in front of each.
 *
 * Repeats collapse into a count. A round that fires two `bash` calls in parallel used to print
 * `⌨️ bash · ⌨️ bash`, which is one fact told twice and a line of the card spent on nothing; the
 * reader wants "two shells", so that is what it says.
 */
export function withIcons(label: string, hint?: string): string {
  const names = label.split(" · ").filter((name) => name.length > 0);
  const order: string[] = [];
  const counts = new Map<string, number>();
  for (const name of names) {
    if (!counts.has(name)) order.push(name);
    counts.set(name, (counts.get(name) ?? 0) + 1);
  }
  return order
    .map((name) => {
      const count = counts.get(name) ?? 1;
      return `${toolIcon(name, hint)} ${name}${count > 1 ? ` ×${count}` : ""}`;
    })
    .join(" · ");
}

/**
 * The argument line as the card prints it: one line, clipped, no backticks to break the span.
 *
 * Cut from the back, not the front: a command's verb is its first word, and `…commit -m "docs: …"`
 * says less than `git commit -m "docs: …"`. Output tails keep their end; arguments keep their start.
 */
export function hintLine(hint: string | undefined, limit = 46): string {
  const flat = (hint ?? "").replace(/\s+/g, " ").replace(/`/g, "'").trim();
  if (flat.length === 0) return "";
  if (flat.length <= limit) return flat;
  // When the budget is tight, spend it on the verb: `cd ~/.presence/projects/sitin-server && sed -…`
  // said where, and then ran out of room for what. A short `cd client-go && go test` keeps its
  // directory, because there it fits and the directory is the point.
  const body = stripCd(flat) || flat;
  return body.length > limit ? `${body.slice(0, limit - 1).trimEnd()}…` : body;
}

/**
 * A round's clock, empty when the round was shorter than the clock can say.
 *
 * A real card carried three `0s` on six lines: sub-second rounds are the normal case for a shell
 * that just greps a file, and printing the zero spends a third of each line on "that was quick".
 * The reader already knows — it is not the line with `▸` on it.
 */
function seconds(ms: number): string {
  if (ms < 1_000) return "";
  const value = Math.round(ms / 1000);
  if (value < 60) return `${value}s`;
  return `${Math.floor(value / 60)}m${String(value % 60).padStart(2, "0")}s`;
}

/** The trail as card lines: finished rounds ticked, the running one marked, oldest dropped first. */
export function renderSteps(trail: Trail, now: number, options: { limit?: number } = {}): string {
  const limit = options.limit ?? 5;
  const lines: string[] = [];
  const hidden = Math.max(0, trail.rounds.length - limit);
  if (hidden > 0) lines.push(`⋯ 前面还有 ${hidden} 步`);
  for (const round of trail.rounds.slice(hidden)) {
    lines.push(`✓ ${stepLine(round.label, round.hint, seconds(round.ms), round.tail)}`);
  }
  if (trail.key !== undefined && trail.key.length > 0 && (trail.label ?? "").length > 0) {
    lines.push(`▸ ${stepLine(trail.label ?? "", trail.hint, seconds(now - trail.since))}`);
  }
  return lines.join("\n");
}

/** `🌿 bash \`git log --oneline -3\` · 2s`: the name, what it ran, how long, and the last thing it said. */
/**
 * The last output line, or nothing when it does not read as anything.
 *
 * A tool's tail is whatever its output ended on, and that is often the shape of a value rather
 * than a fact: a query's last line is the word `dev`. A bare word shorter than a phrase is
 * dropped; a path, a sentence or a Chinese phrase still earns the room next to the clock.
 */
function tailLine(tail: string | undefined): string {
  const said = hintLine(tail, 32);
  if (said.length < 10 && !/[\s　]/.test(said) && !/[\u4e00-\u9fff]/.test(said)) return "";
  return said;
}

function stepLine(label: string, hint: string | undefined, elapsed: string, tail?: string): string {
  // A skill call says which skill; its `export SKILL_DIR=…` says nothing twice over.
  const what = skillOf(hint) ?? hintLine(hint);
  const head = `${withIcons(label, hint)}${what.length === 0 ? "" : ` \`${what}\``}`;
  return [head, elapsed, tailLine(tail)].filter((part) => part.length > 0).join(" · ");
}

/** How many steps to report: rounds that ended, and whether one is running. */
export function stepCount(trail: Trail): { done: number; running: boolean } {
  return { done: trail.rounds.length, running: trail.key !== undefined && trail.key.length > 0 };
}
