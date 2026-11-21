/**
 * Whether a shell command only looks at things.
 *
 * The member gate used to answer this the other way round — a blacklist of writing programs — which
 * is wrong in both directions: `grep "cp" notes.md` was refused because the word appeared in it, and
 * `python3 -c 'open("x","w")'` was allowed because no listed program was named. Deciding "read only"
 * from the actual program and its arguments fixes both, and it is the same table PiBot uses.
 *
 * Only single commands qualify. A pipe, a redirect, a substitution or a second statement can turn a
 * reader into a writer (`grep x f > f`), and this function does not model shells.
 */
import { tokens } from "./toolbox/tools/bash-read.ts";

/** Programs that cannot write, whatever their arguments. */
export const READONLY_PROGRAMS = new Set([
  "ls",
  "cat",
  "head",
  "tail",
  "wc",
  "grep",
  "rg",
  "find",
  "file",
  "stat",
  "du",
  "df",
  "which",
  "echo",
  "pwd",
  "date",
  "cal",
  "uname",
  "sw_vers",
  "whoami",
  "sleep",
  "sort",
  "uniq",
  "cut",
  "tr",
  "awk",
  "jq",
  "basename",
  "dirname",
  "realpath",
  "readlink",
  "md5",
  "md5sum",
  "sha256sum",
  "shasum",
  "diff",
  "comm",
  "column",
  "tree",
  "ps",
  "lsof",
  "netstat",
  "dig",
  "nslookup",
  "host",
  "ping",
  "git",
]);

/** `find` with any of these runs a program or writes a file per match. */
const FIND_WRITES = new Set(["-delete", "-exec", "-execdir", "-ok", "-okdir", "-fprint", "-fprint0", "-fprintf", "-fls"]);

/** `git` subcommands that never change the repository. */
const GIT_READS = new Set([
  "status",
  "log",
  "diff",
  "show",
  "blame",
  "rev-parse",
  "ls-files",
  "ls-tree",
  "shortlog",
  "describe",
  "cat-file",
  "show-ref",
  "ls-remote",
  "whatchanged",
  "name-rev",
  "merge-base",
  "grep",
  "count-objects",
  "for-each-ref",
]);

function gitReads(args: readonly string[]): boolean {
  // Global options come before the subcommand; `-C`, `-c` and the dashed forms take a value.
  let i = 0;
  while (i < args.length) {
    const arg = args[i] as string;
    if (arg === "-C" || arg === "-c" || arg === "--git-dir" || arg === "--work-tree" || arg === "--namespace" || arg === "--exec-path") {
      i += 2;
      continue;
    }
    if (arg.startsWith("--") && arg.includes("=")) {
      i += 1;
      continue;
    }
    if (arg.startsWith("-")) {
      i += 1;
      continue;
    }
    break;
  }
  const sub = args[i];
  if (sub === undefined) return true;
  if (GIT_READS.has(sub)) return true;
  const rest = args.slice(i + 1);
  // `git branch` lists; `git branch x` creates; `git remote -v` lists; `git stash` writes.
  if (sub === "branch") return rest.length === 0 || rest.every((arg) => arg.startsWith("-"));
  if (sub === "tag") return rest.length === 0 || rest.every((arg) => arg.startsWith("-"));
  if (sub === "remote") return rest.length === 0 || rest.every((arg) => arg.startsWith("-"));
  if (sub === "stash") return rest[0] === "list" || rest[0] === "show";
  if (sub === "config") {
    if (rest.some((arg) => arg === "--get" || arg === "--get-all" || arg === "-l" || arg === "--list")) return true;
    // `git config user.name` asks; `git config user.name x` sets.
    return rest.filter((arg) => !arg.startsWith("-")).length <= 1;
  }
  return false;
}

/** Inline code cannot be read statically: `node -e`, `python -c`, `osascript -e` can do anything. */
const INLINE_SCRIPT = /\b(python3?|node|deno|bun|perl|ruby|php|lua|osascript|Rscript|pwsh|powershell)\b[^|;&\n]*\s(-c|-e|--eval|-Command)\b/;

export function hasInlineScript(command: string): boolean {
  return INLINE_SCRIPT.test(command);
}

/**
 * Programs whose read-only forms are worth recognising but whose other forms write: when the
 * analysis cannot certify one, the member gate refuses instead of falling through to the blacklist.
 * `git branch -D` and `find -delete` are exactly the shapes a blacklist misses.
 */
export function needsConservativeRefusal(command: string): string | undefined {
  const args = tokens(command.trim());
  if (args === undefined || args.length === 0) return undefined;
  const [program, ...rest] = args as [string, ...string[]];
  const name = program.split("/").pop() ?? program;
  if (!["git", "find", "sed", "awk", "jq"].includes(name)) return undefined;
  if (isReadOnlyCommand(command)) return undefined;
  return `无法确认这条 ${name} 命令只看不改（${rest.slice(0, 3).join(" ").slice(0, 60)}）`;
}

export function isReadOnlyCommand(command: string): boolean {
  const args = tokens(command.trim());
  if (args === undefined || args.length === 0) return false;
  const [program, ...rest] = args as [string, ...string[]];
  const name = program.split("/").pop() ?? program;
  if (name === "sed") return !rest.includes("-i") && !rest.some((arg) => /^-[a-zA-Z]*i[a-zA-Z]*$/.test(arg));
  if (name === "find") return !rest.some((arg) => FIND_WRITES.has(arg.split("=")[0] ?? arg));
  if (name === "git") return gitReads(rest);
  if (name === "xargs" || name === "env" || name === "sudo" || name === "time" || name === "nohup") return false;
  // `echo hi > f` never reaches here (tokens rejects `>`), but `tee` is a writer by nature.
  return READONLY_PROGRAMS.has(name) && !hasInlineScript(command);
}
