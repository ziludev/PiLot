import { defineExtension, defineTool } from "@earendil-works/pi-durable";
import { bool, obj, str } from "@earendil-works/pi-schema";

/**
 * A turn that ends is not a turn that worked. "✅ 答完了" only means the submission settled; the
 * card cannot tell "the tests pass" from "I ran out of steps while the tests were red". The model
 * knows, and it is the only one that does — so it files a claim, and the card repeats the claim
 * instead of inventing a verdict.
 *
 * Deliberately not a durable doc: the claim belongs to the turn that made it, and a stale claim
 * read by the next turn would be a lie with a timestamp. In memory, read once, gone.
 */

type DoneClaim = { met: boolean; why: string; at: number };

const CLAIMS = new Map<string, DoneClaim>();

export function claimDone(key: string, claim: DoneClaim): void {
  CLAIMS.set(key, claim);
}

/** Reads and clears: the verdict belongs to exactly one card. */
export function takeDone(key: string): DoneClaim | undefined {
  const claim = CLAIMS.get(key);
  CLAIMS.delete(key);
  return claim;
}

/** What the card header says for a claim. Undeclared turns keep the plain "答完了". */
export function doneVerdict(claim: DoneClaim): { template: "green" | "orange"; title: string } {
  return claim.met
    ? { template: "green", title: "✅ 目标达成" }
    : { template: "orange", title: "⚠️ 目标未达成" };
}

const reportDone = defineTool({
  name: "report_done",
  description:
    "声明这一轮是否达成了 alex 要的目标。只要你替他干了活（改文件、跑命令、查清一个问题、部署、排查）就要在最后一轮调用一次：met=true 时 why 写「怎么验证的」（跑了什么、看到什么），met=false 时 why 写「卡在哪、还差什么」。纯闲聊、纯问答、纯咨询不用调。",
  parameters: obj({
    met: bool({ description: "目标达成了吗" }),
    why: str({ description: "一句话：怎么验证的，或者卡在哪" }),
  }),
  replay: "safe",
  execute: async (args, api) => {
    const why = args.why.trim().slice(0, 200);
    claimDone(String(api.conversationId), { met: args.met, why, at: Date.now() });
    return {
      output: [{ type: "text" as const, text: `已记录：${args.met ? "目标达成" : "目标未达成"} — ${why}` }],
    };
  },
});

export const doneExtension = defineExtension({ name: "done", tools: [reportDone] });
