/**
 * The bridge's view of `feishu-gw`: a local HTTP service that owns the Feishu long connection and
 * the app credentials.
 *
 * Two things make this worth a module instead of a few fetch calls at the call sites. First, the
 * response shape: the rest of `feishu.ts` reads `{code, msg, data}` straight off the SDK, and the
 * gateway answers in the same shape (Feishu's own `code`/`msg`, unwrapped from the envelope the
 * gateway returns), so switching transports does not touch error handling anywhere. Second, the
 * timeout: a gateway that hangs must look like a failed call, not a stuck turn.
 */

/** Feishu's own answer, in the shape the SDK returns it: `code === 0` is success. */
export type ImResponse = {
  code: number;
  msg: string;
  // Whatever Feishu put in `data`. Narrowing it here would only push casts into the call sites that
  // read ten different fields off it (message list, thread digest, quoted sender).
  data?: any;
};

export type FeishuIm = {
  message: {
    create(input: {
      params: { receive_id_type: "chat_id" };
      data: { receive_id: string; msg_type: string; content: string };
    }): Promise<ImResponse>;
    reply(input: {
      path: { message_id: string };
      data: { msg_type: string; content: string; reply_in_thread?: boolean };
    }): Promise<ImResponse>;
    patch(input: { path: { message_id: string }; data: { content: string } }): Promise<ImResponse>;
    get(input: { path: { message_id: string } }): Promise<ImResponse>;
  };
  messageResource: {
    get(input: {
      path: { message_id: string; file_key: string };
      params: { type: string };
    }): Promise<{ getReadableStream?: () => AsyncIterable<Buffer> }>;
  };
  botOpenId(): Promise<string | undefined>;
};

const OPERATION_TIMEOUT_MS = 15_000;
// An attachment can be a 30MB file on a slow link; the read is bounded by bytes, not by a deadline
// that would abort a download that is making progress.
const DOWNLOAD_TIMEOUT_MS = 120_000;

/** `FEISHU_TRANSPORT=go` moves the transport into feishu-gw; anything else keeps the SDK. */
export function gatewayEnabled(): boolean {
  return (process.env.FEISHU_TRANSPORT ?? "").trim().toLowerCase() === "go";
}

export function gatewayEndpoint(): { url: string; token: string } {
  return {
    url: (process.env.FEISHU_GW_URL ?? "http://127.0.0.1:8788").replace(/\/+$/, ""),
    token: process.env.FEISHU_GW_TOKEN ?? "",
  };
}

type Envelope = { code?: number; message?: string; feishuCode?: number; feishuMsg?: string };

/** A refusal or an error from the gateway, carrying Feishu's words when it has them. */
export class GatewayError extends Error {
  readonly status: number;
  readonly code: number;

  constructor(status: number, body: Envelope) {
    super(body.message ?? `gateway answered ${status}`);
    this.name = "GatewayError";
    this.status = status;
    this.code = body.feishuCode ?? -1;
  }
}

export function gatewayClient(endpoint: { url: string; token: string } = gatewayEndpoint()): FeishuIm {
  const call = async (method: string, path: string, body?: unknown, timeout = OPERATION_TIMEOUT_MS): Promise<Response> => {
    const response = await fetch(endpoint.url + path, {
      method,
      headers: {
        "content-type": "application/json",
        ...(endpoint.token.length > 0 ? { "x-pilot-token": endpoint.token } : {}),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(timeout),
    });
    if (!response.ok) {
      const envelope = (await response.json().catch(() => ({}))) as { error?: Envelope };
      // A 401 means the two sides disagree about FEISHU_GW_TOKEN, which no retry fixes.
      throw new GatewayError(response.status, envelope.error ?? {});
    }
    return response;
  };

  // The gateway speaks camelCase; the SDK's callers all read `data.message_id`.
  const messageIdOf = (body: unknown): ImResponse["data"] => ({
    message_id: (body as { messageId?: string }).messageId,
  });

  /** Feishu's failure and the gateway's are the same thing to a caller: a non-zero code. */
  const operation = async (method: string, path: string, body?: unknown): Promise<ImResponse> => {
    const response = await call(method, path, body);
    return (await response.json()) as ImResponse;
  };

  return {
    message: {
      async create(input) {
        const answer = await call("POST", "/v1/message", {
          chatId: input.data.receive_id,
          msgType: input.data.msg_type,
          content: input.data.content,
        });
        return { code: 0, msg: "success", data: messageIdOf(await answer.json()) };
      },
      async reply(input) {
        const answer = await call("POST", "/v1/reply", {
          messageId: input.path.message_id,
          msgType: input.data.msg_type,
          content: input.data.content,
          inThread: input.data.reply_in_thread === true,
        });
        return { code: 0, msg: "success", data: messageIdOf(await answer.json()) };
      },
      async patch(input) {
        await call("PATCH", `/v1/message/${encodeURIComponent(input.path.message_id)}`, {
          content: input.data.content,
        });
        return { code: 0, msg: "success" };
      },
      async get(input) {
        const answer = await operation("GET", `/v1/message/${encodeURIComponent(input.path.message_id)}/sender`);
        // The gateway answers `{openId, appId, senderType}`; put it back where the caller looks —
        // the quoted-message check reads `sender.id` / `sender.sender_type`.
        return {
          code: 0,
          msg: "success",
          data: {
            items: [
              {
                sender: {
                  id: (answer as unknown as { appId?: string }).appId,
                  open_bot_id: (answer as unknown as { openId?: string }).openId,
                  sender_type: (answer as unknown as { senderType?: string }).senderType,
                },
              },
            ],
          },
        };
      },
    },
    messageResource: {
      async get(input) {
        const query = new URLSearchParams({ fileKey: input.path.file_key, type: input.params.type });
        const response = await call(
          "GET",
          `/v1/message/${encodeURIComponent(input.path.message_id)}/resource?${query.toString()}`,
          undefined,
          DOWNLOAD_TIMEOUT_MS,
        );
        const bytes = Buffer.from(await response.arrayBuffer());
        return {
          async *getReadableStream() {
            yield bytes;
          },
        };
      },
    },
    async botOpenId() {
      const answer = (await operation("GET", "/v1/bot", undefined)) as unknown as { openId?: string };
      return answer.openId;
    },
  };
}
