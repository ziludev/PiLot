import assert from "node:assert/strict";
import { test } from "node:test";
import { claimDone, doneVerdict, takeDone } from "../src/done.ts";

/**
 * The verdict belongs to the turn that filed it. A claim that survived its turn would let one
 * card's "目标达成" appear at the top of the next one, which is worse than no verdict at all.
 */

test("a claim is read once", () => {
  claimDone("11", { met: true, why: "npm test 128 pass", at: 1_000 });
  assert.deepEqual(takeDone("11"), { met: true, why: "npm test 128 pass", at: 1_000 });
  assert.equal(takeDone("11"), undefined);
});

test("conversations do not see each other's verdict", () => {
  claimDone("12", { met: false, why: "卡在权限", at: 1 });
  assert.equal(takeDone("13"), undefined);
  assert.equal(takeDone("12")?.met, false);
});

test("the header says what was claimed, in the colour it deserves", () => {
  assert.deepEqual(doneVerdict({ met: true, why: "", at: 0 }), { template: "green", title: "✅ 目标达成" });
  assert.deepEqual(doneVerdict({ met: false, why: "测试还是红的", at: 0 }), {
    template: "orange",
    title: "⚠️ 目标未达成",
  });
});
