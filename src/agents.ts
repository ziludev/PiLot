/**
 * Agents you keep talking to.
 *
 * `delegate` is one question and one answer. This is the other shape: a named conversation that
 * outlives the turn, keeps its own memory of what it was asked, and can be redirected mid-flight.
 * "reviewer 盯着这个 MR，我每次改完让它再看一遍" needs a colleague, not a request.
 *
 * The conversation is ownerless on purpose: no turn owns it, so no abort, no turn limit and no
 * restart takes it away. The registry that names it lives in the conversation that hired it.
 */
import {
  configure,
  defineDoc,
  defineTask,
  defineTool,
  type ConversationId,
  type Extension,
  type TaskRuntime,
  type ToolExecutionApi,
  type Tx,
} from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";
import { obj, opt, str } from "@earendil-works/pi-schema";
import { answerTextInTx } from "./answer.ts";
import { notify, surfaceForConversation } from "./channel.ts";
import { log, logError } from "./log.ts";
import { ROLES, toolsFor, type RoleName } from "./roles.ts";
import { settle } from "./settle.ts";

const ROLE_NAMES = Object.keys(ROLES) as RoleName[];
/** How many times an agent is asked for the rest of its work before we call it stuck. */
const TURN_ROUNDS = 2;

export type AgentRecord = {
  role: RoleName;
  conversationId: number;
  status: "working" | "idle" | "failed";
  /** Tail of the last answer, so `list_agents` needs no transcript read. */
  last?: string;
  updatedAt: number;
};

/** Named agents per conversation. Names are what the model says out loud, so they are stable. */
export const Agents = defineDoc<{ agents: Record<string, AgentRecord> }>({
  kind: "app.agents",
  version: 1,
  scope: "conversation",
  history: "latest",
  fork: "current",
  initial: () => ({ agents: {} }),
  checkpointWhen: () => false,
});

function isRole(value: unknown): value is RoleName {
  return typeof value === "string" && (ROLE_NAMES as string[]).includes(value);
}

/**
 * The subagent extension, so an agent cannot hire agents of its own: one level of fan-out, like
 * every other child. Registered by `subagents.ts`, which owns the extension instance.
 */
let boundary: Extension | undefined;
export function setAgentBoundary(extension: Extension): void {
  boundary = extension;
}

async function configureAgent(tx: Tx, id: ConversationId, role: RoleName): Promise<void> {
  await configure(tx, id, {
    extensions: { remove: boundary === undefined ? [] : [boundary] },
    instructions: `你是 ${role}，alex 的长期协作者。上一条消息可能是以前的活，别当新任务重做。\n\n${ROLES[role].instructions}\n\n角色上限：${ROLES[role].maxTurns} 轮工具调用。`,
    ...toolsFor(role),
    maxTurns: ROLES[role].maxTurns,
  });
}

// ---- the turn ---------------------------------------------------------------

type AgentInput = {
  name: string;
  role: RoleName;
  conversationId: number;
  parentConversationId: number;
  message: string;
  /** Redirect what it is doing right now instead of queueing behind it. */
  interrupt: boolean;
};

/**
 * Delivers one message and brings the answer back. A background task, so the conversation that
 * spoke is idle the moment the tool returns, and a restart resumes the wait instead of losing it.
 */
