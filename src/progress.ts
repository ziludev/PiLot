/**
 * One tool round in words: what it did, not which tool it was.
 *
 * The card's step trail used to read `✓ bash · 1m12s` — the same four letters whether the round ran
 * the test suite or deleted a directory. The arguments are the only place that says it, and the
 * model commits them with the call, so the trail can name the round from the moment it starts.
 */
type Json = Record<string, unknown>;

const LIMIT = 28;

function lead(value: unknown, limit = LIMIT): string | undefined {
  if (typeof value !== "string") return undefined;
  const text = value.replace(/\s+/g, " ").trim();
  if (text.length === 0) return undefined;
  return text.length > limit ? `${text.slice(0, limit)}…` : text;
}

/** The file name, which is what identifies the file in a one-line trail. */
function file(path: unknown): string | undefined {
  const text = lead(path, 200);
  if (text === undefined) return undefined;
  const parts = text.split("/").filter((part) => part.length > 0);
  return parts.length === 0 ? text : parts[parts.length - 1];
}

/**
 * `name` and `args` as the model sent them, or the bare name — an argument that is not in the
 * window is a round whose verb is unknown, and a guessed verb is worse than no verb when the card
 * is being read to decide whether to stop the run.
 */
export function describeCall(name: string, args: Json | undefined): string {
  const path = args?.["path"] ?? args?.["file_path"];
  const command = lead(args?.["command"]);
  const pattern = lead(args?.["pattern"]);
  const query = lead(args?.["query"]);
  switch (name) {
    case "read":
    case "Read":
      return file(path) === undefined ? name : `读 ${file(path)}`;
    case "write":
    case "Write":
      return file(path) === undefined ? name : `写 ${file(path)}`;
    case "edit":
    case "Edit":
      return file(path) === undefined ? name : `改 ${file(path)}`;
    case "bash":
    case "Bash":
      return command === undefined ? name : `跑 ${command}`;
    case "project_search":
      return pattern === undefined ? name : `搜 ${pattern}`;
    case "logs_query":
      return query === undefined ? name : `查日志 ${query}`;
    case "db_query":
      return "查库";
    case "delegate": {
      const role = lead(args?.["role"], 12);
      if (role === undefined) return name;
      const task = lead(args?.["task"], 20);
      return task === undefined ? `让 ${role} 干活` : `让 ${role}：${task}`;
    }
    case "delegate_parallel": {
      const tasks = args?.["tasks"];
      return Array.isArray(tasks) ? `并行派 ${tasks.length} 个` : name;
    }
    case "build_feature":
      return "派流水线";
    case "recall":
      return query === undefined ? name : `翻记忆 ${query}`;
    default:
      return name;
  }
}
