import { defineExtension, defineTool, hook, ToolTask } from "@earendil-works/pi-durable";
import { obj, str } from "@earendil-works/pi-schema";
import { getHarness, getSessionContext } from "./runtime-ref.ts";
import { deriveStages } from "./stages.ts";
import { baseOf, workflowOf, DEFAULT_WORKFLOW } from "./projects.ts";
import { projectIdOf } from "./workflow-ctx.ts";
import { dirForConversation, projectPathOf } from "./workflow-ctx.ts";
import { gitBranchOf } from "./git-info.ts";
import { log } from "./log.ts";

// The team's review cap, enforced where it can still stop something: a merge
// request that has used its rounds of machine review cannot be merged from a tool
// call, and the model is told why instead of discovering it in GitLab's answer.
//
// This is an extension and not a bridge rule because it changes what the model may
// do, which is what extensions are for. The bridge still draws the strip: it owns
// the facts, this owns the refusal.

const ROUND_LIMIT = 3;

/** How many merge requests to object to in one turn, so a block does not loop. */
const blockedThisTurn = new Map<string, number>();

async function stagesFor(conversationId: number) {
  const harness = getHarness();
  const context = getSessionContext();
  if (harness === undefined || context === undefined) throw new Error("workflow: no session");
  const cwd = await dirForConversation(harness, context, conversationId);
  const project = await projectPathOf(harness, context, conversationId);
  // Same numbers the strip draws: 提测 goes to the project's test branch, the
  // release request to its target, and the cap is the project's own.
  const projectId = await projectIdOf(harness, context, conversationId);
  const flow = projectId === undefined ? DEFAULT_WORKFLOW : await workflowOf(harness, context, projectId);
  const origin = (await baseOf(harness, context, conversationId)) ?? flow.origin;
  return deriveStages(project, cwd, {
    origin,
    base: flow.testBase,
    branch: (await gitBranchOf(cwd)).trim(),
    target: flow.releaseTarget,
    roundLimit: ROUND_LIMIT,
  });
}

/**
 * The sentence a merge gets when the cap is reached, or undefined when it may run.
 * Exported so the rule is testable without a live merge request.
 */
export function reviewCapRefusal(report: { rounds: number; roundLimit: number }): string | undefined {
  if (report.rounds < report.roundLimit) return undefined;
  return `这次合并被拦了：机器评审已评 ${report.rounds} 轮，上限 ${report.roundLimit} 轮。请改人工 review，或先和 alex 确认是否放宽上限——不要重试这条命令。`;
}

const workflowStatus = defineTool({
  name: "workflow_status",
  description:
    "这条需求现在走到哪一步：开发、提测、发布、上线，以及机器评审用了几轮（上限三轮）。要合并 MR、或者用户问「现在到哪了」之前先调一次：轮次到顶时合并会被拦，答应之前就该知道。",
  parameters: obj({}),
  replay: "safe",
  execute: async (_args, api) => {
    const report = await stagesFor(api.conversationId as unknown as number);
    const lines = report.stages.map((stage) => {
      const head = `${stage.state === "done" ? "✓" : stage.state === "blocked" ? "✗" : stage.state === "active" ? "→" : "·"} ${stage.label}`;
      const parts = [head, stage.detail ?? "", stage.blockedBy ?? "", stage.nextAction ?? ""].filter((part) => part.length > 0);
      return parts.join(" · ");
    });
    lines.push(`评审轮次 ${report.rounds}/${report.roundLimit}（${report.branch} → ${report.target}）`);
    return { output: [{ type: "text" as const, text: lines.join("\n") }] };
  },
});

export const workflowExtension = defineExtension({
  name: "workflow",
  tools: [workflowStatus],
  hooks: [
    hook(ToolTask, {
      // A merge is the one call that cannot be taken back, so it is the one call this
      // extension reads before it runs. Every other git and glab call is free.
      beforeTool: async (call, api) => {
        if (call.name !== "bash") return undefined;
        const args = (call.arguments ?? {}) as Record<string, unknown>;
        const command = typeof args["command"] === "string" ? (args["command"] as string) : "";
        if (!/glab\s+mr\s+merge/.test(command)) return undefined;
        const conversationId = api.conversationId as unknown as number;
        const report = await stagesFor(conversationId).catch(() => undefined);
        if (report === undefined) return undefined;
        const refusal = reviewCapRefusal(report);
        if (refusal === undefined) return undefined;
        const seen = blockedThisTurn.get(String(conversationId)) ?? 0;
        if (seen >= 2) return undefined;
        blockedThisTurn.set(String(conversationId), seen + 1);
        log("workflow", "merge blocked by review cap", { conversationId, rounds: report.rounds });
        return { block: refusal };
      },
    }),
  ],
});
