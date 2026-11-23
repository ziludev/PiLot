import {
  configure,
  defineExtension,
  defineTask,
  defineTool,
  type ConversationId,
  type Tx,
} from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";
import { obj, opt, str } from "@earendil-works/pi-schema";
import { settle } from "./settle.ts";
import { answerTextInTx } from "./answer.ts";
import type { TaskRuntime, ToolExecutionApi } from "@earendil-works/pi-durable";
import { ROLES, toolsFor, type RoleName } from "./roles.ts";
import { notify, surfaceForConversation } from "./channel.ts";
import { reportSurface, ChatPolicy, isIsolatedPolicy } from "./access.ts";import { getHarness } from "./runtime-ref.ts";
import { log, logError } from "./log.ts";
import { AGENT_TASKS, AGENT_TOOLS, setAgentBoundary } from "./agents.ts";
import { config } from "./config.ts";
import {
  collectSnapshot,
  gateStage,
  renderGate,
  type GateResult,
  type GateStage,
} from "./gate.ts";

const ROLE_NAMES = Object.keys(ROLES) as RoleName[];
/** How many times a stage is asked for the rest of its work before the pipeline gives up on it. */
const STAGE_ROUNDS = 3;
const CHILD_MAP = "subagents:children";

// 失败也带着子会话号：一轮没答出来的孩子，它的会话恰恰是最该被翻的。
type SubagentResult =
  | { ok: true; text: string; conversationId: number; gate?: GateResult }
  | { ok: false; error: string; conversationId?: number };

function isRole(value: unknown): value is RoleName {
  return typeof value === "string" && (ROLE_NAMES as string[]).includes(value);
}

/**
 * Every child is a task-owned conversation with the role's instructions and tools.
 * A child never gets the delegate tools again: one level of fan-out, bounded cost.
 */
async function configureChild(
  tx: Tx,
  id: ConversationId,
  role: RoleName,
  isolated: boolean,
): Promise<void> {
  await configure(tx, id, {
    extensions: { remove: [subagentExtension] },
    instructions: `${ROLES[role].instructions}\n\n角色上限：${ROLES[role].maxTurns} 轮工具调用。`,
    ...toolsFor(role),
    maxTurns: ROLES[role].maxTurns,
  });
  // A child of an isolated conversation stays isolated. The memory extension reads this flag off
  // the child's own id, so without it a child hired inside a shared group would see the owner's
  // global memory — the isolation that hid it from the group would stop at the subagent boundary.
  if (isolated) {
    const doc = await tx.doc(ChatPolicy, id as never);
    doc.isolated = true;
  }
}

/**
 * Where a stage actually writes. A task-owned child is configured without its own cwd, so the
 * environment falls back to the workspace; ask the harness only to catch a child that was given one.
 */
async function stageCwdOf(id: ConversationId, context: Context): Promise<string> {
  const harness = getHarness();
  if (harness !== undefined) {
    const full = await harness.conversation(id, context).catch(() => undefined);
    const agent = full === undefined ? undefined : await full.agent(context).catch(() => undefined);
    if (agent?.cwd !== undefined && agent.cwd.length > 0) return agent.cwd;
  }
  return config.workspace;
}

async function ensureChild(
  api: ToolExecutionApi,
  context: Context,
  key: string,
  role: RoleName,
): Promise<ConversationId> {
  const map = (await api.memo<Record<string, number>>(CHILD_MAP, context)) ?? {};
  const known = map[key];
  if (known !== undefined) return known as unknown as ConversationId;

  const parentPolicy = await api
    .snapshot(ChatPolicy, api.conversationId, context)
    .catch(() => undefined);
  const isolated = isIsolatedPolicy(parentPolicy);

  const created = await api.commit(async (tx) => {
    const conversation = await tx.createConversation({ ownership: { kind: "task", taskId: api.taskId } });
    await configureChild(tx, conversation.id, role, isolated);
    return conversation.id as unknown as number;
  }, context);

  // First write wins: a retry after a crash keeps the child that already exists.
  await api.memo(CHILD_MAP, { ...map, [key]: created }, context);
  log("subagent", `spawned ${role}`, { key, conversationId: created });
  return created as unknown as ConversationId;
}

