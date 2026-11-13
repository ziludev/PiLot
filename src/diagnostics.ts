import { execFile } from "node:child_process";
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { promisify } from "node:util";
import { defineExtension, defineTool } from "@earendil-works/pi-durable";
import { num, obj, opt, str } from "@earendil-works/pi-schema";
import { ChatPolicy, readRoots } from "./access.ts";
import { config } from "./config.ts";
import { log, logError } from "./log.ts";
import { record } from "./audit.ts";

/**
 * The two read-only capabilities a colleague needs to ask about a problem — search the code,
 * look at the logs — without a shell. A prompt does not stop `cat .env`, and an allowlist of
 * shell commands does not either (`cat` reaches any path, `git show` any blob), so the blast
 * radius is fixed by the tool instead: one `rg` inside the chat's own project, and the
 * read-only SLS script that already exists on this machine.
 */

const run = promisify(execFile);
const TIMEOUT_MS = 60_000;
const MAX_OUTPUT = 8_000;

/**
 * Logs and database reads go through the presence CLI: it owns the SLS projects and the RDS
 * credentials, and it already refuses writes. Resolving it here (instead of a skill script)
 * keeps the two tools working when a skill is renamed or uninstalled.
 */
function presenceCli(): string | undefined {
  const configured = (process.env["PRESENCE_CLI"] ?? "").trim();
  const candidates = configured.length > 0
    ? [configured.replace(/^~/, homedir())]
    : [join(homedir(), "work/presence-cli/dist/index.js"), join(homedir(), "work/presence-cli/bin/presence")];
  return candidates.find((candidate) => existsSync(candidate));
}

/** psql is not on the agent's PATH on macOS; the CLI shells out to it, so add the usual homes. */
function cliEnv(): NodeJS.ProcessEnv {
  const extra = ["/opt/homebrew/opt/postgresql@17/bin", "/opt/homebrew/bin", "/usr/local/bin"]
    .filter((dir) => existsSync(join(dir, "psql")));
  // The CLI shells out to `node` and `psql`; neither is guaranteed to be on the agent's PATH.
  const path = [process.env["PATH"] ?? "", dirname(process.execPath), ...extra]
    .filter((part) => part.length > 0)
    .join(":");
  return { ...process.env, PATH: path };
}

/** Runs the presence CLI and returns its parsed `data` payload. */
async function presence(args: string[]): Promise<Record<string, unknown>> {
  const cli = presenceCli();
  if (cli === undefined) throw new Error("找不到 presence CLI：设 PRESENCE_CLI，或放到 ~/work/presence-cli/dist/index.js");
  // process.execPath, not "node": the agent can be started with a node that is not on PATH.
  const { stdout } = await run(process.execPath, [cli, ...args, "--json"], {
    timeout: TIMEOUT_MS,
    maxBuffer: 8 * 1024 * 1024,
    env: cliEnv(),
  });
  const parsed = JSON.parse(stdout) as { ok?: boolean; data?: Record<string, unknown>; error?: { message?: string } };
  if (parsed.ok === false) throw new Error(parsed.error?.message ?? "presence CLI 返回失败");
  return parsed.data ?? {};
}

/** Log stores are names like `<prefix>-dev-stdout`; anything else is a typo or an injection attempt. */
const LOGSTORE = /^[a-z0-9][a-z0-9-]{1,60}$/;
const ENVIRONMENTS = ["dev", "prod"] as const;
type Environment = (typeof ENVIRONMENTS)[number];

export type LogsQuery = {
  env: Environment;
  logstore: string;
  query: string;
  minutes: number;
  limit: number;
};

/** Validates and normalizes the arguments; returns why they are refused, or the query. */
export function parseLogsQuery(args: Record<string, unknown>): LogsQuery | string {
  const env = typeof args["env"] === "string" ? args["env"] : "dev";
  if (!ENVIRONMENTS.includes(env as Environment)) return `env 只能是 dev 或 prod，收到 "${env}"`;
  const logstore = typeof args["logstore"] === "string" && args["logstore"].length > 0
    ? args["logstore"]
    : `${process.env.SLS_LOGSTORE_PREFIX ?? "app"}-${env}-stdout`;
  if (!LOGSTORE.test(logstore)) return `logstore 名不合法："${logstore}"`;
  const query = typeof args["query"] === "string" ? args["query"].trim() : "";
  if (query.length === 0) return "query 不能为空";
  if (query.length > 500) return "query 太长（上限 500 字符）";
  const minutes = Math.min(1440, Math.max(1, Math.trunc(Number(args["minutes"] ?? 60)) || 60));
  const limit = Math.min(200, Math.max(1, Math.trunc(Number(args["limit"] ?? 50)) || 50));
  return { env: env as Environment, logstore, query, minutes, limit };
}

