/**
 * Sampling audit CLI.
 *
 *   node scripts/claims-audit.ts sample --n 2     → print the reports to audit (never the same twice)
 *   node scripts/claims-audit.ts record --task 42 --verdicts /tmp/audit-42.txt
 *   node scripts/claims-audit.ts report           → cumulative error rate
 *
 * The numbers come from the verifier's own file, not from anyone's summary of it.
 */
import { DatabaseSync } from "node:sqlite";
import { readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  EMPTY_STATE,
  auditBrief,
  parseVerdicts,
  renderTally,
  repoFromSpec,
  tally,
  unaudited,
  type AuditState,
  type Report,
} from "../src/claims.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const dbPath = join(root, ".data/agent.sqlite");
const statePath = join(root, ".data/claims-audit.json");

function arg(name: string, fallback?: string): string | undefined {
  const index = process.argv.indexOf(`--${name}`);
  return index === -1 ? fallback : process.argv[index + 1];
}

function loadState(): AuditState {
  try {
    const parsed = JSON.parse(readFileSync(statePath, "utf8")) as AuditState;
    return parsed.version === 1 && Array.isArray(parsed.runs) ? parsed : EMPTY_STATE;
  } catch {
    return EMPTY_STATE;
  }
}

function saveState(state: AuditState): void {
  writeFileSync(statePath, `${JSON.stringify(state, null, 2)}\n`);
}

function finishedReports(): Report[] {
  const db = new DatabaseSync(dbPath, { readOnly: true });
  try {
    const rows = db
      .prepare(
        `select t.id as task_id, t.conversation_id as conv,
                json_extract(t.record, '$.input.spec') as spec,
                json_extract(e.record, '$.data.text') as report
           from tasks t
           join entries e on e.conversation_id = t.conversation_id
          where t.kind in ('app.feature', '"app.feature"')
            and json_extract(e.record, '$.kind') = 'app.feature-report'
          order by e.id desc`,
      )
      .all() as { task_id: number; conv: number; spec: string | null; report: string | null }[];
    const seen = new Set<number>();
    const out: Report[] = [];
    for (const row of rows) {
      if (seen.has(row.task_id)) continue;
      seen.add(row.task_id);
      if (typeof row.spec !== "string" || typeof row.report !== "string") continue;
      const repo = repoFromSpec(row.spec);
      out.push({
        taskId: row.task_id,
        conversationId: row.conv,
        spec: row.spec,
        report: row.report,
        repo: repo === undefined ? undefined : repo.replace(/^~/, homedir()),
      });
    }
    return out;
  } finally {
    db.close();
  }
}

const command = process.argv[2];

if (command === "sample") {
  const want = Number(arg("n", "2"));
  const state = loadState();
  const picked = unaudited(finishedReports(), state, Number.isFinite(want) ? want : 2);
  if (picked.length === 0) {
    console.log("没有新的报告可抽样（都审过了，或最近没有完成的流水线）。");
  }
  for (const item of picked) {
    console.log(`\n===== task ${item.taskId} =====`);
    console.log(auditBrief(item, `/tmp/audit-${item.taskId}.txt`));
    console.log(`\n（已有结论文件时先删掉：rm -f /tmp/audit-${item.taskId}.txt）`);
  }
} else if (command === "record") {
  const taskId = Number(arg("task"));
  const verdictsFile = arg("verdicts");
  if (!Number.isFinite(taskId) || verdictsFile === undefined) {
    console.error("用法：record --task <id> --verdicts <file>");
    process.exit(2);
  }
  const verdicts = parseVerdicts(readFileSync(verdictsFile, "utf8"));
  if (verdicts.length === 0) {
    console.error(`没在 ${verdictsFile} 里找到 CLAIM 行——这次不算完成，别把它当「零错误」。`);
    process.exit(3);
  }
  const state = loadState();
  state.runs = state.runs.filter((run) => run.taskId !== taskId);
  state.runs.push({ taskId, at: new Date().toISOString(), verdicts });
  saveState(state);
  console.log(renderTally(state));
} else if (command === "report") {
  console.log(renderTally(loadState()));
} else {
  const state = loadState();
  const { claims } = tally(state.runs);
  console.log(`用法：sample [--n 2] | record --task <id> --verdicts <file> | report`);
  console.log(`当前已抽 ${state.runs.length} 份 / ${claims} 条断言。`);
}