async function runChild(
  api: ToolExecutionApi,
  context: Context,
  key: string,
  role: RoleName,
  task: string,
  /** Set only by the pipeline: a plain delegate has no artefact to check. */
  gate?: GateStage,
): Promise<SubagentResult> {
  // 记住孩子是谁：后面每条失败路径都要把它报回去。
  let spawned: ConversationId | undefined;
  try {
    const conversationId = await ensureChild(api, context, key, role);
    spawned = conversationId;
    const child = await api.conversation(conversationId, context);
    if (child === undefined) return { ok: false, error: `子会话 ${conversationId} 丢失`, conversationId: conversationId as unknown as number };

    // Snapshot before the stage writes anything: "did it change the tree" is unanswerable later.
    const gateCwd = gate === undefined ? undefined : await stageCwdOf(conversationId, context);
    const before = gateCwd === undefined ? undefined : await collectSnapshot(gateCwd);
    const startedAt = Date.now();

    api.output(`→ ${role}（${ROLES[role].title}）开始…\n`);
    const submission = await child.submit(
      { type: "input", content: task, requestId: `subagent:${api.taskId}:${key}` },
      context,
    );
    const settled = await submission.wait(context);
    if (settled.status !== "done" || settled.answer === undefined) {
      return { ok: false, error: `${role} 没答出来：${settled.reason ?? "unanswered"}`, conversationId: spawned as unknown as number };
    }
    // The stage's own text, not just the last entry: a subagent that finishes with a tool call
    // would otherwise hand back only the line it wrote after that call.
    let text = await api.commit((tx) => answerTextInTx(tx, conversationId, settled.answer as never, settled.entry), context);
    let gateResult: GateResult | undefined;
    if (gate !== undefined && gateCwd !== undefined && before !== undefined) {
      const gated = await gateStage({
        stage: gate,
        cwd: gateCwd,
        before,
        text,
        ask: async (message) => {
          const again = await child.submit(
            { type: "input", content: message, requestId: `subagent:${api.taskId}:${key}:gate` },
            context,
          );
          const settledAgain = await again.wait(context);
          if (settledAgain.status !== "done" || settledAgain.answer === undefined) return undefined;
          return api.commit(
            (tx) => answerTextInTx(tx, conversationId, settledAgain.answer as never, settledAgain.entry),
            context,
          );
        },
      });
      text = gated.text;
      gateResult = gated.result;
      gateResult.ms = Date.now() - startedAt;
      api.output(`${gateResult.ok ? "✔" : "❌"} ${gate.replace("feature-", "")} 脚本核验${gateResult.ok ? "通过" : "未通过"}\n`);
      if (!gateResult.ok) {
        log("subagent", `${role} failed the gate`, { key, lines: gateResult.lines });
        return {
          ok: false,
          error: `${role} 没过脚本核验：\n${gateResult.lines.join("\n")}`,
          conversationId: conversationId as unknown as number,
        };
      }
    }
    log("subagent", `${role} answered`, { key, chars: text.length });
    api.output(`✔ ${role} 完成（${text.length} 字）\n`);
    return { ok: true, text, conversationId: conversationId as unknown as number, gate: gateResult };
  } catch (error) {
    logError("subagent", `${role} failed`, error);
    return { ok: false, error: `${role}: ${(error as Error).message}`, conversationId: spawned as unknown as number };
  }
}

