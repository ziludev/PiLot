import { execFile } from "node:child_process";
import { readFile, stat } from "node:fs/promises";
import { join } from "node:path";
import { promisify } from "node:util";

/**
 * A stage's verdict comes from a script, not from the paragraph the stage wrote about itself.
 *
 * The pipeline used to trust "我做完了": a child could finish with the tests red, or with nothing on
 * disk at all, and the report still read like success. Every check here is either something git can
 * see or something the filesystem can measure, so a stage that did not do the work fails even when
 * its prose is confident. Checks that a script cannot honestly judge stay soft: they show up in the
 * report as ⚠️ instead of failing work that may be fine.
 */

const run = promisify(execFile);

export type GateStage = "feature-design" | "feature-implement" | "feature-test" | "feature-review";

/** What one file looked like when the stage began. */
type FileState = { path: string; mtimeMs: number; size: number };

export type StageSnapshot = {
  /** False when the cwd is not a git work tree: hard checks degrade to warnings instead of lying. */
  repo: boolean;
  head: string;
  dirty: FileState[];
  at: number;
};

export type GateCheck = {
  name: string;
  ok: boolean;
  /** A hard check fails the stage; a soft one is advice in the report. */
  hard: boolean;
  detail?: string;
};

export type GateResult = {
  stage: GateStage;
  ok: boolean;
  checks: GateCheck[];
  lines: string[];
  /** Paths the stage changed, as git saw them: the scope a review has to look at. */
  touched?: string[];
  /** Wall time the stage took, so a report can say what the run cost. */
  ms?: number;
};

async function git(cwd: string, args: string[]): Promise<string | undefined> {
  try {
    const { stdout } = await run("git", ["-C", cwd, ...args], { timeout: 15_000 });
    return stdout;
  } catch {
    return undefined;
  }
}

/**
 * The tree as it is now: HEAD plus every dirty path with its mtime. `git status` alone cannot tell
 * a file the stage edited from one that was already dirty before it started — this repository
 * normally has a few — so the mtime is what makes "did this stage touch anything" answerable.
 */
export async function collectSnapshot(cwd: string): Promise<StageSnapshot> {
  const at = Date.now();
  const head = await git(cwd, ["rev-parse", "HEAD"]);
  if (head === undefined) return { repo: false, head: "", dirty: [], at };
  // `-uall`: an untracked directory is otherwise reported as `?? docs/`, and the file the
  // stage wrote would never appear in the diff.
  const status = (await git(cwd, ["status", "--porcelain", "--untracked-files=all"])) ?? "";
  const dirty: FileState[] = [];
  for (const line of status.split("\n")) {
    if (line.trim().length === 0) continue;
    let path = line.slice(3).trim();
    if (path.startsWith('"') && path.endsWith('"')) path = path.slice(1, -1);
    if (path.includes(" -> ")) path = path.split(" -> ")[1] ?? path;
    try {
      const info = await stat(join(cwd, path));
      dirty.push({ path, mtimeMs: info.mtimeMs, size: info.size });
    } catch {
      // Deleted files still count as touched; -1 marks "no longer on disk".
      dirty.push({ path, mtimeMs: 0, size: -1 });
    }
  }
  return { repo: true, head: head.trim(), dirty, at };
}

/** Paths a stage changed: its commits since the snapshot, plus dirty files whose mtime/size moved. */
export async function touchedFiles(
  cwd: string,
  before: StageSnapshot,
  after: StageSnapshot,
): Promise<string[]> {
  const touched = new Set<string>();
  if (before.repo && after.repo && before.head.length > 0 && before.head !== after.head) {
    const names = (await git(cwd, ["diff", "--name-only", before.head, after.head])) ?? "";
    for (const name of names.split("\n")) {
      const path = name.trim();
      if (path.length > 0) touched.add(path);
    }
  }
  const known = new Map(before.dirty.map((file) => [file.path, file]));
  for (const file of after.dirty) {
    const old = known.get(file.path);
    if (old === undefined || old.mtimeMs !== file.mtimeMs || old.size !== file.size) touched.add(file.path);
  }
  return [...touched].sort();
}

const TEST_DIR = /(^|\/)(tests?|__tests__|spec)\//i;
const TEST_FILE = /(\.(test|spec)\.[cm]?[jt]sx?$)|(_test\.go$)|(Tests?\.java$)|(^|\/)test_[^/]+\.py$/;

export function isTestPath(path: string): boolean {
  return TEST_DIR.test(path) || TEST_FILE.test(path);
}

