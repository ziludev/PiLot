/**
 * `head -20 f`, then `sed -n '21,60p' f`, then `cat f`: one file read in three calls and three
 * round-trips, each answering the question the previous one could not. The first slice is a guess;
 * by the second, the guess is known to be wrong.
 *
 * So a slice read is recognised here, and the second read of the same file in the same stretch is
 * widened to the whole file. Only the plainest forms count, and only when the command is one
 * command: a pipe, a redirect, a substitution or a second statement keeps its shell meaning, since
 * there the output is not the file but something derived from it.
 */
export type SliceRead = { path: string; offset?: number; limit?: number };

/** Path chars that end the command's meaning if they appear outside quotes. */
const BREAKS = "|&;<>`$\n";

/** Split into words, honouring quotes; undefined when the command does more than read one file. */
export function tokens(command: string): string[] | undefined {
  const out: string[] = [];
  let quote: string | undefined;
  let current = "";
  for (const char of command) {
    if (quote !== undefined) {
      if (char === quote) quote = undefined;
      else current += char;
      continue;
    }
    if (char === "'" || char === '"') {
      quote = char;
      continue;
    }
    if (char === " " || char === "\t") {
      if (current !== "") out.push(current);
      current = "";
      continue;
    }
    if (BREAKS.includes(char)) return undefined;
    current += char;
  }
  if (quote !== undefined) return undefined;
  if (current !== "") out.push(current);
  return out;
}

/** The file a plain slice command reads, or undefined when it is anything more than that. */
export function parseSliceRead(command: string): SliceRead | undefined {
  const args = tokens(command.trim());
  if (args === undefined || args.length < 2) return undefined;
  const [head, ...rest] = args as [string, ...string[]];
  if (head === "cat") return rest.length === 1 ? { path: rest[0] as string } : undefined;
  if (head === "head") {
    // `head f` prints ten lines, so ten is what the model asked for.
    // `head -20` with no file is a syntax error, not a read of a file named "-20".
    if (rest.length === 1 && !(rest[0] as string).startsWith("-")) {
      return { path: rest[0] as string, limit: 10 };
    }
    if (rest.length === 3 && rest[0] === "-n" && isCount(rest[1])) {
      return { path: rest[2] as string, limit: Number(rest[1]) };
    }
    if (rest.length === 2 && /^-\d+$/.test(rest[0] as string)) {
      return { path: rest[1] as string, limit: Number((rest[0] as string).slice(1)) };
    }
    return undefined;
  }
  // `sed -n '40,120p' f` — the form used to page through a file. `$` as the end means "to the end".
  if (head === "sed" && rest.length === 3 && rest[0] === "-n") {
    const range = /^(\d+)(?:,(\d+|\$))?p$/.exec(rest[1] as string);
    if (range === null) return undefined;
    const start = Number(range[1]);
    if (start < 1) return undefined;
    if (range[2] === undefined) return { path: rest[2] as string, offset: start, limit: 1 };
    if (range[2] === "$") return { path: rest[2] as string, offset: start };
    const end = Number(range[2]);
    if (end < start) return undefined;
    return { path: rest[2] as string, offset: start, limit: end - start + 1 };
  }
  return undefined;
}

function isCount(value: string | undefined): boolean {
  return value !== undefined && /^\d+$/.test(value) && Number(value) > 0;
}

/** A pause this long between two reads of one file is a new question, not the same paging. */
const STALE_MS = 10 * 60_000;

type Read = { count: number; at: number };

const reads = new Map<string, Read>();

/**
 * Count one slice read of `path` and return how many times it has been read in this stretch.
 * The caller widens the read once this says 2: the first slice was a guess, and the second read
 * means the guess did not answer it.
 */
export function noteSlice(conversationId: number, path: string, now = Date.now()): number {
  const key = `${conversationId}\u0000${path}`;
  const previous = reads.get(key);
  const count = previous === undefined || now - previous.at > STALE_MS ? 1 : previous.count + 1;
  reads.set(key, { count, at: now });
  if (reads.size > 200) {
    for (const [key2, read] of reads) if (now - read.at > STALE_MS) reads.delete(key2);
  }
  return count;
}

/** Test hook. */
export function forgetSlices(): void {
  reads.clear();
}

/** The line that says the command was not run as written, so the output is not mistaken for it. */
export function widenedNote(path: string, count: number): string {
  return `（自动改写：${path} 刚才已经读过 ${count - 1} 次，这条命令改成一次读全文，省掉后面再翻几次。命令本身没有执行。）`;
}