/**
 * Production logs are the owner's call. A guest turn is not refused outright — it asks, and the
 * card goes to the owner's DM, so a colleague diagnosing a prod problem does not have to
 * interrupt the conversation to get a human to type the query.
 */
/** Where `project_search` may look: the same roots a guest read uses. */
export function searchRoot(
  args: Record<string, unknown>,
  roots: readonly string[],
): { ok: true; path: string } | { ok: false; reason: string } {
  const base = roots[0] ?? config.workspace;
  const raw = typeof args["path"] === "string" && args["path"].length > 0 ? args["path"] : "";
  const absolute = raw.length === 0 ? base : resolve(base, raw);
  const inside = roots.some((root) => {
    const resolved = resolve(root);
    return absolute === resolved || absolute.startsWith(`${resolved}/`);
  });
  return inside ? { ok: true, path: absolute } : { ok: false, reason: `不能再搜 ${raw}：只允许本会话所属项目和工作区。` };
}

function clip(text: string): string {
  const trimmed = text.trim();
  return trimmed.length <= MAX_OUTPUT ? trimmed : `${trimmed.slice(0, MAX_OUTPUT)}\n… （截断）`;
}

/** One log line: time level service, then the message (the interesting part, so it keeps its room). */
function formatLogItems(items: readonly Record<string, unknown>[]): string {
  return items
    .map((item) => {
      const head = [item["time"], item["level"], item["service"]].filter((part) => typeof part === "string" && part.length > 0).join(" ");
      const message = String(item["message"] ?? "").replace(/\s+/g, " ").slice(0, 600);
      return `${head} ${message}`.trim();
    })
    .join("\n");
}

/** A result set as `a | b` rows: enough to read a balance or a call record off a phone screen. */
function formatRows(columns: readonly string[], items: readonly Record<string, unknown>[]): string {
  if (columns.length === 0) return "(没有列)";
  const header = columns.join(" | ");
  const rows = items.map((item) => columns.map((column) => String(item[column] ?? "")).join(" | "));
  return [header, ...rows].join("\n");
}

const logsQuery = defineTool({
  name: "logs_query",
  description:
    "查阿里云 SLS 日志（只读）。env 选 dev 或 prod，logstore 默认 <prefix>-<env>-stdout（prefix 取 SLS_LOGSTORE_PREFIX，兜底 app），query 用 SLS 查询语法（如 '\"2100083235\"' 或 'severity: ERROR and service: gateway'），minutes 是时间窗口（默认 60）。生产日志也能查：只读，写操作另有代码拦截。",
  parameters: obj({
    query: str({ description: "SLS 查询语句" }),
    env: opt(str({ description: "dev 或 prod，默认 dev" })),
    logstore: opt(str({ description: "logstore 名，默认 <prefix>-<env>-stdout" })),
    minutes: opt(num({ description: "往前查多少分钟，默认 60，上限 1440" })),
    limit: opt(num({ description: "返回条数上限，默认 50，上限 200" })),
  }),
  replay: "safe",
  execute: async (args, api, context) => {
    const parsed = parseLogsQuery(args as Record<string, unknown>);
    if (typeof parsed === "string") return { output: [{ type: "text" as const, text: parsed }] };
    const policy = await api.snapshot(ChatPolicy, api.conversationId, context).catch(() => undefined);
    const guestTurn = policy?.guest === true && policy.turn !== "owner";
    log("logs", "sls query", { env: parsed.env, logstore: parsed.logstore, minutes: parsed.minutes, query: parsed.query });
    // Anyone in a shared group can read prod logs now, so this record stands in for the approval
    // that used to be asked: who asked, which logstore, what they were looking for. One line.
    await record(api, context, {
      kind: "query",
      tool: "logs_query",
      ...(guestTurn ? { who: "guest" } : {}),
      detail: `env=${parsed.env} logstore=${parsed.logstore} ${parsed.minutes}m ${parsed.query.slice(0, 160)}`,
    });
    const header = `[sls] env=${parsed.env} logstore=${parsed.logstore} 近 ${parsed.minutes} 分钟 query=${parsed.query}`;
    try {
      const data = await presence([
        "log",
        "query",
        parsed.env,
        parsed.logstore,
        parsed.query,
        "--minutes",
        String(parsed.minutes),
        "--limit",
        String(parsed.limit),
        "--full",
        ...(parsed.env === "prod" ? ["--yes"] : []),
      ]);
      const items = Array.isArray(data["items"]) ? (data["items"] as Array<Record<string, unknown>>) : [];
      const total = Number(data["total"] ?? items.length);
      const body = items.length === 0 ? "没有匹配的日志。" : formatLogItems(items);
      return { output: [{ type: "text" as const, text: clip(`${header}\ntotal=${total} 显示 ${items.length} 条\n${body}`) }] };
    } catch (error) {
      logError("logs", "sls query failed", error);
      const detail = (error as { stderr?: string; message?: string }).stderr ?? (error as Error).message;
      return { output: [{ type: "text" as const, text: `日志查询失败：${clip(String(detail))}` }] };
    }
  },
});