const AgentTurn = defineTask<AgentInput, { phase: "turn" }, string>({
  name: "app.agent-turn",
  version: 1,
  initial: () => ({ phase: "turn" }),
  phases: {
    turn: async (task, runtime, context) => {
      const { conversationId, name, message, role } = task.input;
      const child = await runtime.conversation(conversationId as never, context);
      if (child === undefined) {
        await finish(runtime, task, context, `❌ ${name} 的会话 ${conversationId} 找不到了`);
        return;
      }

      try {
        const requestId = `agent:${String(task.id)}`;
        let submission;
        try {
          // A steer joins the run in flight; with no run going it becomes the next input, which is
          // also what "interrupt" means for an idle agent.
          submission = await child.submit(
            task.input.interrupt
              ? { type: "input", content: message, requestId, whenBusy: "steer" }
              : { type: "input", content: message, requestId },
            context,
          );
        } catch (error) {
          // Steering only exists while a run is going; an idle agent just gets the message.
          log("agent", `${name} could not be steered, sending it instead`, { error: (error as Error).message });
          submission = await child.submit({ type: "input", content: message, requestId }, context);
        }

        let settled = await submission.wait(context);
        for (
          let round = 1;
          round <= TURN_ROUNDS && settled.status === "unanswered" && settled.reason === "turn_limit";
          round += 1
        ) {
          log("agent", `${name} hit the step limit, continuing`, { round });
          const more = await child.submit(
            { type: "continue", requestId: `agent:${String(task.id)}:c${round}` },
            context,
          );
          settled = await more.wait(context);
        }

        if (settled.status !== "done" || settled.answer === undefined) {
          const why =
            settled.reason === "turn_limit"
              ? `${(TURN_ROUNDS + 1) * ROLES[role].maxTurns} 步还没收尾，去会话 ${conversationId} 看进度`
              : `${settled.reason ?? "没答出来"}${settled.detail === undefined ? "" : `（${String(settled.detail)}）`}`;
          await mark(runtime, task, context, { status: "failed" });
          await finish(runtime, task, context, `❌ ${name} 没答完：${why}`);
          return;
        }

        let text = "";
        await runtime.commit(async (tx) => {
          text = await answerTextInTx(tx, conversationId as never, settled.answer as never, settled.entry);
          return undefined;
        }, context);
        const answer = text.length === 0 ? "（它没说话，只有工具调用，去会话里看）" : text;
        await mark(runtime, task, context, { status: "idle", last: answer.slice(-400) });
        await finish(runtime, task, context, `${name}：\n\n${answer}`);
      } catch (error) {
        logError("agent", `${name} failed`, error);
        await mark(runtime, task, context, { status: "failed" });
        await finish(runtime, task, context, `❌ ${name} 出错了：${(error as Error).message}`);
      }
    },
  },
  abort: (_task, runtime, context) =>
    settle("app.agent-turn", () =>
      runtime.commit(() => ({ status: "terminal", outcome: { status: "aborted" } }), context),
    ),
});

async function mark(
  runtime: TaskRuntime<AgentInput, { phase: "turn" }, string, object>,
  task: { input: AgentInput },
  context: Context,
  patch: Partial<Pick<AgentRecord, "status" | "last">>,
): Promise<void> {
  await runtime.commit(async (tx) => {
    const doc = await tx.doc(Agents, task.input.parentConversationId as never);
    const record = doc.agents[task.input.name];
    if (record === undefined) return undefined;
    if (patch.status !== undefined) record.status = patch.status;
    if (patch.last !== undefined) record.last = patch.last;
    record.updatedAt = Date.now();
    return undefined;
  }, context);
}

/** Lands the answer in the conversation that asked, then one message: nobody should have to poll. */
async function finish(
  runtime: TaskRuntime<AgentInput, { phase: "turn" }, string, object>,
  task: { input: AgentInput },
  context: Context,
  text: string,
): Promise<void> {
  await runtime.commit(async (tx) => {
    await tx.appendEntry(task.input.parentConversationId as never, {
      kind: "app.agent-report",
      data: { name: task.input.name, text } as never,
    });
    return undefined;
  }, context);
  const surfaceId = surfaceForConversation(task.input.parentConversationId) ?? "";
  if (surfaceId.length > 0) {
    await notify(surfaceId, text).catch((error) => logError("agent", "could not notify", error));
  }
  await settle("app.agent-turn", () =>
    runtime.commit(() => ({ status: "terminal", outcome: { status: "completed", result: text } }), context),
  );
}

// ---- tools ------------------------------------------------------------------

function nameOf(args: { name?: string; role: RoleName }): string {
  const given = (args.name ?? "").trim();
  return given === "" ? args.role : given;
}

async function hire(
  api: ToolExecutionApi,
  context: Context,
  record: AgentRecord,
  name: string,
  message: string,
  interrupt: boolean,
): Promise<void> {
  await api.createTask(
    AgentTurn,
    {
      name,
      role: record.role,
      conversationId: record.conversationId,
      parentConversationId: api.conversationId as unknown as number,
      message,
      interrupt,
    },
    { ownership: { kind: "conversation" }, background: true },
    context,
  );
}

