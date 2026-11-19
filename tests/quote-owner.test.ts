import assert from "node:assert/strict";
import { test } from "node:test";

const APP = "cli_aa982a6359385cdc";
const BOT_OPEN_ID = "ou_0834bd15106922ed15900fb59477f901";

test("our own message is ours, by app id", async () => {
  const { writtenByApp } = await import("../src/feishu.ts");
  // What Feishu actually returns for a card the bot sent: id is the app id, open_id is empty.
  assert.equal(
    writtenByApp({ id: APP, open_bot_id: BOT_OPEN_ID, sender_type: "app" }, APP, BOT_OPEN_ID),
    true,
  );
  // The old check compared `id` with the bot's open_id, so it answered false for this same message.
  assert.equal(writtenByApp({ id: APP, sender_type: "app" }, APP, undefined), true);
});

test("a person quoting us is not us", async () => {
  const { writtenByApp } = await import("../src/feishu.ts");
  assert.equal(
    writtenByApp({ id: "ou_aef70b576acd2241c8b04c493589cc89", sender_type: "user" }, APP, BOT_OPEN_ID),
    false,
  );
});

test("another bot in the group is not us", async () => {
  const { writtenByApp } = await import("../src/feishu.ts");
  assert.equal(
    writtenByApp({ id: "cli_other", open_bot_id: "ou_other", sender_type: "app" }, APP, BOT_OPEN_ID),
    false,
  );
});
