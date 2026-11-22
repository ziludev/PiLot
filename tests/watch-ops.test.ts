import assert from "node:assert/strict";
import { createServer } from "node:net";
import type { AddressInfo } from "node:net";
import { after, before, test } from "node:test";
import WebSocket from "ws";

/**
 * The socket's two contracts live here: without `?ops=1` every frame keeps the
 * shape older clients parse (`value` plus `ops`), and with it only the first
 * frame carries a `value`. The desktop client depends on the second shape being
 * a delta and nothing more; a stray `value` would let it skip applying ops and
 * quietly stop tracking the conversation.
 */

const token = "watch-ops-test-token";

async function freePort(): Promise<number> {
	const probe = createServer();
	await new Promise<void>((done) => probe.listen(0, "127.0.0.1", done));
	const { port } = probe.address() as AddressInfo;
	await new Promise<void>((done) => probe.close(() => done()));
	return port;
}

const port = await freePort();
process.env["AGENT_HTTP_PORT"] = String(port);
process.env["AGENT_HTTP_TOKEN"] = token;

const { startAgentServer } = await import("../src/server.ts");

type Commit = { kind: "value"; value: unknown; ops?: unknown[] };

const initial = {
	entries: [{ id: 1, kind: "pi.user", model: null, data: { text: "hi" } }],
	docs: { "pi.live": { run: { status: "running" }, generation: null, tools: [] } },
};
const committed = {
	entries: [
		{ id: 1, kind: "pi.user", model: null, data: { text: "hi" } },
		{ id: 2, kind: "pi.assistant", model: null, data: { text: "hello" } },
	],
	docs: { "pi.live": { run: { status: "running" }, generation: null, tools: [], busy: true } },
};

/** Each socket attach gets its own listener list; the newest one is the live socket. */
const watchers: Array<(call: Commit) => void> = [];
const conversation = {
	id: 7,
	watch: async () => {
		const local: Array<(call: Commit) => void> = [];
		watchers.push((call) => local.forEach((listener) => listener(call)));
		return {
			value: initial,
			start(listener: (call: Commit) => void) {
				local.push(listener);
			},
			stop: async () => {},
		};
	},
};

const runtime = {
	harness: {
		conversation: async (id: number) => (id === 7 ? conversation : undefined),
	},
	attachSurface: async () => {},
};

function emit(call: Commit): void {
	const listener = watchers.at(-1);
	assert.ok(listener, "no socket has attached yet");
	listener(call);
}

/** Fan a commit out to every attached socket, for tests with two modes at once. */
function emitAll(call: Commit): void {
	assert.ok(watchers.length > 0, "no socket has attached yet");
	for (const listener of watchers) listener(call);
}

type Probe = {
	next(timeout?: number): Promise<Record<string, unknown>>;
	none(ms?: number): Promise<void>;
	close(): void;
};

async function open(path: string): Promise<Probe> {
	const socket = new WebSocket(`ws://127.0.0.1:${port}${path}`);
	const buffered: Record<string, unknown>[] = [];
	const waiters: Array<(frame: Record<string, unknown>) => void> = [];
	socket.on("message", (data) => {
		const frame = JSON.parse(String(data)) as Record<string, unknown>;
		const waiter = waiters.shift();
		if (waiter) waiter(frame);
		else buffered.push(frame);
	});
	await new Promise<void>((resolve, reject) => {
		socket.once("open", () => resolve());
		socket.once("error", reject);
	});
	return {
		next(timeout = 2000) {
			if (buffered.length > 0) return Promise.resolve(buffered.shift() as Record<string, unknown>);
			return new Promise((resolve, reject) => {
				const timer = setTimeout(() => reject(new Error("timed out waiting for a frame")), timeout);
				waiters.push((frame) => {
					clearTimeout(timer);
					resolve(frame);
				});
			});
		},
		async none(ms = 200) {
			await new Promise((resolve) => setTimeout(resolve, ms));
			assert.equal(buffered.length, 0, `unexpected frame: ${JSON.stringify(buffered[0])}`);
			assert.equal(waiters.length, 0, "a frame arrived in the quiet window");
		},
		close() {
			socket.close();
		},
	};
}

let server: { port: number; close(): Promise<void> };

before(async () => {
	server = await startAgentServer(runtime as never);
});

after(async () => {
	await server.close();
});

test("without ?ops=1 every frame keeps the old value-plus-ops shape", async () => {
	const socket = await open(`/ws?conversationId=7&token=${token}`);

	const first = await socket.next();
	assert.equal(first["type"], "value");
	assert.deepEqual(first["value"], initial);
	assert.deepEqual(first["ops"], []);

	const ops = [["s", ["docs", "pi.live", "busy"], true]];
	emit({ kind: "value", value: committed, ops });
	const second = await socket.next();
	assert.equal(second["type"], "value");
	assert.deepEqual(second["value"], committed);
	assert.deepEqual(second["ops"], ops);
	socket.close();
});

