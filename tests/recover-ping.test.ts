import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

/**
 * A card patch raises no Feishu notification. The live turn pings for that reason; the recovery
 * path (a turn that finished after the process restarted) used to patch the card and stop there,
 * so the answer landed in a card nobody was told about — measured: "现在补发后没有通知".
 */
const source = readFileSync(new URL("../src/feishu.ts", import.meta.url), "utf8");

/** The recovery loop's body, from the leftover-card log line to the end of the for-loop. */
function recoveryBody(): string {
  const start = source.indexOf('"recovered answer delivered"');
  assert.ok(start > 0, "recovery delivery log line is gone");
  const end = source.indexOf("standaloneHeader(settled.reason", start);
  assert.ok(end > start, "recovery else-branch is gone");
  return source.slice(start, end + 200);
}

test("a recovered answer pings, because a card patch is silent", () => {
  const body = recoveryBody();
  assert.match(body, /await ping\(`✅ 答完了（重启后补发） · \$\{total\}`\)/);
});

test("recovery pings the same surface the live turn would", () => {
  const start = source.indexOf("if (card.messageId === undefined) return;");
  assert.ok(start > 0, "recovery ping helper is gone");
  const helper = source.slice(start, start + 700);
  // A group's root message is a broadcast: silent. A topic thread, and a p2p chat, notify.
  assert.match(helper, /chatType === "group" && card\.thread !== true/);
  // Too short to be news stays quiet, through the same gate the live path uses (the behaviour of
  // that gate is pinned in done-ping.test.ts; this file only checks it is wired here).
  assert.match(helper, /shouldPingDone\(config\.feishu\.donePingSeconds, Date\.now\(\) - card\.at\)/);
  assert.match(helper, /pingDone\(card\.chatId, card\.messageId, text/);
});

test("recovery takes the 👀 off the message it answered", () => {
  const start = source.indexOf('"recovered answer delivered"');
  assert.ok(start > 0);
  const tail = source.slice(start, source.indexOf("clearCardPending", start));
  // Left alone, the mark says "still working" about a turn that finished in a previous process.
  assert.match(tail, /await settle\(true\)/);
  const failed = source.slice(source.indexOf('`❌ 没答完（重启后补发）'));
  assert.match(failed.slice(0, 200), /await settle\(false\)/);
});

test("the mark is written down before the turn can die", () => {
  // The live turn's finally is what normally settles it; a crash skips that finally, so the id
  // has to be on the card record by then.
  assert.match(source, /markWorking\(message\.message_id\)[\s\S]{0,300}attachReaction\(/);
});

test("a pending card remembers where it was asked", () => {
  // Without chatType the recovery cannot tell a group root from a p2p chat.
  assert.match(source, /markCardPending\(runtime\.harness, context, requestId, \{[\s\S]{0,400}chatType: message\.chat_type/);
});
