import { defineExtension, GenerationTask, hook, type Extension, type Registry, type ToolRegistration } from "@earendil-works/pi-durable";
import type { Context } from "@earendil-works/chord/context";

/**
 * Causality for the span store: one trace that answers "this chat message, this turn, these tool
 * calls, that subagent".
 *
 * The Harness records spans for durable phases, model requests and submissions, and nests the ones
 * it creates itself. It does not record tool calls, and a trace could not be found by the
 * conversation it belonged to — so "what did that turn actually do" was answered by reading
 * `agent.sqlite` timestamps after the fact. Two additions close that:
 *
 * 1. Every tool's `execute` runs inside a `pi.tool.call` span, a child of whatever span the task is
 *    in. Wrapping at install time is what makes it universal: MCP tools, subagent tools and the
 *    ones added later all pass through `registry.install`.
 * 2. Spans carry `session.id` (the conversation) and `task.id` (the turn), stamped once per request
 *    by the generation hook. `session.id` is the key `durable trace --session` queries, so a
 *    conversation's traces are found without knowing the trace id.
 *
 * A child conversation records its parent here, so a subagent's spans answer to both: its own
 * `session.id`, and the `parent.session.id` of the conversation that spawned it. The link is
 * in-memory (a restart forgets it), which is enough to explain a run that is still recent.
 */

type Attributes = Record<string, string | number | boolean | undefined>;

const PREFIX = "pi.tool";

/** Conversations spawned by a subagent: child → the conversation that asked for it. */
const parents = new Map<number, number>();

function sessionIdOf(conversationId: unknown): number | undefined {
  return typeof conversationId === "number" ? conversationId : undefined;
}

/** Record that `child` was spawned by `parent`, for the link a subagent's traces carry. */
export function linkChildSession(child: number, parent: number): void {
  if (child !== parent) parents.set(child, parent);
}

export function parentSessionOf(session: number): number | undefined {
  return parents.get(session);
}

/**
 * Add attributes to the span the task is currently in; never throws. Telemetry must not be able to
 * fail a turn, and a tool whose span cannot start still runs.
 */
function annotate(context: Context | undefined, attributes: Attributes): void {
  try {
    context?.telemetry?.setAttributes(attributes);
  } catch {
    // A span store that cannot record is already reported by `telemetry.ts`.
  }
}

/** The attributes every span of this turn carries: who asked, and which turn. */
function turnAttributes(conversationId: unknown, taskId: unknown): Attributes {
  const session = sessionIdOf(conversationId);
  return {
    "session.id": session,
    "tracing.task.id": typeof taskId === "number" ? taskId : undefined,
    "parent.session.id": session === undefined ? undefined : parents.get(session),
  };
}

const instrumented = new WeakSet<ToolRegistration>();

/**
 * Wrap one tool so its execution is a span. Arguments are not recorded — they carry file contents,
 * commands and user text — only the tool name, the call id and whether the call failed.
 */
function traced(tool: ToolRegistration): ToolRegistration {
  if (instrumented.has(tool)) return tool;
  instrumented.add(tool);
  const execute = tool.execute;
  return {
    ...tool,
    execute: async (args, api, context) => {
      const span = context?.telemetry;
      if (span === undefined) return execute(args, api, context);
      const attributes: Attributes = {
        "tool.name": tool.name,
        ...turnAttributes(api.conversationId, api.taskId),
        "tracing.call.id": api.callId,
      };
      if (api.wake !== undefined) attributes["tracing.wake"] = String(api.wake);
      // Recording failures inside the store do not throw (they stop recording and are reported on
      // flush), so the call is not wrapped again here: re-running a tool to "retry the span" would
      // run it twice.
      return span.startSpan({ name: `${PREFIX}.call`, attributes }, async (child) => {
        try {
          const result = await execute(args, api, context);
          child.setAttributes({ "tool.result": result.isError === true ? "error" : "ok" });
          return result;
        } catch (error) {
          const failure = error instanceof Error ? error : new Error(String(error));
          child.setStatus({ status: "error", error: { name: failure.name, message: failure.message } });
          throw error;
        }
      });
    },
  };
}

/**
 * Make every extension installed afterwards traced. Called once, before the first install, so the
 * rule is the registry's rather than each extension's.
 */
export function instrumentRegistry(registry: Registry): void {
  const install = registry.install.bind(registry);
  registry.install = (extension: Extension): void => {
    install(
      extension.tools === undefined
        ? extension
        : { ...extension, tools: extension.tools.map((tool) => traced(tool as ToolRegistration)) },
    );
  };
}

/**
 * Stamps `session.id` / `task.id` on each model request: without it a turn that called no tool
 * leaves no span naming its conversation, and `--session` cannot find it.
 */
export const traceExtension = defineExtension({
  name: "tracing",
  hooks: [
    hook(GenerationTask, {
      beforeRequest: (_request, api, context) => {
        annotate(context, turnAttributes(api.conversationId, api.taskId));
        return undefined;
      },
    }),
  ],
});
