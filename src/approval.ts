import { basename, isAbsolute, relative, resolve } from "node:path";
import { defineExtension, hook, ToolTask } from "@earendil-works/pi-durable";
import { ChatPolicy, readRoots } from "./access.ts";
import { hasInlineScript, isReadOnlyCommand, needsConservativeRefusal } from "./readonly.ts";
import { config } from "./config.ts";
import { requestApproval, surfaceForConversation } from "./channel.ts";
import { backupFile } from "./checkpoint.ts";
import { hasGrant } from "./grants.ts";
import { getHarness, getSessionContext } from "./runtime-ref.ts";
import { APPROVAL_EVENT, noteTaskEvent } from "./task-events.ts";

/**
 * Commands that can destroy work or reach outside the sandbox. Kept short on purpose.
 * `key` is the risk class a conversation-wide grant is remembered under, so "allow for
 * this chat" covers the same kind of call and nothing else.
 */
const RISKY: Array<{ pattern: RegExp; reason: string; key: string }> = [
  // Long options and a flag after the path count too: `rm --recursive --force` and
  // `rm x -rf` destroy the same files as `rm -rf x`.
  { pattern: /\brm\s+([^|;&\n]*\s)?(-[a-zA-Z]*[rf]|--recursive\b|--force\b)/, reason: "删除文件", key: "rm" },
  { pattern: /\bfind\b[^|;&\n]*\s-delete\b/, reason: "批量删除文件", key: "rm" },
  { pattern: /\bgit\s+push\b.*(--force|-f\b)/, reason: "强推远端", key: "force-push" },
  // `git push origin +main` rewrites the remote branch without the word force.
  { pattern: /\bgit\s+push\b[^|;&\n]*\s\+[A-Za-z0-9._/-]+/, reason: "强推远端", key: "force-push" },
  { pattern: /\bgit\s+reset\s+--hard\b/, reason: "丢弃本地改动", key: "reset-hard" },
  { pattern: /\bgit\s+clean\s+-(?![a-zA-Z]*n)[a-zA-Z]*[fd]/, reason: "删除未跟踪文件", key: "git-clean" },
  { pattern: /\bsudo\b/, reason: "提权执行", key: "sudo" },
  { pattern: /\b(mkfs|diskutil\s+erase|dd\s+if=)/, reason: "写磁盘", key: "disk" },
  { pattern: />\s*\/dev\/(sd|disk|rdisk)/, reason: "写裸设备", key: "raw-device" },
  { pattern: /\bchmod\s+(-R\s+)?(0?777|[ugoa]*\+rwx)\b/, reason: "放开文件权限", key: "chmod-777" },
  { pattern: /\b(curl|wget)\b[^|]*\|\s*(ba|z)?sh\b/, reason: "把网络内容直接交给 shell", key: "pipe-to-shell" },
  { pattern: /\bnpm\s+publish\b/, reason: "发布到 npm", key: "npm-publish" },
  { pattern: /\bgh\s+(pr|release)\b.*(merge|create|delete)\b/, reason: "操作 GitHub 远端", key: "gh-write" },
  { pattern: /\bshutdown\b|\breboot\b/, reason: "关机/重启", key: "power" },
];

const WRITE_TOOLS = new Set(["write", "edit"]);

/**
 * MCP 浏览器工具里，只有这几个是「看一眼」：快照、控制台、网络记录、截图、查找。
 * 其余（导航、点击、输入、填表、上传）都能把工作区里的东西送到外网去，或者替人在别人
 * 的系统里按下按钮——对群成员来说和 `git push` 是一个量级，所以硬拒绝。
 */
const BROWSER_READS = new Set([
  "browser_snapshot",
  "browser_find",
  "browser_console_messages",
  "browser_network_requests",
  "browser_network_request",
  "browser_take_screenshot",
  "browser_tabs",
]);

/**
 * 群成员的写操作：拒绝，不弹卡。
 *
 * 审批卡是「alex 不在场时替他按一下确认」用的；同事要改代码，得他自己决定，一张卡放在群里
 * 只会让所有人等十分钟然后超时。密钥更要紧：配置文件里的 key 一旦被读进群聊就是泄露，
 * 没法撤回。所以这一档是硬拒绝，不是审批——代价是偶尔误判（同事跑了个带重定向的读命令），
 * 那时他会一句话让 alex 来做，比泄露便宜。
 */