export function isDocPath(path: string): boolean {
  return /\.md$/i.test(path);
}

/** The design document's fixed sections: enough of them present means the structure was followed. */
const DESIGN_HEADINGS = ["目标", "非目标", "方案", "数据与接口", "取舍", "风险", "验收标准"];

async function readIfPresent(cwd: string, path: string): Promise<{ size: number; text: string } | undefined> {
  try {
    const info = await stat(join(cwd, path));
    if (!info.isFile()) return undefined;
    const text = await readFile(join(cwd, path), "utf8").catch(() => "");
    return { size: info.size, text };
  } catch {
    return undefined;
  }
}

const CHECK_EVIDENCE = /(exit\s*(0|code\s*0)|BUILD SUCCESS|tests?=\s*\d+|通过|\bpassed\b|\b0 failed\b|\bOK\b|编译成功)/i;
const RUN_SUMMARY = /(通过|失败|passed|failed|tests?=\s*\d+|\d+\s*(个)?用例)/i;
const CITATION = /[\w./-]+\.[A-Za-z0-9]+:\d+/g;
const CITATION_OR_NONE = /(UNKNOWN|无法确认|无法从代码确认|无法核实)/i;
const REVIEW_VERDICT = /(必须改|建议改|通过|没问题)/;
const TESTS_CLEAN = /(通过|passed|\b0 failed\b|no failures?)/i;
const TESTS_RED = /(失败\s*[1-9]|failed\s*[1-9]|\b[1-9]\d*\s*failing)/i;

function citations(text: string): string[] {
  return [...new Set(text.match(CITATION) ?? [])];
}

async function checkDesign(
  cwd: string,
  touched: string[],
  text: string,
): Promise<GateCheck[]> {
  const docs = touched.filter(isDocPath);
  let chosen: { path: string; size: number; text: string } | undefined;
  for (const path of docs) {
    const info = await readIfPresent(cwd, path);
    if (info !== undefined && info.size >= 300) {
      chosen = { path, ...info };
      break;
    }
  }
  // A doc that was cited but landed outside the diff (written earlier, then edited) still counts:
  // the stage's job is a document on disk, not a document in this particular commit.
  if (chosen === undefined) {
    for (const path of citations(text)) {
      const file = path.split(":")[0] ?? "";
      if (!isDocPath(file)) continue;
      const info = await readIfPresent(cwd, file);
      if (info !== undefined && info.size >= 300) {
        chosen = { path: file, ...info };
        break;
      }
    }
  }
  const checks: GateCheck[] = [
    chosen === undefined
      ? {
          name: "设计文档落盘",
          ok: false,
          hard: true,
          detail: docs.length === 0 ? `没有任何 .md 改动（改过：${touched.join(", ") || "无"}）` : `${docs.join(", ")} 存在但不足 300 字`,
        }
      : { name: "设计文档落盘", ok: true, hard: true, detail: `${chosen.path}（${Math.round(chosen.size / 100) / 10}k 字）` },
  ];
  const headings = chosen === undefined ? [] : DESIGN_HEADINGS.filter((heading) => chosen?.text.includes(heading));
  checks.push({
    name: "固定结构",
    ok: headings.length >= 3,
    hard: false,
    detail: `命中 ${headings.length}/${DESIGN_HEADINGS.length} 节${headings.length < 3 ? "，建议按 目标/非目标/方案/数据与接口/取舍/风险/验收标准 写" : ""}`,
  });
  return checks;
}

function checkImplement(touched: string[], text: string, repo: boolean): GateCheck[] {
  const checks: GateCheck[] = [
    {
      name: "代码有改动",
      ok: touched.length > 0 || !repo,
      hard: true,
      detail: touched.length > 0 ? `${touched.length} 个文件` : repo ? "工作区与阶段开始前完全一样" : "不在 git 仓库里，无法核验改动",
    },
  ];
  checks.push({
    name: "汇报带验证输出",
    ok: CHECK_EVIDENCE.test(text),
    hard: false,
    detail: CHECK_EVIDENCE.test(text) ? undefined : "没有看到命令、退出码或汇总行，只有自述",
  });
  return checks;
}