/** Same two environments as the logs; the CLI calls the dev cluster `test`. */
const DB_ENVIRONMENTS = ["dev", "prod"] as const;
type DbEnvironment = (typeof DB_ENVIRONMENTS)[number];
const DATA_SOURCE = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/;

/** Shapes that write. Checked on the SQL with string literals blanked, so `where kind = 'create'` passes. */
const SQL_WRITE =
  /\b(insert|update|delete|drop|alter|create|truncate|grant|revoke|copy|vacuum|merge|refresh|reindex|comment|call|do|set|begin|commit|rollback|lock|analyze|cluster|listen|notify|prepare|execute|deallocate|discard|import|checkpoint|load|handler)\b/i;
const SQL_READ = /^\s*(select|with|explain|show|table|values)\b/i;

export type DbQuery = {
  env: DbEnvironment;
  cliEnv: "test" | "prod";
  source: string;
  sql: string;
  maxRows: number;
};

function withoutStrings(sql: string): string {
  return sql.replace(/'(?:[^']|'')*'/g, "''").replace(/"(?:[^"\\]|\\.)*"/g, '""');
}

/** Validates and normalizes the arguments; returns why they are refused, or the query. */
export function parseDbQuery(args: Record<string, unknown>): DbQuery | string {
  const env = typeof args["env"] === "string" ? args["env"].trim() : "dev";
  if (!DB_ENVIRONMENTS.includes(env as DbEnvironment)) return `env 只能是 dev 或 prod，收到 "${env}"`;
  const source = typeof args["source"] === "string" && args["source"].length > 0 ? args["source"].trim() : "pgCfg";
  if (!DATA_SOURCE.test(source)) return `数据源名不合法："${source}"`;
  const sql = typeof args["sql"] === "string" ? args["sql"].trim() : "";
  if (sql.length === 0) return "sql 不能为空";
  if (sql.length > 4000) return "sql 太长（上限 4000 字符）";
  if (!SQL_READ.test(sql)) return "只允许查询（SELECT / WITH / EXPLAIN / SHOW）";
  const body = withoutStrings(sql).replace(/;\s*$/, "");
  if (body.includes(";")) return "一次只允许一条语句";
  if (SQL_WRITE.test(body)) return "只允许查询：SQL 里出现了写操作的关锰字";
  const maxRows = Math.min(500, Math.max(1, Math.trunc(Number(args["maxRows"] ?? 100)) || 100));
  return { env: env as DbEnvironment, cliEnv: env === "prod" ? "prod" : "test", source, sql, maxRows };
}

