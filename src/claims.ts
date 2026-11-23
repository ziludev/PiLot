/**
 * Sampling audit: a claim that nobody checks is a claim, not a fact.
 *
 * The pipeline now gates each stage on what landed, but nothing measures how often the finished
 * reports are *wrong*. This module holds the pieces: pull finished reports, hand them to a fresh
 * verifier that has no memory of producing them, and tally the verdicts it writes back.
 */

export type ClaimStatus = "GROUNDED" | "WRONG" | "UNKNOWN";

export type Verdict = {
  claim: number;
  status: ClaimStatus;
  why: string;
};

export type AuditRun = {
  taskId: number;
  at: string;
  verdicts: Verdict[];
};

export type AuditState = {
  version: 1;
  runs: AuditRun[];
};

export type Report = {
  taskId: number;
  conversationId: number;
  spec: string;
  report: string;
  /** Repo the report claims to be about, when the spec names one. */
  repo?: string;
};

export const EMPTY_STATE: AuditState = { version: 1, runs: [] };

/** One verdict per line: `CLAIM 3 GROUNDED src/server.ts:412` — a numbered line nobody has to read twice. */
const VERDICT_LINE = /^\s*CLAIM\s+(\d+)\s+(GROUNDED|WRONG|UNKNOWN)\b\s*(.*)$/i;

export function parseVerdicts(text: string): Verdict[] {
  const verdicts: Verdict[] = [];
  for (const line of text.split("\n")) {
    const match = VERDICT_LINE.exec(line);
    if (match === null) continue;
    verdicts.push({
      claim: Number(match[1]),
      status: match[2]!.toUpperCase() as ClaimStatus,
      why: match[3]!.trim(),
    });
  }
  return verdicts.sort((a, b) => a.claim - b.claim);
}

/**
 * The report says what should be true; the repo says what is. A path in the spec is the only hint we
 * have of where to look, so take the first absolute or home-relative one.
 */
export function repoFromSpec(spec: string): string | undefined {
  const match = /(?:^|[\s(（`"])((?:~|Users)\/[^\s,，。)）`"：:]+|\/Users\/[^\s,，。)）`"：:]+)/.exec(spec);
  return match?.[1]?.replace(/[./]+$/, "");
}

export function tally(runs: AuditRun[]): { claims: number; grounded: number; wrong: number; unknown: number; rate: number } {
  const all = runs.flatMap((run) => run.verdicts);
  const grounded = all.filter((v) => v.status === "GROUNDED").length;
  const wrong = all.filter((v) => v.status === "WRONG").length;
  const unknown = all.filter((v) => v.status === "UNKNOWN").length;
  const claims = all.length;
  const judged = grounded + wrong;
  return { claims, grounded, wrong, unknown, rate: judged === 0 ? 0 : wrong / judged };
}

export function unaudited(reports: Report[], state: AuditState, want: number): Report[] {
  const seen = new Set(state.runs.map((run) => run.taskId));
  return reports.filter((item) => !seen.has(item.taskId)).slice(0, want);
}

/** What the verifier is handed: the claims to check and where to look. No word from the producers. */
export function auditBrief(report: Report, verdictFile: string): string {
  const repo = report.repo ?? "（spec 里没写仓库，自己从正文里找线索，找不到就全判 UNKNOWN）";
  return [
    `工作仓库：${repo}`,
    "",
    "需求原文：",
    report.spec,
    "",
    "被审的报告（另一个人写的，它自己认为已经完成）：",
    report.report,
    "",
    "你的任务：把报告里每一条**关于代码/数据/构建状态的断言**编上号，然后逐条到仓库里核实。",
    "不要相信报告里的任何「已验证」「测试通过」，只认你在文件里看到的。",
    "",
    "每条判一个：GROUNDED（能在代码里指到）、WRONG（代码里不是这样）、UNKNOWN（现在查不到，比如分支已变、文件已删）。",
    `把结果逐行写成 \`CLAIM <编号> <GROUNDED|WRONG|UNKNOWN> <文件:行 或一句话理由>\`，追加到 ${verdictFile}（用 bash 写文件）。`,
    "GROUNDED 和 WRONG 都必须带 文件:行。最后再回一段话总结：一共几条、错在哪类。",
  ].join("\n");
}

export function renderTally(state: AuditState): string {
  const { claims, grounded, wrong, unknown, rate } = tally(state.runs);
  if (claims === 0) return "还没有抽样记录。";
  const lines = [
    `累计抽样 ${state.runs.length} 份报告、${claims} 条断言`,
    `有依据 ${grounded}、错 ${wrong}、查不到 ${unknown}`,
    `错误率 ${(rate * 100).toFixed(1)}%（错 /（有依据 + 错））`,
  ];
  const recent = state.runs.slice(-3).map((run) => {
    const wrongs = run.verdicts.filter((v) => v.status === "WRONG");
    const head = `- task ${run.taskId}（${run.at.slice(0, 10)}）${run.verdicts.length} 条，错 ${wrongs.length}`;
    return wrongs.length === 0 ? head : `${head}\n${wrongs.map((v) => `  · #${v.claim} ${v.why}`).join("\n")}`;
  });
  return [...lines, "", ...recent].join("\n");
}