const delegate = defineTool({
  name: "delegate",
  description:
    "派活给一个专职子 agent：planner（拆需求）/ designer（写设计文档）/ implementer（实现）/ tester（写测试）/ reviewer（只读评审）。子 agent 有自己的会话和上下文，返回它的结论。适合把一件明确的事整体外包。",
  parameters: obj({
    role: str({ description: ROLE_NAMES.join(" | ") }),
    task: str({ description: "给这个角色的完整指令：目标、已知信息、期望产出" }),
  }),
  // Rerunning after a crash must find the same child conversation, not spawn another.
  replay: "safe",
  execute: async (args, api, context) => {
    if (!isRole(args.role)) {
      return { output: [{ type: "text" as const, text: `未知角色 ${args.role}，可选：${ROLE_NAMES.join(", ")}` }] };
    }
    api.output(`派给 ${args.role}（${ROLES[args.role].title}）…`);
    const result = await runChild(api, context, "delegate", args.role, args.task);
    if (!result.ok) {
      // 失败了也把孩子的会话号交出去：没答出来的那一轮，正是要进去看的那一轮。
      const details: Record<string, string | number | boolean> = { role: args.role, failed: true };
      if (result.conversationId !== undefined) details.conversationId = result.conversationId;
      return { output: [{ type: "text" as const, text: `失败：${result.error}` }], isError: true, details };
    }
    await api.details({ conversationId: result.conversationId, role: args.role }, context);
    return {
      output: [{ type: "text" as const, text: `【${args.role} 的产出】\n${result.text}` }],
      details: { conversationId: result.conversationId, role: args.role },
    };
  },
});

const delegateParallel = defineTool({
  name: "delegate_parallel",
  description:
    "并行派给多个角色，全部完成后汇总。适合互不依赖的事（例如同时做 reviewer 评审 + tester 写测试）。",
  parameters: obj({
    tasks: str({
      description: 'JSON 数组：[{"role":"designer","task":"…"},{"role":"reviewer","task":"…"}]。按字符串传入。',
    }),
  }),
  replay: "safe",
  execute: async (args, api, context) => {
    let tasks: Array<{ role?: string; task?: string }>;
    try {
      tasks = JSON.parse(args.tasks) as Array<{ role?: string; task?: string }>;
    } catch {
      return { output: [{ type: "text" as const, text: "tasks 必须是 JSON 数组字符串" }], isError: true };
    }
    const jobs = tasks
      .filter((item) => isRole(item.role) && typeof item.task === "string")
      .map((item, index) => ({ role: item.role as RoleName, task: item.task as string, key: `parallel-${index}` }));
    if (jobs.length === 0) {
      return { output: [{ type: "text" as const, text: "没有有效的任务" }], isError: true };
    }
    api.output(`并行派发给 ${jobs.map((job) => job.role).join(" / ")}…`);
    const results = await Promise.all(jobs.map((job) => runChild(api, context, job.key, job.role, job.task)));
    const text = results
      .map((result, index) => {
        const job = jobs[index]!;
        return result.ok ? `【${job.role}】\n${result.text}` : `【${job.role}】失败：${result.error}`;
      })
      .join("\n\n---\n\n");
    const failed = results.filter((result) => !result.ok).length;
    return {
      output: [{ type: "text" as const, text }],
      isError: failed === results.length ? true : undefined,
      details: {
        roles: jobs.map((job) => job.role),
        failed,
        // 并行批次以前只报角色和失败数，父会话因此进不去任何一个孩子。
        children: results.map((result, index) => ({
          role: jobs[index]!.role,
          conversationId: result.conversationId,
          failed: !result.ok,
        })),
      } as never,
    };
  },
});

// ---- background pipeline ---------------------------------------------------

type FeatureInput = {
  spec: string;
  /** Where to report when it finishes. */
  surfaceId: string;
  /** Parent conversation, so the result also lands in its transcript. */
  parentConversationId: number;
  skipTests: boolean;
  skipReview: boolean;
};

type StageKey = GateStage;

/**
 * The same pipeline as the synchronous tool, but as a background durable task:
 * the conversation goes idle immediately, the stages keep running, and the owner
 * gets one message when it is done. A crash resumes at the first unfinished stage
 * because each stage's output is memoized.
 */