const dbQuery = defineTool({
  name: "db_query",
  description:
    "只读地查数据库（Postgres）。env 选 dev 或 prod（默认 dev，dev 走测试集群），source 是数据源名（默认 pgCfg，见 `presence db list`），sql 只接 SELECT / WITH / EXPLAIN / SHOW，单条语句，maxRows 默认 100（上限 500）。",
  parameters: obj({
    sql: str({ description: "SELECT / WITH / EXPLAIN / SHOW 语句（单条）" }),
    env: opt(str({ description: "dev 或 prod，默认 dev" })),
    source: opt(str({ description: "数据源名，默认 pgCfg" })),
    maxRows: opt(num({ description: "返回行数上限，默认 100，上限 500" })),
  }),
  replay: "safe",
  execute: async (args, api, context) => {
    const parsed = parseDbQuery(args as Record<string, unknown>);
    if (typeof parsed === "string") return { output: [{ type: "text" as const, text: parsed }] };
    const policy = await api.snapshot(ChatPolicy, api.conversationId, context).catch(() => undefined);
    const guestTurn = policy?.guest === true && policy.turn !== "owner";
    log("db", "pg query", { env: parsed.cliEnv, source: parsed.source, chars: parsed.sql.length });
    // One line per query: who asked, where, and what they were looking for.
    await record(api, context, {
      kind: "query",
      tool: "db_query",
      ...(guestTurn ? { who: "guest" } : {}),
      detail: `env=${parsed.env} source=${parsed.source} ${parsed.sql.slice(0, 200)}`,
    });
    const header = `[db] env=${parsed.env} source=${parsed.source} SQL: ${parsed.sql.slice(0, 200)}`;
    try {
      const data = await presence([
        "db",
        "pg",
        parsed.cliEnv,
        parsed.source,
        parsed.sql,
        "--max-rows",
        String(parsed.maxRows),
        ...(parsed.cliEnv === "prod" ? ["--yes"] : []),
      ]);
      const columns = Array.isArray(data["columns"]) ? (data["columns"] as string[]) : [];
      const items = Array.isArray(data["items"]) ? (data["items"] as Array<Record<string, unknown>>) : [];
      const total = Number(data["total"] ?? items.length);
      const body = items.length === 0 ? "(0 行)" : formatRows(columns, items);
      return { output: [{ type: "text" as const, text: clip(`${header}\ntotal=${total} 返回 ${items.length} 行\n${body}`) }] };
    } catch (error) {
      logError("db", "pg query failed", error);
      const detail = (error as { stderr?: string; message?: string }).stderr ?? (error as Error).message;
      return { output: [{ type: "text" as const, text: `数据库查询失败：${clip(String(detail))}` }] };
    }
  },
});

const projectSearch = defineTool({
  name: "project_search",
  description:
    "在代码里搜索（只读，ripgrep）。pattern 是正则；path 默认本会话所属项目的根目录，只能搜这个项目或工作区内部。返回 文件:行号:内容。",
  parameters: obj({
    pattern: str({ description: "搜索的正则或字面量" }),
    path: opt(str({ description: "相对项目根的路径，默认项目根" })),
    max: opt(num({ description: "最多返回多少行匹配，默认 60，上限 200" })),
  }),
  replay: "safe",
  execute: async (args, api, context) => {
    const pattern = typeof args["pattern"] === "string" ? args["pattern"].trim() : "";
    if (pattern.length === 0) return { output: [{ type: "text" as const, text: "pattern 不能为空" }] };
    if (pattern.length > 300) return { output: [{ type: "text" as const, text: "pattern 太长（上限 300 字符）" }] };
    const roots = await readRoots(api, api.conversationId as unknown as number, context);
    const root = searchRoot(args as Record<string, unknown>, roots);
    if (!root.ok) return { output: [{ type: "text" as const, text: root.reason }] };
    const max = Math.min(200, Math.max(1, Math.trunc(Number(args["max"] ?? 60)) || 60));
    try {
      const { stdout } = await run(
        "rg",
        ["--line-number", "--no-heading", "--color", "never", "--max-count", String(max), "--", pattern, root.path],
        { timeout: TIMEOUT_MS, maxBuffer: 4 * 1024 * 1024 },
      );
      log("search", "project search", { pattern, root: root.path, chars: stdout.length });
      await record(api, context, { kind: "query", tool: "project_search", detail: `${pattern} @ ${basename(root.path)}` });
      return { output: [{ type: "text" as const, text: clip(stdout.length === 0 ? `没有匹配「${pattern}」。` : stdout) }] };
    } catch (error) {
      const failed = error as { code?: number; stderr?: string; message?: string };
      // rg exits 1 when nothing matched, which is a result, not a failure.
      if (failed.code === 1) {
        return { output: [{ type: "text" as const, text: `没有匹配「${pattern}」（${basename(root.path)}）。` }] };
      }
      logError("search", "project search failed", error);
      return { output: [{ type: "text" as const, text: `搜索失败：${clip(String(failed.stderr ?? failed.message))}` }] };
    }
  },
});

export const diagnosticsExtension = defineExtension({
  name: "diagnostics",
  tools: [logsQuery, projectSearch, dbQuery],
});