const WRITE_COMMANDS: Array<{ pattern: RegExp; reason: string }> = [
  { pattern: /\b(rm|rmdir|mv|cp|mkdir|touch|chmod|chown|ln|truncate|tee|dd)\b/, reason: "改文件" },
  { pattern: /\bsed\s+-i/, reason: "就地改文件" },
  { pattern: /\bgit\s+(add|commit|push|pull|fetch|merge|rebase|reset|checkout|switch|restore|stash|tag|cherry-pick|clean|rm|mv|apply|am|init|clone)\b/, reason: "改仓库" },
  { pattern: /\b(npm|pnpm|yarn|bun)\s+(i|install|add|remove|uninstall|publish|link|update)\b/, reason: "装依赖/发包" },
  { pattern: /\b(pip|pip3|python3?\s+-m\s+pip)\s+install\b/, reason: "装依赖" },
  { pattern: /\bgo\s+(get|install|mod\s+tidy)\b/, reason: "装依赖" },
  { pattern: /\b(curl|wget)\b[^|;&]*\s(-o\s|-O\b|--output)/, reason: "往磁盘写文件" },
  { pattern: /\b(mysql|psql|sqlite3)\b[^|;&]*\b(insert|update|delete|drop|alter|create|replace|vacuum)\b/i, reason: "改数据库" },
  { pattern: /\b(kubectl|helm)\b[^|;&]*\b(apply|delete|patch|edit|scale|rollout|upgrade|install)\b/, reason: "动集群" },
  { pattern: /\b(docker|docker-compose)\b[^|;&]*\b(rm|stop|kill|down|up|run|build|push)\b/, reason: "动容器" },
  { pattern: /\b(kill|killall|pkill)\b/, reason: "停进程" },
  // 重定向写文件；`2>/dev/null` 和 `&>` 里那个 `>` 前面是数字或 &，不算写。
  { pattern: /(?<![\d&])>{1,2}\s*(?!\/dev\/null\b|&)[^\s|;&]+/, reason: "写文件" },
];

/** 库里的列名：password / api_key 这类查出来就等于公开了。 */
const SECRET_COLUMN = /(password|passwd|secret|api[_-]?key|private[_-]?key|access[_-]?key|credential|token_secret)/i;