const FeatureTask = defineTask<FeatureInput, { phase: "run" }, string>({
  name: "app.feature",
  version: 1,
  initial: () => ({ phase: "run" }),
  phases: {
    run: async (task, runtime, context) => {
      const stages: Array<{ key: StageKey; role: RoleName; title: string }> = [
        { key: "feature-design", role: "designer", title: "写设计文档" },
        { key: "feature-implement", role: "implementer", title: "实现" },
      ];
      if (!task.input.skipTests) {
        stages.push({ key: "feature-test", role: "tester", title: "写测试" });
      }
      // Review last, after the tests exist: a reviewer that cannot see the test a change had to pass
      // reviews half the change. It reads the diff, not the summary.
      if (!task.input.skipReview) {
        stages.push({ key: "feature-review", role: "reviewer", title: "评审改动" });
      }

      const outputs: Record<string, string> = {};
      const gates: Record<string, GateResult> = {};
      for (const stage of stages) {
        const done = await runtime.memo<string>(`stage:${stage.key}`, context);
        if (done !== undefined) {
          outputs[stage.key] = done;
          const remembered = await runtime.memo<GateResult>(`gate:${stage.key}`, context);
          if (remembered !== undefined) gates[stage.key] = remembered;
          continue;
        }
        const result = await runStage(runtime, task, context, stage.key, stage.role, stage.title, outputs, task.input.spec);
        if (!result.ok) {
          await report(runtime, task, context, `❌ ${stage.role} 失败了：${result.error}`);
          await settle("app.feature", () =>
            runtime.commit(
              () => ({ status: "terminal", outcome: { status: "failed", error: { message: result.error } } }),
              context,
            ),
          );
          return;
        }
        outputs[stage.key] = result.text;
        if (result.gate !== undefined) {
          gates[stage.key] = result.gate;
          await runtime.memo(`gate:${stage.key}`, result.gate, context);
        }
      }

      const summary = buildSummary(task.input.spec, outputs, stages, gates);
      await report(runtime, task, context, summary);
      await settle("app.feature", () =>
        runtime.commit(
          () => ({ status: "terminal", outcome: { status: "completed", result: summary } }),
          context,
        ),
      );
    },
  },
  abort: (_task, runtime, context) =>
    settle("app.feature", () =>
      runtime.commit(() => ({ status: "terminal", outcome: { status: "aborted" } }), context),
    ),
});

