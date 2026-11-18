import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createServer } from "node:net";
import type { AddressInfo } from "node:net";
import { join, resolve } from "node:path";
import { after, before, test } from "node:test";

/**
 * `/api/health` is what the desktop client's footer draws: uptime, queue depth
 * and version. These tests go through the real HTTP route, because the shape
 * that reaches the wire — and the promise that the route never answers 5xx — is
 * what the client depends on.
 */

const root = resolve(import.meta.dirname, "..");
const token = "route-test-token";

/** A port nothing else is holding: config is fixed at import, so it has to be picked first. */
async function freePort(): Promise<number> {
	const probe = createServer();
	await new Promise<void>((done) => probe.listen(0, "127.0.0.1", done));
	const { port } = probe.address() as AddressInfo;
	await new Promise<void>((done) => probe.close(() => done()));
	return port;
}

const port = await freePort();
// Set before the import below: config reads the environment once, at load time.
process.env["AGENT_HTTP_PORT"] = String(port);
process.env["AGENT_HTTP_TOKEN"] = token;

const { startAgentServer } = await import("../src/server.ts");
const { config } = await import("../src/config.ts");

/** Flip to true mid-test to act out a bridge whose storage has died. */
let storageDead = false;

const runtime = {
	harness: {
		inspect: async () => {
			if (storageDead) throw new Error("storage is dead");
			return {
				submissions: [{ status: "queued" }, { status: "placed" }],
				tasks: [{ state: { kind: "running" } }, { state: { kind: "done" } }],
			};
		},
		snapshot: async () => {
			if (storageDead) throw new Error("storage is dead");
			// Surfaces to conversations: the thread and its parent share one chat.
			return { chats: { "11": 1, "12": 2, "13": 2 } };
		},
	},
};

let server: { port: number; close(): Promise<void> };

before(async () => {
	server = await startAgentServer(runtime as never);
});

after(async () => {
	await server.close();
});

function health(auth: string = token): Promise<Response> {
	return fetch(`http://127.0.0.1:${port}/api/health`, {
		headers: auth === "" ? {} : { authorization: `Bearer ${auth}` },
	});
}

test("the bridge reports itself with the numbers the footer draws", async () => {
	const response = await health();
	assert.equal(response.status, 200);
	const body = (await response.json()) as Record<string, unknown>;

	const pkg = JSON.parse(readFileSync(join(root, "package.json"), "utf8")) as { version: string };
	assert.equal(body["pid"], process.pid);
	assert.equal(body["version"], pkg.version);

	const startedAt = Date.parse(String(body["startedAt"]));
	assert.ok(Number.isFinite(startedAt), `startedAt is not a time: ${String(body["startedAt"])}`);
	assert.ok(startedAt <= Date.now(), "startedAt is in the future");

	const uptimeMs = body["uptimeMs"];
	assert.equal(typeof uptimeMs, "number");
	assert.ok((uptimeMs as number) >= 0, `uptimeMs is negative: ${String(uptimeMs)}`);

	assert.equal(body["queueDepth"], 2, "queued and placed submissions are the queue");
	assert.equal(body["runningTurns"], 1, "only the running task counts");
	assert.equal(body["conversations"], 2, "two surfaces sharing a chat count once");
});

test("a wrong token is refused, an empty one is not accepted by accident", async () => {
	assert.equal((await health("")).status, 401);
	assert.equal((await health("not-the-token")).status, 401);
});

test("a dead storage still answers 200 with zeros, never 5xx", async () => {
	storageDead = true;
	try {
		const response = await health();
		// A 5xx here would make the client claim the agent is gone while it is
		// alive but unable to count its queue.
		assert.equal(response.status, 200);
		const body = (await response.json()) as Record<string, unknown>;
		assert.equal(body["queueDepth"], 0);
		assert.equal(body["runningTurns"], 0);
		assert.equal(body["conversations"], 0);
		assert.equal(body["pid"], process.pid, "the bridge still knows it is alive");
	} finally {
		storageDead = false;
	}
});

test("the route is served with the token the config was loaded with", () => {
	assert.equal(config.http.port, port, "config did not pick up AGENT_HTTP_PORT");
	assert.equal(config.http.token, token, "config did not pick up AGENT_HTTP_TOKEN");
});
