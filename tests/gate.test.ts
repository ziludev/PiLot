import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtemp, mkdir, writeFile, appendFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, test } from "node:test";
import {
  collectSnapshot,
  evaluateGate,
  gateStage,
  isTestPath,
  repairMessage,
  touchedFiles,
} from "../src/gate.ts";

const dirs: string[] = [];

async function repo(): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), "gate-"));
  dirs.push(dir);
  execFileSync("git", ["init", "-q", dir]);
  await writeFile(join(dir, "README.md"), "hello\n");
  execFileSync("git", ["-C", dir, "add", "-A"]);
  execFileSync("git", ["-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init"]);
  return dir;
}

after(async () => {
  for (const dir of dirs) await rm(dir, { recursive: true, force: true });
});

const longDoc = `# 方案\n\n## 目标\nx\n## 非目标\ny\n## 方案\nz\n## 风险\n${"细节。".repeat(200)}\n`;

test("a design stage that wrote no document fails the gate", async () => {
  const dir = await repo();
  const before = await collectSnapshot(dir);
  const after = await collectSnapshot(dir);
  const checks = await evaluateGate("feature-design", {
    cwd: dir,
    touched: await touchedFiles(dir, before, after),
    text: "设计文档已写好，路径 docs/plan.md",
    repo: true,
  });
  const landing = checks.find((check) => check.name === "设计文档落盘");
  assert.equal(landing?.ok, false);
  assert.equal(landing?.hard, true);
});

test("a design stage passes on the document it actually wrote", async () => {
  const dir = await repo();
  const before = await collectSnapshot(dir);
  await mkdir(join(dir, "docs"), { recursive: true });
  await writeFile(join(dir, "docs", "plan.md"), longDoc);
  const after = await collectSnapshot(dir);
  const touched = await touchedFiles(dir, before, after);
  assert.deepEqual(touched, ["docs/plan.md"]);
  const checks = await evaluateGate("feature-design", { cwd: dir, touched, text: "docs/plan.md", repo: true });
  assert.equal(checks.find((check) => check.name === "设计文档落盘")?.ok, true);
  assert.equal(checks.find((check) => check.name === "固定结构")?.ok, true);
});

test("an implement stage that changed nothing fails even when it says it is done", async () => {
  const dir = await repo();
  const before = await collectSnapshot(dir);
  const after = await collectSnapshot(dir);
  const checks = await evaluateGate("feature-implement", {
    cwd: dir,
    touched: await touchedFiles(dir, before, after),
    text: "已实现全部需求，构建通过。",
    repo: true,
  });
  const changed = checks.find((check) => check.name === "代码有改动");
  assert.equal(changed?.ok, false);
  assert.equal(changed?.hard, true);
});

test("an edit to a file that was already dirty still counts as a change", async () => {
  // The workspace normally has a few dirty files. "Same git status as before" would read such an
  // edit as no work at all, so the snapshot compares mtime and size as well.
  const dir = await repo();
  await writeFile(join(dir, "dirty.txt"), "one\n");
  const before = await collectSnapshot(dir);
  await appendFile(join(dir, "dirty.txt"), "two\n");
  const after = await collectSnapshot(dir);
  const touched = await touchedFiles(dir, before, after);
  assert.deepEqual(touched, ["dirty.txt"]);
});