const spawnAgent = defineTool({
  name: "spawn_agent",
  description:
    "雇一个长期协作的 agent（不是一次性派活）：给它名字和角色，它记着自己的上下文，之后能用 tell_agent 接着聊、改方向、让它再看一遍。适合「盯着这个 MR」「长期帮我查某个方向」这类反复要问的事。派完立刻返回，答复会作为消息发回来。",
  parameters: obj({
    task: str({ description: "第一件事：做什么，边界和验收标准" }),
    role: str({ description: "角色：planner/designer/implementer/tester/reviewer" }),
    name: opt(str({ description: "名字，默认用角色名；同名再调一次等于把事交给同一个 agent" })),
  }),
  replay: "safe",
  execute: async (args, api, context) => {
    const role: RoleName = isRole(args.role) ? args.role : "implementer";
    const name = nameOf({ name: args.name, role });
    const known = (await api.snapshot(Agents, api.conversationId, context).catch(() => undefined))?.agents?.[name];
    if (known !== undefined) {
      // Same name: the same colleague picks the thread back up, it does not get a twin.
      await hire(api, context, known, name, args.task, false);
      return {
        output: [
          { type: "text" as const, text: `${name} 已经在岗（会话 ${known.conversationId}），这件事交给它了，答复会发回来。` },
        ],
      };
    }

    let created = 0;
    await api.commit(async (tx) => {
      // Ownerless: no turn owns a colleague, so no abort or restart takes it away.
      const conversation = await tx.createConversation({ ownership: { kind: "ownerless" } });
      await configureAgent(tx, conversation.id, role);
      const doc = await tx.doc(Agents, api.conversationId);
      doc.agents[name] = {
        role,
        conversationId: conversation.id as unknown as number,
        status: "working",
        updatedAt: Date.now(),
      };
      created = conversation.id as unknown as number;
      return undefined;
    }, context);

    await api.details({ conversationId: created as never }, context);
    await hire(
      api,
      context,
      { role, conversationId: created, status: "working", updatedAt: Date.now() },
      name,
      args.task,
      false,
    );
    log("agent", `hired ${name}`, { role, conversationId: created });
    return {
      output: [
        {
          type: "text" as const,
          text: `已雇 ${name}（${role}，会话 ${created}），它开始干活了，答复会作为消息发回来；中途要改方向用 tell_agent。`,
        },
      ],
    };
  },
});

const tellAgent = defineTool({
  name: "tell_agent",
  description:
    "给在岗的 agent 发消息：接着派活、补要求、或者打断它正在做的（interrupt=\"1\"）。立即返回，答复照旧发回来。",
  parameters: obj({
    name: str({ description: "agent 的名字" }),
    message: str({ description: "要跟它说什么" }),
    interrupt: opt(str({ description: '传 "1" 打断它当前的动作、直接按这条来' })),
  }),
  replay: "safe",
  execute: async (args, api, context) => {
    const doc = await api.snapshot(Agents, api.conversationId, context).catch(() => undefined);
    const record = doc?.agents?.[args.name];
    if (record === undefined) {
      const names = Object.keys(doc?.agents ?? {});
      return {
        output: [
          {
            type: "text" as const,
            text: `没有叫 ${args.name} 的 agent${names.length === 0 ? "，先用 spawn_agent 雇一个" : `。在岗的有：${names.join("、")}`}`,
          },
        ],
      };
    }
    await hire(api, context, record, args.name, args.message, args.interrupt === "1");
    return {
      output: [
        {
          type: "text" as const,
          text: args.interrupt === "1" ? `已经打断 ${args.name}，按新指令来。` : `已经转告 ${args.name}，答复会发回来。`,
        },
      ],
    };
  },
});

const listAgents = defineTool({
  name: "list_agents",
  description: "看在岗的 agent：名字、角色、在忙还是闲着、上一次答到哪。",
  parameters: obj({}),
  replay: "safe",
  execute: async (_args, api, context) => {
    const doc = await api.snapshot(Agents, api.conversationId, context).catch(() => undefined);
    const entries = Object.entries(doc?.agents ?? {});
    if (entries.length === 0) return { output: [{ type: "text" as const, text: "还没有雇过 agent。" }] };
    const lines = entries.map(([name, record]) => {
      const state = record.status === "working" ? "在忙" : record.status === "failed" ? "上次没答完" : "闲着";
      const last = record.last === undefined ? "" : `\n  上次：${record.last.replace(/\s+/g, " ").slice(0, 120)}`;
      return `- ${name}（${record.role}，${state}，会话 ${record.conversationId}）${last}`;
    });
    return { output: [{ type: "text" as const, text: lines.join("\n") }] };
  },
});

const stopAgent = defineTool({
  name: "stop_agent",
  description: "停掉某个 agent 正在做的事（会话保留，之后还能用 tell_agent 找它）。",
  parameters: obj({ name: str({ description: "agent 的名字" }) }),
  replay: "safe",
  execute: async (args, api, context) => {
    const doc = await api.snapshot(Agents, api.conversationId, context).catch(() => undefined);
    const record = doc?.agents?.[args.name];
    if (record === undefined) return { output: [{ type: "text" as const, text: `没有叫 ${args.name} 的 agent。` }] };
    const handle = await api.conversation(record.conversationId as never, context);
    if (handle === undefined) return { output: [{ type: "text" as const, text: `${args.name} 的会话不见了。` }] };
    await handle.abort(context);
    return { output: [{ type: "text" as const, text: `已经让 ${args.name} 停下，它还在岗。` }] };
  },
});

export const AGENT_TOOLS = [spawnAgent, tellAgent, listAgents, stopAgent];
export const AGENT_TASKS = [AgentTurn];