async function runStage(
  runtime: TaskRuntime<FeatureInput, { phase: "run" }, string, object>,
  task: { input: FeatureInput; id: unknown },
  context: Context,
  key: StageKey,
  role: RoleName,
  title: string,
  previous: Record<string, string>,
  spec: string,
): Promise<{ ok: true; text: string; gate?: GateResult } | { ok: false; error: string }> {
  try {
    const existing = await runtime.memo<number>(`child:${key}`, context);
    let childId = existing;
    if (childId === undefined) {
      // Read the hiring conversation's policy here: a stage belongs to the feature task, not to that
      // conversation, so nothing about it is inherited unless we copy it.
      const parentPolicy = await runtime
        .snapshot(ChatPolicy, task.input.parentConversationId as never, context)
        .catch(() => undefined);
      let created = 0;
      await runtime.commit(async (tx) => {
        const conversation = await tx.createConversation({
          ownership: { kind: "task", taskId: task.id as never },
        });
        await configureChild(tx, conversation.id, role, isIsolatedPolicy(parentPolicy));
        created = conversation.id as unknown as number;
        return undefined;
      }, context);
      childId = created;
      await runtime.memo(`child:${key}`, childId, context);
      log("feature", `spawned ${role}`, { key, conversationId: childId });
    }

    const child = await runtime.conversation(childId as never, context);
    if (child === undefined) return { ok: false, error: `子会话 ${childId} 丢失` };

    // The stage's own working directory, and the tree as it stood before the stage ran: the gate
    // judges the changes this stage made, not everything that happens to be dirty in the repo.
    const stageCwd = await stageCwdOf(childId as never, context);
    const before = await collectSnapshot(stageCwd);
    const startedAt = Date.now();

    // A reviewer gets the spec and the machine's list of changed files, never the implementer's
    // account of what it did: a verifier that inherits the producer's view misses what it missed.
    const scope =
      key === "feature-review"
        ? ((await runtime.memo<GateResult>("gate:feature-implement", context))?.touched ?? [])
        : [];
    const prompt = buildStagePrompt(key, spec, previous, scope);
    const submission = await child.submit(
      { type: "input", content: prompt, requestId: `feature:${String(task.id)}:${key}` },
      context,
    );
    let settled = await submission.wait(context);
    // A stage that ran out of steps is not a failed stage: the work is in its transcript, so ask
    // for the rest the same way the chat does, up to STAGE_ROUNDS times.
    for (let round = 1; round <= STAGE_ROUNDS && settled.status === "unanswered" && settled.reason === "turn_limit"; round += 1) {
      log("feature", `${role} hit the step limit, continuing`, { key, round });
      const more = await child.submit({ type: "continue", requestId: `feature:${String(task.id)}:${key}:c${round}` }, context);
      settled = await more.wait(context);
    }
    if (settled.status !== "done" || settled.answer === undefined) {
      // Running out of steps is not the same failure as answering badly: the edits are usually on
      // disk by then, so say where to look instead of reporting an empty stage.
      if (settled.reason === "turn_limit") {
        return {
          ok: false,
          error: `${role} 用完 ${(STAGE_ROUNDS + 1) * ROLES[role].maxTurns} 步还没收尾，改动多半已经落盘，去会话 ${childId} 看`,
        };
      }
      return {
        ok: false,
        error: `${role} 没答出来：${settled.reason ?? "unanswered"}${settled.detail === undefined ? "" : `（${String(settled.detail)}）`}`,
      };
    }
    // Every text block of the stage, read in a transaction: the plain entry record does not carry
    // the model messages, and the last entry alone drops what was said before the final tool call.
    let text = "";
    await runtime.commit(async (tx) => {
      text = await answerTextInTx(tx, childId as never, settled.answer as never, settled.entry);
      return undefined;
    }, context);
    if (text.length === 0) return { ok: false, error: `${role} 产出为空` };
    // The report is not the verdict: a script looks at what actually landed, and a stage that
    // missed an artefact gets one message to add it before the pipeline calls the work failed.
    let gate: GateResult | undefined;
    const gated = await gateStage({
      stage: key,
      cwd: stageCwd,
      before,
      text,
      ask: async (message) => {
        const again = await child.submit(
          { type: "input", content: message, requestId: `feature:${String(task.id)}:${key}:gate` },
          context,
        );
        const settledAgain = await again.wait(context);
        if (settledAgain.status !== "done" || settledAgain.answer === undefined) return undefined;
        let more = "";
        await runtime.commit(async (tx) => {
          more = await answerTextInTx(tx, childId as never, settledAgain.answer as never, settledAgain.entry);
          return undefined;
        }, context);
        return more;
      },
    });
    gate = gated.result;
    gate.ms = Date.now() - startedAt;
    text = gated.text;
    if (!gate.ok) {
      log("feature", `${role} failed the gate`, { key, lines: gate.lines });
      return { ok: false, error: `${role} 没过脚本核验：\n${gate.lines.join("\n")}` };
    }
    await runtime.memo(`stage:${key}`, text, context);
    log("feature", `${role} done`, { key, chars: text.length, gate: gate.ok });
    return { ok: true, text, gate };
  } catch (error) {
    logError("feature", `${title} failed`, error);
    return { ok: false, error: `${role}: ${(error as Error).message}` };
  }
}