test("a commit made by the stage counts as a change", async () => {
  const dir = await repo();
  const before = await collectSnapshot(dir);
  await writeFile(join(dir, "src.ts"), "export const a = 1;\n");
  execFileSync("git", ["-C", dir, "add", "-A"]);
  execFileSync("git", ["-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "work"]);
  const after = await collectSnapshot(dir);
  assert.deepEqual(await touchedFiles(dir, before, after), ["src.ts"]);
});

test("a test stage must land a test file, not just say it ran one", async () => {
  const dir = await repo();
  const before = await collectSnapshot(dir);
  await writeFile(join(dir, "src.ts"), "export const a = 1;\n");
  const after = await collectSnapshot(dir);
  const touched = await touchedFiles(dir, before, after);
  const checks = await evaluateGate("feature-test", { cwd: dir, touched, text: "12 个用例全部通过", repo: true });
  const landed = checks.find((check) => check.name === "测试文件有改动");
  assert.equal(landed?.ok, false);
  assert.equal(landed?.hard, true);
  assert.equal(checks.find((check) => check.name === "运行结果已汇报")?.ok, true);
});

test("a red test run is reported instead of passing silently", async () => {
  const dir = await repo();
  const checks = await evaluateGate("feature-test", {
    cwd: dir,
    touched: ["tests/gate.test.ts"],
    text: "npm test：通过 11，失败 2",
    repo: true,
  });
  assert.equal(checks.find((check) => check.name === "运行结果已汇报")?.ok, false);
});

test("a review that flags problems without a location fails", async () => {
  const dir = await repo();
  const bare = await evaluateGate("feature-review", {
    cwd: dir,
    touched: ["src/a.ts"],
    text: "必须改：这里的错误处理不对，另外名字也不好。",
    repo: true,
  });
  assert.equal(bare.find((check) => check.name === "问题带 文件:行")?.ok, false);
  assert.equal(bare.find((check) => check.name === "问题带 文件:行")?.hard, true);

  const cited = await evaluateGate("feature-review", {
    cwd: dir,
    touched: ["src/a.ts"],
    text: "必须改：src/a.ts:12 的错误分支没有返回值。UNKNOWN：线上配置没读到。",
    repo: true,
  });
  assert.equal(cited.find((check) => check.name === "问题带 文件:行")?.ok, true);
  assert.equal(cited.find((check) => check.name === "无法确认的列为 UNKNOWN")?.ok, true);
});

test("a review that says the change is fine needs no citations", async () => {
  const dir = await repo();
  const checks = await evaluateGate("feature-review", {
    cwd: dir,
    touched: ["src/a.ts"],
    text: "看着没问题，通过。",
    repo: true,
  });
  assert.equal(checks.find((check) => check.name === "问题带 文件:行")?.hard, false);
  assert.equal(checks.every((check) => !check.hard || check.ok), true);
});

test("outside a git repo the hard checks step back instead of failing the stage", async () => {
  const dir = await mkdtemp(join(tmpdir(), "gate-nogit-"));
  dirs.push(dir);
  const before = await collectSnapshot(dir);
  assert.equal(before.repo, false);
  const checks = await evaluateGate("feature-implement", { cwd: dir, touched: [], text: "done", repo: false });
  assert.equal(checks.find((check) => check.name === "代码有改动")?.ok, true);
  assert.match(checks.find((check) => check.name === "代码有改动")?.detail ?? "", /无法核验/);
});

test("a stage that missed its artefact gets one repair round and is then judged again", async () => {
  const dir = await repo();
  const before = await collectSnapshot(dir);
  let asked = "";
  const result = await gateStage({
    stage: "feature-design",
    cwd: dir,
    before,
    text: "文档在 docs/plan.md",
    ask: async (message) => {
      asked = message;
      await mkdir(join(dir, "docs"), { recursive: true });
      await writeFile(join(dir, "docs", "plan.md"), longDoc);
      return "补好了：docs/plan.md";
    },
  });
  assert.match(asked, /设计文档落盘/);
  assert.equal(result.result.ok, true);
  assert.match(result.text, /补好了/);
});

test("the repair message names only the hard failures", async () => {
  const dir = await repo();
  const before = await collectSnapshot(dir);
  const result = await gateStage({ stage: "feature-implement", cwd: dir, before, text: "都做完了" });
  assert.equal(result.result.ok, false);
  const message = repairMessage(result.result);
  assert.match(message, /代码有改动/);
  assert.doesNotMatch(message, /汇报带验证输出/);
});

test("test paths are recognised in the conventions this project and the JVM one use", () => {
  for (const path of ["tests/gate.test.ts", "src/a.test.ts", "internal/x_test.go", "src/test/java/A.java"]) {
    assert.equal(isTestPath(path), true, path);
  }
  for (const path of ["src/gate.ts", "docs/plan.md", "src/testable.ts"]) {
    assert.equal(isTestPath(path), false, path);
  }
});