test("?ops=1 sends a full first frame and then deltas without a value", async () => {
	const socket = await open(`/ws?conversationId=7&token=${token}&ops=1`);

	const first = await socket.next();
	assert.equal(first["type"], "value");
	assert.deepEqual(first["value"], initial);
	assert.equal("ops" in first, false, "the baseline needs no delta");

	const ops = [
		["p", ["entries"], 1, 0, [{ id: 2, kind: "pi.assistant", model: null, data: { text: "hello" } }]],
		["s", ["docs", "pi.live", "busy"], true],
	];
	emit({ kind: "value", value: committed, ops });
	const second = await socket.next();
	assert.equal(second["type"], "ops");
	assert.deepEqual(second["ops"], ops);
	assert.equal("value" in second, false, "a delta frame must not carry a full view");
	socket.close();
});

test("?ops=1 skips a commit whose delta is empty", async () => {
	const socket = await open(`/ws?conversationId=7&token=${token}&ops=1`);
	await socket.next();

	emit({ kind: "value", value: committed, ops: [] });
	await socket.none();
	socket.close();
});

test("?ops=1 falls back to a full frame when the commit has no delta", async () => {
	const socket = await open(`/ws?conversationId=7&token=${token}&ops=1`);
	await socket.next();

	emit({ kind: "value", value: committed });
	const frame = await socket.next();
	assert.equal(frame["type"], "value");
	assert.deepEqual(frame["value"], committed);
	socket.close();
});

test("?ops=1 forwards a mid-stream root replacement as a delta", async () => {
	const socket = await open(`/ws?conversationId=7&token=${token}&ops=1`);
	await socket.next();

	const ops = [["r", committed]];
	emit({ kind: "value", value: committed, ops });
	const frame = await socket.next();
	assert.equal(frame["type"], "ops");
	assert.deepEqual(frame["ops"], ops);
	socket.close();
});

test("only ops=1 opts in; ops=0 and other values keep the legacy shape", async () => {
	for (const param of ["ops=0", "ops=true", "ops=1x", "ops="]) {
		const socket = await open(`/ws?conversationId=7&token=${token}&${param}`);
		const first = await socket.next();
		assert.equal(first["type"], "value", `${param}: first frame type`);
		assert.deepEqual(first["value"], initial, `${param}: first frame value`);
		assert.deepEqual(first["ops"], [], `${param}: the legacy frame carries ops`);

		const ops = [["s", ["docs", "pi.live", "busy"], true]];
		emit({ kind: "value", value: committed, ops });
		const next = await socket.next();
		assert.equal(next["type"], "value", `${param} must not opt into deltas`);
		assert.deepEqual(next["value"], committed, `${param}: full value`);
		assert.deepEqual(next["ops"], ops, `${param}: unchanged client still sees the delta`);
		socket.close();
	}
});

test("?ops=1 falls back to a full frame when the delta is not an array", async () => {
	const socket = await open(`/ws?conversationId=7&token=${token}&ops=1`);
	await socket.next();

	emit({ kind: "value", value: committed, ops: "broken" as unknown as unknown[] });
	const frame = await socket.next();
	assert.equal(frame["type"], "value");
	assert.deepEqual(frame["value"], committed);
	socket.close();
});

test("two sockets on one conversation each keep their own frame shape", async () => {
	const legacy = await open(`/ws?conversationId=7&token=${token}`);
	const delta = await open(`/ws?conversationId=7&token=${token}&ops=1`);
	const legacyFirst = await legacy.next();
	const deltaFirst = await delta.next();
	assert.equal(legacyFirst["type"], "value");
	assert.deepEqual(legacyFirst["ops"], []);
	assert.equal(deltaFirst["type"], "value");
	assert.equal("ops" in deltaFirst, false);

	const ops = [["s", ["docs", "pi.live", "busy"], true]];
	emitAll({ kind: "value", value: committed, ops });
	const legacyFrame = await legacy.next();
	const deltaFrame = await delta.next();
	assert.equal(legacyFrame["type"], "value");
	assert.deepEqual(legacyFrame["value"], committed);
	assert.deepEqual(legacyFrame["ops"], ops);
	assert.equal(deltaFrame["type"], "ops");
	assert.equal("value" in deltaFrame, false);
	assert.deepEqual(deltaFrame["ops"], ops);
	legacy.close();
	delta.close();
});