export function buildStagePrompt(
  key: StageKey,
  spec: string,
  previous: Record<string, string>,
  /** Files the implement stage changed, from the gate — the review's only trusted scope. */
  changed: string[] = [],
): string {
  const design = previous["feature-design"] ?? "";
  const implementation = previous["feature-implement"] ?? "";
  if (key === "feature-design") {
    return `需求：\n${spec}\n\n请写设计文档并保存到 workspace/docs/ 下，给出路径、摘要和给实现者的注意事项。`;
  }
  if (key === "feature-implement") {
    return `需求：\n${spec}\n\n设计文档与注意事项：\n${design}\n\n请实现并自测，然后按约定格式汇报。`;
  }
  if (key === "feature-review") {
    const scope = changed.length === 0 ? "（脚本没取到清单：自己用 git status / git diff 找）" : changed.map((path) => `- ${path}`).join("\n");
    return `需求：\n${spec}\n\n设计摘要：\n${design}\n\n本次改动文件（脚本从 git 取的范围）：\n${scope}\n\n实现者和测试者的自述故意不给你——那会带偏你看到的东西。自己读 git diff 和这些文件，然后：\n- 每条问题必须带 文件:行 和理由；查不到的单独列 UNKNOWN。\n- 只列真正必须改的，最多三条；没有就一句话说通过。`;
  }
  return `需求：\n${spec}\n\n设计摘要：\n${design}\n\n实现者的汇报：\n${implementation}\n\n请按约定写测试并运行一次。`;
}

/** Cut a report at a line, not mid-word: a headline that ends in "…（还有 1 千字）" reads as shorthand. */
export function cutAtLine(text: string, max: number): string {
  if (text.length <= max) return text;
  const cut = text.slice(0, max);
  const boundary = cut.lastIndexOf("\n");
  const kept = boundary > max * 0.6 ? cut.slice(0, boundary) : cut;
  const left = text.length - kept.length;
  return `${kept}\n…（还有约 ${Math.round(left / 100) * 100} 字，完整版在客户端这一轮里）`;
}

function buildSummary(
  spec: string,
  outputs: Record<string, string>,
  stages: ReadonlyArray<{ key: string }>,
  gates: Record<string, GateResult>,
): string {
  const body = stages
    .map((stage) => {
      const text = outputs[stage.key] ?? "";
      return `**${stage.key.replace("feature-", "")}**\n${text.slice(0, 900)}`;
    })
    .join("\n\n");
  // Say which parts a script checked and which are only the stage's own claim: a report that reads
  // the same whether or not the work was verified is how "做完了" stopped meaning anything.
  const checked = stages.filter((stage) => gates[stage.key] !== undefined);
  const soft = checked.flatMap((stage) => gates[stage.key]!.checks.filter((check) => !check.ok));
  const block =
    checked.length === 0
      ? ""
      : `**机器核验（脚本判定）**\n${checked
          .map((stage) => {
            const gate = gates[stage.key]!;
            const name = stage.key.replace("feature-", "");
            const flags = gate.checks.map((check) => `${check.ok ? "✅" : "⚠️"}${check.name}`).join(" " );
            const seconds = gate.ms === undefined ? "" : `（${Math.round(gate.ms / 1000)}s）`;
            return `- ${name} ${gate.ok ? "✅" : "❌"} ${flags}${seconds}`;
          })
          .join("\n")}\n其余结论来自各角色自述，未经脚本核验${soft.length === 0 ? "" : `；另有 ${soft.length} 项只有自述`}。\n\n`;
  return `需求：${spec.slice(0, 200)}\n\n${block}${body}`;
}

async function report(
  runtime: TaskRuntime<FeatureInput, { phase: "run" }, string, object>,
  task: { input: FeatureInput },
  context: Context,
  text: string,
): Promise<void> {
  // Land it in the parent transcript so the conversation remembers what happened,
  // then push one message: the owner should not have to poll.
  await runtime.commit(async (tx) => {
    await tx.appendEntry(task.input.parentConversationId as never, {
      kind: "app.feature-report",
      data: { text: cutAtLine(text, 4000) } as never,
    });
    return undefined;
  }, context);
  const origin = surfaceForConversation(task.input.parentConversationId) ?? task.input.surfaceId;
  // A group asked for the work; the report goes to the owner's DM all the same.
  const surfaceId = await reportSurface(runtime, context, origin);
  const headline = text.startsWith("❌") ? "🧩 需求没做完" : "🧩 需求做完了";
  if (surfaceId.length > 0) {
    await notify(surfaceId, `${headline}\n\n${cutAtLine(text, 3000)}`).catch((error) =>
      logError("feature", "could not notify", error),
    );
  }
}

