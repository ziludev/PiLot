import assert from "node:assert/strict";
import { test } from "node:test";
import { hasInlineScript, isReadOnlyCommand, needsConservativeRefusal } from "../src/readonly.ts";

/**
 * The member gate decides "read only" from the program and its arguments, not from words that
 * happen to appear in the command line. Both directions are pinned here: a reader that mentions
 * `cp` is still a reader, and a `git branch -D` is still a delete.
 */

const reads = [
  "ls -la logs",
  "cat notes.md",
  "cat rm.log",
  "grep -n 'cp' notes.md",
  "grep -rn 'timeout' src/",
  "head -n 40 README.md",
  "sed -n '1,60p' src/feishu.ts",
  "find . -name '*.ts'",
  "find . -type f -newer package.json",
  "git status",
  "git log --oneline -5",
  "git diff --stat",
  "git branch",
  "git branch -a",
  "git remote -v",
  "git stash list",
  "git config user.name",
  "git -C /tmp/x log",
  "jq '.name' package.json",
  "wc -l src/*.ts",
  "awk '{print $1}' logs/agent.log",
  "awk '$3 > 10 {print $1}' data.txt",
  "awk '/foo|bar/ {print $0}' data.txt",
];

const writes = [
  "git branch feat/x",
  "git branch -D old",
  "git commit -m x",
  "git stash",
  "git push origin main",
  "find . -delete",
  "find . -exec rm {} +",
  "sed -i '' 's/a/b/' f.ts",
  "awk '{print > \"pwn.txt\"}' data.txt",
  "awk '{print >> \"pwn.txt\"}' data.txt",
  "awk 'BEGIN { system(\"rm -rf /\") }'",
  "awk -i inplace '{print}' file.txt",
  "awk -f /tmp/pwn.awk data.txt",
  "awk --file=/tmp/pwn.awk data.txt",
  "awk --source '{system(\"id\")}' data.txt",
  "awk 'BEGIN { \"id\" | getline out }'",
  "node -e 'require(\"fs\").writeFileSync(\"a\",\"b\")'",
  "python3 -c 'open(\"x\",\"w\")'",
  "echo hi > f",
  "cat a | tee b",
];

test("readers are recognised, including ones that mention writing words", () => {
  for (const command of reads) assert.equal(isReadOnlyCommand(command), true, `should read: ${command}`);
});

test("non-readers are not", () => {
  for (const command of writes) assert.equal(isReadOnlyCommand(command), false, `should not read: ${command}`);
});

test("a shape the analysis cannot certify is refused conservatively, not blacklisted", () => {
  assert.match(needsConservativeRefusal("git branch -D old") ?? "", /只看不改/);
  assert.match(needsConservativeRefusal("find . -delete") ?? "", /只看不改/);
  assert.equal(needsConservativeRefusal("git status"), undefined);
  assert.equal(needsConservativeRefusal("find . -name '*.ts'"), undefined);
  // Not one of the conservatively-refused programs: the blacklist is enough for it.
  assert.equal(needsConservativeRefusal("mv a b"), undefined);
});

test("inline code is flagged whatever the program", () => {
  assert.equal(hasInlineScript("node -e 'x'"), true);
  assert.equal(hasInlineScript("python3 -c 'x'"), true);
  assert.equal(hasInlineScript("osascript -e 'x'"), true);
  assert.equal(hasInlineScript("node --test tests/a.test.ts"), false);
  assert.equal(hasInlineScript("python3 script.py"), false);
});
