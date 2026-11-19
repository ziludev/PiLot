import assert from "node:assert/strict";
import { createServer, type Server } from "node:http";
import { test } from "node:test";
import { gatewayClient, GatewayError, type FeishuIm } from "../src/feishu-gw.ts";

/** A stand-in for feishu-gw: every call is recorded, the answer is whatever the test wants. */
async function fakeGateway(
  answer: (request: { method: string; path: string; body: string; token: string }) => { status: number; body: unknown; raw?: Buffer },
): Promise<{ im: FeishuIm; calls: Array<{ method: string; path: string; body: string; token: string }>; server: Server; url: string }> {
  const calls: Array<{ method: string; path: string; body: string; token: string }> = [];
  const server = createServer(async (request, response) => {
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk as Buffer));
    const call = {
      method: request.method ?? "GET",
      path: request.url ?? "/",
      body: Buffer.concat(chunks).toString("utf8"),
      token: request.headers["x-pilot-token"] as string ?? "",
    };
    calls.push(call);
    const reply = answer(call);
    response.writeHead(reply.status, { "content-type": "application/json" });
    response.end(reply.raw ?? JSON.stringify(reply.body));
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  const url = typeof address === "object" && address !== null ? `http://127.0.0.1:${address.port}` : "";
  return { im: gatewayClient({ url, token: "secret" }), calls, server, url };
}

const close = async (server: Server): Promise<void> => {
  await new Promise<void>((resolve) => server.close(() => resolve()));
};

test("create sends the chat, type and content the SDK would have sent", async () => {
  const gateway = await fakeGateway(() => ({ status: 200, body: { messageId: "om_1" } }));
  try {
    const response = await gateway.im.message.create({
      params: { receive_id_type: "chat_id" },
      data: { receive_id: "oc_chat", msg_type: "text", content: JSON.stringify({ text: "hi" }) },
    });
    assert.equal(response.code, 0);
    assert.equal(response.data?.message_id, "om_1");
    const [call] = gateway.calls;
    assert.equal(call?.method, "POST");
    assert.equal(call?.path, "/v1/message");
    assert.equal(call?.token, "secret");
    assert.deepEqual(JSON.parse(call?.body ?? "{}"), {
      chatId: "oc_chat",
      msgType: "text",
      content: JSON.stringify({ text: "hi" }),
    });
  } finally {
    await close(gateway.server);
  }
});

test("a thread reply carries reply_in_thread through to the gateway", async () => {
  const gateway = await fakeGateway(() => ({ status: 200, body: { messageId: "om_2" } }));
  try {
    await gateway.im.message.reply({
      path: { message_id: "om_1" },
      data: { msg_type: "interactive", content: "{}", reply_in_thread: true },
    });
    assert.deepEqual(JSON.parse(gateway.calls[0]?.body ?? "{}"), {
      messageId: "om_1",
      msgType: "interactive",
      content: "{}",
      inThread: true,
    });
  } finally {
    await close(gateway.server);
  }
});

test("patch reports success with code 0 so a failing card is the only thing that throws", async () => {
  const gateway = await fakeGateway(() => ({ status: 200, body: { ok: true } }));
  try {
    const response = await gateway.im.message.patch({ path: { message_id: "om_9" }, data: { content: "{}" } });
    assert.equal(response.code, 0);
    assert.equal(gateway.calls[0]?.method, "PATCH");
    assert.equal(gateway.calls[0]?.path, "/v1/message/om_9");
  } finally {
    await close(gateway.server);
  }
});

test("Feishu's own failure code survives the gateway so logs still say why", async () => {
  const gateway = await fakeGateway(() => ({
    status: 502,
    body: { error: { code: "feishu_api", message: "patch failed: feishu 230001: invalid card", feishuCode: 230001, feishuMsg: "invalid card" } },
  }));
  try {
    await assert.rejects(
      gateway.im.message.patch({ path: { message_id: "om_9" }, data: { content: "{}" } }),
      (error: unknown) => {
        assert.ok(error instanceof GatewayError);
        assert.equal(error.code, 230001);
        assert.match(error.message, /invalid card/);
        return true;
      },
    );
  } finally {
    await close(gateway.server);
  }
});

test("a token mismatch fails loudly instead of silently posting nothing", async () => {
  const gateway = await fakeGateway(() => ({ status: 401, body: { error: { code: "unauthorized", message: "bad token" } } }));
  try {
    await assert.rejects(gateway.im.message.create({ params: { receive_id_type: "chat_id" }, data: { receive_id: "oc", msg_type: "text", content: "{}" } }), /bad token/);
  } finally {
    await close(gateway.server);
  }
});

test("sender lookup lands where the quoted-message check reads it", async () => {
  const gateway = await fakeGateway(() => ({ status: 200, body: { openId: "", appId: "cli_abc", senderType: "app" } }));
  try {
    const response = await gateway.im.message.get({ path: { message_id: "om_3" } });
    const sender = response.data?.items?.[0]?.sender;
    assert.equal(sender?.id, "cli_abc");
    assert.equal(sender?.sender_type, "app");
  } finally {
    await close(gateway.server);
  }
});

test("a download comes back as one readable stream, so the caller's copy path is unchanged", async () => {
  const bytes = Buffer.from([0x89, 0x50, 0x4e, 0x47]);
  const gateway = await fakeGateway(() => ({ status: 200, body: {}, raw: bytes }));
  try {
    const resource = await gateway.im.messageResource.get({
      path: { message_id: "om_4", file_key: "file_v2" },
      params: { type: "image" },
    });
    const chunks: Buffer[] = [];
    for await (const chunk of resource.getReadableStream?.() ?? []) chunks.push(chunk);
    assert.deepEqual(Buffer.concat(chunks), bytes);
    assert.match(gateway.calls[0]?.path ?? "", /fileKey=file_v2&type=image/);
  } finally {
    await close(gateway.server);
  }
});

test("bot identity comes from the gateway, since it holds the credentials", async () => {
  const gateway = await fakeGateway(() => ({ status: 200, body: { openId: "ou_bot", name: "PiLot" } }));
  try {
    assert.equal(await gateway.im.botOpenId(), "ou_bot");
  } finally {
    await close(gateway.server);
  }
});