const buildFeature = defineTool({
  name: "build_feature",
  description:
    "把需求跑完整流水线：designer 写设计文档 → implementer 实现自测 → tester 写测试 → reviewer 评审改动。每个阶段结束都有脚本核验（设计文档真的落盘、代码真的有改动、测试文件真的有改动、评审提问题必须带 文件:行），核验不过先给该阶段一次补充机会，仍不过就把任务判失败并在报告里写清哪项没过。background=\"1\" 时丢到后台跑，立即返回，完成后主动发消息（推荐，尤其是要几分钟的活）；否则同步等待结果。",
  parameters: obj({
    spec: str({ description: "需求描述：要做什么、边界、验收标准" }),
    background: opt(str({ description: '传 "1" 后台跑（推荐）' })),
    skipTests: opt(str({ description: '传 "1" 跳过测试阶段' })),
    skipReview: opt(str({ description: '传 "1" 跳过评审阶段' })),
  }),
  replay: "safe",
  execute: async (args, api, context) => {
    if (args.background === "1") {
      const existing = await api.memo<number>("feature-task", context);
      if (existing !== undefined) {
        return {
          output: [{ type: "text" as const, text: `这个后台任务已经派过了（task ${existing}），完成会通知你。` }],
        };
      }
      const surfaceId = surfaceForConversation(api.conversationId) ?? "";
      const taskId = await api.createTask(
        FeatureTask,
        {
          spec: args.spec,
          surfaceId,
          parentConversationId: api.conversationId as unknown as number,
          skipTests: args.skipTests === "1",
          skipReview: args.skipReview === "1",
        },
        // Background: outlives this turn, so the conversation goes idle while it runs.
        { ownership: { kind: "conversation" }, background: true },
        context,
      );
      await api.memo("feature-task", taskId as unknown as number, context);
      log("feature", "background task created", { taskId, surfaceId });
      return {
        output: [
          {
            type: "text" as const,
            text: `已在后台开工（task ${taskId}）：designer → implementer${
              args.skipTests === "1" ? "" : " → tester"
            }${args.skipReview === "1" ? "" : " → reviewer"}。做完我主动发你消息，期间你可以继续聊别的。`,
          },
        ],
      };
    }

    const report: string[] = [];
    api.output("① designer 写设计文档…");
    const design = await runChild(
      api,
      context,
      "feature-design",
      "designer",
      buildStagePrompt("feature-design", args.spec, {}),
      "feature-design",
    );
    if (!design.ok) {
      return { output: [{ type: "text" as const, text: `设计阶段失败：${design.error}` }], isError: true };
    }
    report.push(`【designer】\n${design.text}${design.gate === undefined ? "" : `\n\n${renderGate(design.gate)}`}`);

    api.output("② implementer 实现…");
    const implementation = await runChild(
      api,
      context,
      "feature-implement",
      "implementer",
      buildStagePrompt("feature-implement", args.spec, { "feature-design": design.text }),
      "feature-implement",
    );
    if (!implementation.ok) {
      return {
        output: [{ type: "text" as const, text: `${report.join("\n\n")}\n\n实现阶段失败：${implementation.error}` }],
        isError: true,
      };
    }
    report.push(`【implementer】\n${implementation.text}${implementation.gate === undefined ? "" : `\n\n${renderGate(implementation.gate)}`}`);

    let testText = "";
    if (args.skipTests !== "1") {
      api.output("③ tester 写测试并运行…");
      const tests = await runChild(
        api,
        context,
        "feature-test",
        "tester",
        buildStagePrompt("feature-test", args.spec, {
          "feature-design": design.text,
          "feature-implement": implementation.text,
        }),
        "feature-test",
      );
      testText = tests.ok ? tests.text : `失败：${tests.error}`;
      report.push(`【tester】\n${testText}${tests.ok && tests.gate !== undefined ? `\n\n${renderGate(tests.gate)}` : ""}`);
    }

    if (args.skipReview !== "1") {
      api.output("④ reviewer 读改动评审…");
      const review = await runChild(
        api,
        context,
        "feature-review",
        "reviewer",
        buildStagePrompt("feature-review", args.spec, { "feature-design": design.text }, implementation.gate?.touched ?? []),
        "feature-review",
      );
      report.push(
        review.ok
          ? `【reviewer】\n${review.text}${review.gate === undefined ? "" : `\n\n${renderGate(review.gate)}`}`
          : `【reviewer】失败：${review.error}`,
      );
    }

    return {
      output: [{ type: "text" as const, text: report.join("\n\n---\n\n") }],
      details: { stages: report.length } as never,
    };
  },
});