/** 密钥、凭据、记忆库：看路径就能判定的那些，读也不行。 */
export const SECRET_PATH =
  /(^|[\s"'=:(/,])(\.env(\.[\w-]+)?|\.npmrc|\.netrc|\.git-credentials|\.ssh|id_(rsa|dsa|ecdsa|ed25519)|[^\s/]*\.(pem|key|p12|pfx|keystore)|credentials(\.json)?|secrets?\.(json|ya?ml|toml)|agent\.sqlite)(?=$|[\s"'),;|&])/i;

/** Why a member's call is refused outright — no card, no waiting. */
export function memberBlock(tool: string, args: Record<string, unknown>): string | undefined {
  const path = typeof args["path"] === "string" ? args["path"] : "";
  if (path.length > 0 && SECRET_PATH.test(path)) return "这个文件里有密钥";
  if (WRITE_TOOLS.has(tool)) return `改文件（${path.length > 0 ? path : tool}）`;
  if (EXTERNAL_WRITES.has(tool)) return "写飞书";
  // 库里的密码列：查得出来的和说出来的是一回事。
  if (tool === "db_query" && typeof args["sql"] === "string" && SECRET_COLUMN.test(args["sql"])) {
    return "查密钥字段";
  }
  const browser = /^mcp__playwright__(.+)$/.exec(tool);
  if (browser !== null && BROWSER_READS.has(browser[1] ?? "") === false) return "动浏览器";
  if (tool !== "bash") return undefined;
  const command = typeof args["command"] === "string" ? args["command"] : "";
  if (command.length === 0) return undefined;
  if (SECRET_PATH.test(command)) return "这个命令会碰到密钥文件";
  // 内联脚本没有参数可分析：`node -e`、`python -c` 里一行就是任意写操作。
  if (hasInlineScript(command)) return "执行内联脚本";
  // 判成只读就放行，不必靠黑名单猜：`grep "cp" notes.md` 不写任何东西，
  // 而名单里出现词根从来不是写操作的证据。
  if (isReadOnlyCommand(command)) return undefined;
  for (const rule of WRITE_COMMANDS) {
    const hit = rule.pattern.exec(command);
    if (hit !== null) return `${rule.reason}（${hit[0].trim().slice(0, 40)}）`;
  }
  // 黑名单没覆盖、也分析不出只读的：`git branch -D`、`find -delete` 这类，拒。
  return needsConservativeRefusal(command);
}

/** Tools that change something outside the workspace, in someone else's system. */
const EXTERNAL_WRITES = new Set(["feishu_create_task"]);

function insideRoot(target: string, root: string): boolean {
  const rel = relative(resolve(root), resolve(target));
  return rel === "" || (!rel.startsWith("..") && !isAbsolute(rel));
}

function outsideWorkspace(path: string, roots: readonly string[] = [config.workspace]): boolean {
  const base = roots[0] ?? config.workspace;
  const absolute = isAbsolute(path) ? path : resolve(base, path);
  return !roots.some((root) => insideRoot(absolute, root));
}

/**
 * The two-step install: nothing is piped, so `| sh` never matches — a file lands and then runs.
 * Matching runs on basenames because a download to `i.sh` and `sh ./i.sh` are the same act, and
 * on both sides of `&&`, `;` and newlines because the shell does not care which came first.
 *
 * `curl -O` and plain `wget` name the file after the URL, while `curl -o` / `wget -O` name it
 * themselves; the two tools disagree about that letter, so each pattern stays with its tool.
 */
export function downloadThenRun(command: string): string | undefined {
  const downloaded: string[] = [];
  const push = (names: Iterable<string>): void => {
    for (const name of names) if (name.length > 0 && name !== "." && name !== "/") downloaded.push(name);
  };
  const explicit = (pattern: RegExp): void => {
    for (const match of command.matchAll(pattern)) {
      const target = match[2];
      if (target !== undefined) push([basename(target)]);
    }
  };
  const fromUrls = (pattern: RegExp): void => {
    for (const match of command.matchAll(pattern)) {
      const path = (match[1] ?? "").split(/[?#]/)[0];
      if (path !== undefined) push([basename(path)]);
    }
  };

  explicit(/(?:curl|wget)\b[^\n]*?(?:--output[= ]\s*|--output-document[= ]\s*|-o\s+|-O\s+|>\s*)(["']?)([^\s"'|;&]+)\1/g);
  // curl -O keeps the URL's basename; a bare wget does the same thing by default.
  if (/(?:curl|wget)\b[^\n]*?(?:\s-O\b|--remote-name\b)/.test(command)) {
    fromUrls(/(?:curl|wget)\b[^\n]*?(https?:\/\/[^\s"'|;&]+)/g);
  }
  if (/\bwget\b/.test(command) && !/\bwget\b[^\n]*?(?:-O\s+|--output-document[= ])/.test(command)) {
    fromUrls(/\bwget\b[^\n]*?(https?:\/\/[^\s"'|;&]+)/g);
  }
  if (downloaded.length === 0) return undefined;

  const executed: string[] = [];
  for (const match of command.matchAll(/\b(?:(?:ba|z|k)?sh|source|\.)\s+([^\s|;&]+)/g)) {
    const path = match[1];
    if (path !== undefined) executed.push(basename(path));
  }
  for (const match of command.matchAll(/\bchmod\s+[^\n]*?\b([^\s|;&]+)\s*&&\s*([^\s|;&]+)/g)) {
    const path = match[2];
    if (path !== undefined) executed.push(basename(path));
  }
  const hit = executed.find((name) => downloaded.includes(name));
  return hit === undefined ? undefined : `下载后直接执行（${hit}）：${command.slice(0, 120)}`;
}

type ApprovalNeed = { reason: string; key: string };

/** Returns why a call needs a human, or undefined when it may run unattended. */
export function approvalReason(
  tool: string,
  args: Record<string, unknown>,
  allowedRoots: readonly string[] = [config.workspace],
): ApprovalNeed | undefined {
  if (tool === "bash" && typeof args["command"] === "string") {
    const command = args["command"];
    // Not in RISKY: this one needs the whole command line, not one pattern.
    const twoStep = downloadThenRun(command);
    if (twoStep !== undefined) return { reason: twoStep, key: "pipe-to-shell" };
    for (const risky of RISKY) {
      if (risky.pattern.test(command)) return { reason: `${risky.reason}：${command.slice(0, 120)}`, key: risky.key };
    }
    return undefined;
  }
  if (EXTERNAL_WRITES.has(tool)) {
    return { reason: `会写入你的飞书：${JSON.stringify(args).slice(0, 200)}`, key: "feishu-write" };
  }
  if (WRITE_TOOLS.has(tool)) {
    const path = typeof args["path"] === "string" ? args["path"] : "";
    if (path.length > 0 && outsideWorkspace(path, allowedRoots)) {
      return { reason: `写到工作区之外：${path}`, key: "write-outside" };
    }
  }
  return undefined;
}

/**
 * 成员轮的完整规则：上面那些硬拒绝，加上「本来要人力确认的也一律不批」——群里没人会去点卡，
 * 弹出来只是让同事等十分钟然后失败。
 */
export function memberRefusal(tool: string, args: Record<string, unknown>): string | undefined {
  const blocked = memberBlock(tool, args);
  if (blocked !== undefined) return blocked;
  return approvalReason(tool, args)?.reason;
}

/** Long enough to answer from a meeting; the run is blocked either way. */
const APPROVAL_TIMEOUT_MS = 600_000;

export const guardExtension = defineExtension({
  name: "guard",
  hooks: [
    hook(ToolTask, {
      beforeTool: async (call, api, context) => {
        const args = (call.arguments ?? {}) as Record<string, unknown>;
        // 群里的人查得到东西，但改不了东西：写操作和密钥一律当场拒绝，不弹卡。
        // 缺省（没写过 turn 的会话，比如客户端里的）不按成员处理——宁可放过，也不能让 alex
        // 自己在客户端里被挡在门外。
        const policy = await api.snapshot(ChatPolicy, api.conversationId, context).catch(() => undefined);
        if (policy?.turn === "member") {
          const denied = memberRefusal(call.name, args);
          if (denied !== undefined) {
            const owner = config.feishu.ownerName;
            console.log(`[guard] refused on a member turn: ${call.name} ${denied}`);
            void noteTaskEvent(
              getHarness(),
              getSessionContext(),
              api.conversationId as unknown as number,
              api.taskId as unknown as number,
              APPROVAL_EVENT,
              `${call.name}：${denied} → 已拒绝（群里不批写操作）`,
            );
            return { block: `${denied}——这一步只有 ${owner} 能做。跟提问的人说一句就好，不要绕过。` };
          }
        }
        // 改之前先留一份原件：git 管不到的文件（笔记、配置、一次性脚本）改坏了没有别的退路。
        // 放在审批之前，拒绝的那次也留了底，但只对真正要写的工具做。
        if (WRITE_TOOLS.has(call.name)) {
          const path = typeof args["path"] === "string" ? args["path"] : "";
          if (path.length > 0) {
            const backup = backupFile(path);
            if (backup !== undefined) console.log(`[guard] checkpoint ${path} -> ${backup}`);
          }
        }
        // One surface the rest of the way: what a call *does* matters, so the risk list below
        // (and the approval card it triggers) is the gate — the owner's own turns go through it.
        const allowedRoots = WRITE_TOOLS.has(call.name)
          ? await readRoots(api, api.conversationId as unknown as number, context).catch(() => [config.workspace])
          : [config.workspace];
        const need = approvalReason(call.name, args, allowedRoots);
        if (need === undefined) return undefined;
        const { reason, key } = need;
        // "本会话内全部允许" already covered this class of call.
        if (hasGrant(api.conversationId as unknown as number, key)) {
          console.log(`[guard] ${call.name} allowed by session grant (${key})`);
          return undefined;
        }

        const memoKey = `approval:${call.id}`;
        // A hook reruns after a crash: reuse the stored answer instead of asking twice.
        const decided = await api.memo<boolean>(memoKey, context);
        if (decided !== undefined) {
          return decided ? undefined : { block: `${config.feishu.ownerName} 拒绝了这次操作（${reason}）` };
        }

        const surfaceId = surfaceForConversation(api.conversationId) ?? "";
        console.log(`[guard] asking approval for ${call.name}: ${reason}`);
        const summary = summarize(call.name, args);
        const approved = await requestApproval({
          callId: call.id,
          conversationId: api.conversationId as unknown as number,
          grantKey: key,
          tool: call.name,
          summary,
          reason,
          surfaceId,
          timeoutMs: APPROVAL_TIMEOUT_MS,
        });
        await api.memo(memoKey, approved, context);
        console.log(`[guard] ${call.name} ${approved ? "approved" : "denied"}`);
        // The card is gone in a day; the task dialog should still say it waited for a yes.
        void noteTaskEvent(
          getHarness(),
          getSessionContext(),
          api.conversationId as unknown as number,
          api.taskId as unknown as number,
          APPROVAL_EVENT,
          `${call.name}${summary === "" ? "" : `：${summary}`} → ${approved ? "已批准" : "被拒绝"}`,
        );
        return approved ? undefined : { block: `未获批准（${reason}）` };
      },
    }),
  ],
});

function summarize(tool: string, args: Record<string, unknown>): string {
  if (tool === "bash" && typeof args["command"] === "string") return args["command"].slice(0, 600);
  const path = typeof args["path"] === "string" ? args["path"] : "?";
  if (tool === "write") {
    const content = typeof args["content"] === "string" ? args["content"] : "";
    return `${path}\n\n${content.slice(0, 400)}`;
  }
  if (tool === "edit") {
    return `${path}\n\n${String(args["newText"] ?? args["new_text"] ?? "").slice(0, 400)}`;
  }
  return `${tool} ${JSON.stringify(args).slice(0, 400)}`;
}