function checkTest(touched: string[], text: string, repo: boolean): GateCheck[] {
  const tests = touched.filter(isTestPath);
  const checks: GateCheck[] = [
    {
      name: "测试文件有改动",
      ok: tests.length > 0 || !repo,
      hard: true,
      detail:
        tests.length > 0
          ? tests.slice(0, 3).join(", ")
          : repo
            ? `没有新增或修改测试文件（改过：${touched.join(", ") || "无"}）`
            : "不在 git 仓库里，无法核验改动",
    },
  ];
  const red = TESTS_RED.test(text);
  checks.push({
    name: "运行结果已汇报",
    ok: RUN_SUMMARY.test(text) && !red,
    hard: false,
    detail: red ? "汇报里有失败用例" : RUN_SUMMARY.test(text) ? undefined : "没有贴通过/失败数",
  });
  if (!red && TESTS_CLEAN.test(text) === false) {
    checks.push({ name: "测试是绿的", ok: false, hard: false, detail: "没有看到明确的通过结论" });
  }
  return checks;
}

function checkReview(text: string): GateCheck[] {
  const cites = citations(text);
  const verdict = REVIEW_VERDICT.test(text);
  const claimsClean = /(通过|没问题)/.test(text) && !/(必须改)/.test(text);
  return [
    {
      // Flagging a problem without saying where it is leaves the next reader guessing, so that one
      // is hard; a review that says "通过" needs no citations to be usable.
      name: "问题带 文件:行",
      ok: cites.length > 0 || claimsClean,
      hard: !claimsClean,
      detail: cites.length > 0 ? `${cites.slice(0, 3).join(" ")}${cites.length > 3 ? " …" : ""}` : "没有一处位置引用",
    },
    {
      name: "无法确认的列为 UNKNOWN",
      ok: CITATION_OR_NONE.test(text),
      hard: false,
      detail: CITATION_OR_NONE.test(text) ? undefined : "建议显式写「无法从代码确认」的清单，别用推测填满",
    },
    { name: "有明确结论", ok: verdict, hard: false, detail: verdict ? undefined : "没有看到 必须改/建议改/通过" },
  ];
}

export async function evaluateGate(
  stage: GateStage,
  input: { cwd: string; touched: string[]; text: string; repo: boolean },
): Promise<GateCheck[]> {
  switch (stage) {
    case "feature-design":
      return checkDesign(input.cwd, input.touched, input.text);
    case "feature-implement":
      return checkImplement(input.touched, input.text, input.repo);
    case "feature-test":
      return checkTest(input.touched, input.text, input.repo);
    case "feature-review":
      return checkReview(input.text);
  }
}

function line(check: GateCheck): string {
  return `${check.ok ? "✅" : check.hard ? "❌" : "⚠️"} ${check.name}${check.detail === undefined ? "" : `：${check.detail}`}`;
}

export function renderGate(result: GateResult): string {
  return `gate ${result.stage.replace("feature-", "")} ${result.ok ? "通过" : "未通过"}\n${result.lines.join("\n")}`;
}

async function runOnce(
  stage: GateStage,
  cwd: string,
  before: StageSnapshot,
  text: string,
): Promise<GateResult> {
  const after = await collectSnapshot(cwd);
  const touched = await touchedFiles(cwd, before, after);
  const checks = await evaluateGate(stage, { cwd, touched, text, repo: before.repo && after.repo });
  return {
    stage,
    ok: checks.every((check) => !check.hard || check.ok),
    checks,
    lines: checks.map(line),
    touched,
  };
}

/** What a stage is told when its own work fails the script's checks. */
export function repairMessage(result: GateResult): string {
  const failed = result.checks.filter((check) => check.hard && !check.ok);
  return `脚本核验没过：\n${failed.map((check) => `- ${check.name}${check.detail === undefined ? "" : `：${check.detail}`}`).join("\n")}\n把缺的部分补上，再按原来的汇报格式答一次；已经做完的部分不要重做。`;
}

/**
 * Gate one finished stage, with a single repair round: a stage that missed an artefact is usually
 * one message away from adding it, and failing the whole pipeline for that would be the gate being
 * wrong about what it knows.
 */
export async function gateStage(options: {
  stage: GateStage;
  cwd: string;
  before: StageSnapshot;
  text: string;
  ask?: (message: string) => Promise<string | undefined>;
}): Promise<{ result: GateResult; text: string }> {
  let text = options.text;
  let result = await runOnce(options.stage, options.cwd, options.before, text);
  if (!result.ok && options.ask !== undefined) {
    const more = await options.ask(repairMessage(result)).catch(() => undefined);
    if (more !== undefined && more.trim().length > 0) {
      text = more;
      result = await runOnce(options.stage, options.cwd, options.before, text);
    }
  }
  return { result, text };
}