const taskStatus = defineTool({
  name: "task_status",
  description:
    "看后台任务状态：不传 taskId 列出当前所有在跑的后台任务（含 app.feature 流水线）；传 taskId 看单个详情。",
  parameters: obj({ taskId: opt(str({ description: "任务 id，可选" })) }),
  replay: "safe",
  execute: async (args, _api, context) => {
    const harness = getHarness();
    if (harness === undefined) {
      return { output: [{ type: "text" as const, text: "拿不到 harness 句柄" }], isError: true };
    }
    if (typeof args.taskId === "string" && args.taskId.length > 0) {
      const record = await harness.getTask(args.taskId as never, context);
      if (record === undefined) {
        return { output: [{ type: "text" as const, text: `没有 task ${args.taskId}（可能已经结束并被清理）` }] };
      }
      const state = record.state as unknown as { status?: string; kind?: string; outcome?: unknown };
      return {
        output: [
          {
            type: "text" as const,
            text: `task ${record.id}
kind: ${record.kind}
status: ${state.status ?? state.kind}
outcome: ${JSON.stringify(
              state.outcome ?? null,
            ).slice(0, 500)}`,
          },
        ],
      };
    }

    const inspection = await harness.inspect(context);
    const live = inspection.tasks.filter((task) => task.record.kind !== "pi.generation");
    if (live.length === 0) {
      return { output: [{ type: "text" as const, text: "现在没有在跑的后台任务" }] };
    }
    const lines = live.map((task) => {
      const state = task.state as unknown as { kind?: string } & Record<string, unknown>;
      const detail = state.kind === "running" ? "running" : state.kind ?? "?";
      return `- ${task.record.id} ${task.record.kind} [${detail}] conversation=${task.record.conversationId}`;
    });
    return { output: [{ type: "text" as const, text: `在跑的任务：\n${lines.join("\n")}` }] };
  },
});

const cancelTask = defineTool({
  name: "cancel_task",
  description: "取消一个后台任务（例如跑偏的 app.feature 流水线）。会级联停掉它拥有的子 agent。",
  parameters: obj({ taskId: str({ description: "要取消的 task id" }) }),
  execute: async (args, _api, context) => {
    const harness = getHarness();
    if (harness === undefined) {
      return { output: [{ type: "text" as const, text: "拿不到 harness 句柄" }], isError: true };
    }
    const result = await harness.abortTask(args.taskId as never, context);
    log("subagent", "cancel requested", { taskId: args.taskId, result });
    return {
      output: [
        {
          type: "text" as const,
          text: result === "terminal" ? `task ${args.taskId} 已经是结束状态` : `已取消 task ${args.taskId}（连带子 agent）`,
        },
      ],
    };
  },
});

/** Deferred per conversation: a group message never needs to spawn a subagent. */
export const SUBAGENT_TOOLS = [delegate, delegateParallel, buildFeature, taskStatus, cancelTask];

export const subagentExtension = defineExtension({
	name: "subagents",
	// Long-lived named agents live in the same extension: they are the other way to hand work
	// over, and an agent that could hire agents would be an unbounded fan-out.
	tools: [...SUBAGENT_TOOLS, ...AGENT_TOOLS],
	tasks: [FeatureTask, ...AGENT_TASKS],
});

setAgentBoundary(subagentExtension);
