import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { after, before, test } from "node:test";
import { createEditTool as createInProcessEdit, createReadTool as createInProcessRead, createWriteTool as createInProcessWrite } from "@earendil-works/pi-durable/tools";
import { NodeExecutionEnv } from "@earendil-works/pi-durable/env/node";
import type { ToolExecutionApi, ToolExecutionResult, ToolRegistration } from "@earendil-works/pi-durable";
import { Toolbox } from "../src/toolbox/client.ts";
import { createBashTool } from "../src/toolbox/tools/bash.ts";
import { createEditToolProxy, createWriteToolProxy } from "../src/toolbox/tools/files.ts";
import { createReadToolProxy } from "../src/toolbox/tools/read.ts";

/**
 * Parity for `read`, `write`, and `edit`: one input, two implementations, the same
 * model-visible result — text, diagnostics, details (the diff and the patch), the
 * bytes left on disk, and the error text when something is refused.
 */

const root = resolve(import.meta.dirname, "..");
const bin = join(root, "toolbox-go", "bin", "toolbox");
const dir = mkdtempSync(join(tmpdir(), "tbx-files-"));
const work = join(dir, "work");
const readings = join(work, "readings");
const writing = join(work, "writing");
const socketPath = join(dir, "sock");
let toolbox: Toolbox;

const skip = (() => {
  if (existsSync(bin)) return false;
  try {
    execFileSync("go", ["version"], { stdio: "ignore" });
    return false;
  } catch {
    return "no go toolchain and no prebuilt toolbox-go/bin/toolbox";
  }
})();

type Seen = {
  text: string;
  diagnostics: { severity: string; code: string; message: string }[];
  details: unknown;
  structuredOutput: unknown;
  isError: boolean;
  error: string | undefined;
  outputTypes: string[];
};

function fakeApi(cwd: string): ToolExecutionApi {
  return {
    env: new NodeExecutionEnv({ cwd }),
    output: () => {},
    outputWindow: undefined,
    retainedOutput: () => ({ text: "", truncated: false }),
    diagnostic: () => {},
    details: undefined,
    // The reference read tool asks the conversation model what it can see; no model means defaults.
    agent: async () => ({ model: undefined }),
    models: { getModel: () => undefined },
  } as unknown as ToolExecutionApi;
}

async function run(tool: ToolRegistration, args: Record<string, unknown>, cwd: string): Promise<Seen> {
  const seen: Seen = {
    text: "",
    diagnostics: [],
    details: undefined,
    structuredOutput: undefined,
    isError: false,
    error: undefined,
    outputTypes: [],
  };
  try {
    const result = (await tool.execute(args, fakeApi(cwd), {})) as ToolExecutionResult;
    const parts = (result.output ?? []) as Array<{ type: string; text?: string }>;
    seen.outputTypes = parts.map((part) => part.type);
    seen.text = parts
      .filter((part) => part.type === "text")
      .map((part) => part.text ?? "")
      .join("");
    seen.diagnostics = (result.diagnostics ?? []).map((diagnostic) => ({ ...diagnostic }));
    seen.details = result.details;
    seen.structuredOutput = result.structuredOutput;
    seen.isError = result.isError === true;
  } catch (error) {
    seen.error = (error as Error).message;
  }
  return seen;
}

/** The two paths write to different files, so every mention of a root is folded away. */
function scrub(value: unknown, roots: string[]): unknown {
  if (typeof value === "string") {
    let text = value;
    // One placeholder for either side's root, longest first so a nested root is not split twice.
    for (const from of [...roots].sort((a, b) => b.length - a.length)) text = text.split(from).join("<root>");
    return text;
  }
  if (Array.isArray(value)) return value.map((item) => scrub(item, roots));
  if (value !== null && typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [key, item] of Object.entries(value as Record<string, unknown>)) out[key] = scrub(item, roots);
    return out;
  }
  return value;
}

async function parity(
  reference: ToolRegistration,
  candidate: ToolRegistration,
  args: Record<string, unknown>,
  roots: { reference: string; candidate: string },
): Promise<Seen> {
  const want = await run(reference, args, roots.reference);
  const got = await run(candidate, args, roots.candidate);
  const scrubBoth = (seen: Seen) => ({
    ...seen,
    text: scrub(seen.text, [roots.reference, roots.candidate]) as string,
    error: scrub(seen.error, [roots.reference, roots.candidate]) as string | undefined,
    diagnostics: scrub(seen.diagnostics, [roots.reference, roots.candidate]),
    details: scrub(seen.details, [roots.reference, roots.candidate]),
    structuredOutput: scrub(seen.structuredOutput, [roots.reference, roots.candidate]),
  });
  assert.deepEqual(scrubBoth(got), scrubBoth(want), `paths disagree for ${JSON.stringify(args)}`);
  return want;
}

before(async () => {
  if (skip) return;
  for (const base of [readings, writing]) {
    mkdirSync(join(base, "ref"), { recursive: true });
    mkdirSync(join(base, "go"), { recursive: true });
  }
  toolbox = await Toolbox.start({ bin, socketPath });
});

after(async () => {
  await toolbox?.close();
  rmSync(dir, { recursive: true, force: true });
});

const readTool = () => createInProcessRead();
const readProxy = () => createReadToolProxy(toolbox);
const writeTool = () => createInProcessWrite();
const writeProxy = () => createWriteToolProxy(toolbox);
const editTool = () => createInProcessEdit();
const editProxy = () => createEditToolProxy(toolbox);

/** Both sides read the same file, so a shared directory keeps the paths equal. */
function seedBoth(base: string, name: string, content: string): { reference: string; candidate: string } {
  for (const side of ["ref", "go"]) writeFileSync(join(base, side, name), content);
  return { reference: join(base, "ref"), candidate: join(base, "go") };
}

test("parity: read a whole file", { skip }, async () => {
  const roots = seedBoth(readings, "plain.txt", "one\ntwo\nthree\n");
  const seen = await parity(readTool(), readProxy(), { path: "plain.txt" }, roots);
  assert.equal(seen.text, "one\ntwo\nthree\n");
  assert.equal(seen.details, undefined);
  assert.deepEqual(seen.diagnostics, []);
  assert.equal(seen.structuredOutput, "one\ntwo\nthree\n");
});

test("parity: read with offset and limit", { skip }, async () => {
  const roots = seedBoth(readings, "offset.txt", "1\n2\n3\n4\n5\n");
  const seen = await parity(readTool(), readProxy(), { path: "offset.txt", offset: 2, limit: 2 }, roots);
  assert.equal(seen.text, "2\n3");
  assert.deepEqual(seen.diagnostics, [{ severity: "info", message: "3 more lines in file. Use offset=4 to continue." }]);
  // A user-requested window is not the tool's truncation, so no details are reported.
  assert.equal(seen.details, undefined);
});

test("parity: read truncates a long file the same way", { skip }, async () => {
  const lines = Array.from({ length: 3000 }, (_, index) => `line ${index + 1}`).join("\n");
  const roots = seedBoth(readings, "long.txt", `${lines}\n`);
  const seen = await parity(readTool(), readProxy(), { path: "long.txt" }, roots);
  const details = seen.details as { truncation: Record<string, unknown> };
  assert.equal(details.truncation.outputLines, 2000);
  assert.equal(details.truncation.truncated, true);
  assert.deepEqual(seen.diagnostics, [
    { severity: "info", code: "truncated", message: "Showing lines 1-2000 of 3001. Use offset=2001 to continue." },
  ]);
});

test("parity: read refuses a missing file with the same text", { skip }, async () => {
  const roots = { reference: join(readings, "ref"), candidate: join(readings, "go") };
  const seen = await parity(readTool(), readProxy(), { path: "nope.txt" }, roots);
  assert.match(seen.error ?? "", /^ENOENT: no such file or directory, open '.*nope\.txt'$/);
});

test("parity: read sends an image through unchanged", { skip }, async () => {
  // A 1x1 PNG, enough to exercise the sniffing, the base64 round trip, and the limits.
  const png = Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFAAH/q842iQAAAABJRU5ErkJggg==",
    "base64",
  );
  for (const side of ["ref", "go"]) writeFileSync(join(readings, side, "dot.png"), png);
  const roots = { reference: join(readings, "ref"), candidate: join(readings, "go") };
  const seen = await parity(readTool(), readProxy(), { path: "dot.png" }, roots);
  assert.deepEqual(seen.outputTypes, ["image"]);
  const image = seen.structuredOutput as { type: string; data: string; mimeType: string };
  assert.equal(image.type, "image");
  assert.equal(image.mimeType, "image/png");
  assert.deepEqual(Buffer.from(image.data, "base64"), png);
});

/** A BMP whose header parses: declared size 128, pixel data at 54, 12-byte DIB, 24 bpp. */
function bmpBytes(): Buffer {
  const bytes = Buffer.alloc(128);
  bytes.write("BM", 0, "ascii");
  bytes.writeUInt32LE(128, 2);
  bytes.writeUInt32LE(54, 10);
  bytes.writeUInt32LE(12, 14);
  bytes.writeUInt16LE(1, 22);
  bytes.writeUInt16LE(24, 24);
  return bytes;
}

/** A PNG header with the given chunks: type and payload, each with a zeroed CRC. */
function pngBytes(chunks: Array<[string, Buffer]>): Buffer {
  const parts: Buffer[] = [Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])];
  for (const [type, payload] of chunks) {
    const length = Buffer.alloc(4);
    length.writeUInt32BE(payload.length, 0);
    parts.push(length, Buffer.from(type, "ascii"), payload, Buffer.alloc(4));
  }
  return Buffer.concat(parts);
}

test("parity: read refuses a bmp with the conversion message", { skip }, async () => {
  for (const side of ["ref", "go"]) writeFileSync(join(readings, side, "tiny.bmp"), bmpBytes());
  const roots = { reference: join(readings, "ref"), candidate: join(readings, "go") };
  const seen = await parity(readTool(), readProxy(), { path: "tiny.bmp" }, roots);
  assert.equal(seen.isError, true);
  assert.deepEqual(seen.diagnostics, [
    {
      severity: "error",
      code: "unsupported_image",
      message: "tiny.bmp is an image (image/bmp) that needs converting, and no image processor is configured",
    },
  ]);
});

test("parity: read treats an animated png as text", { skip }, async () => {
  const ihdr = Buffer.from([0, 0, 0, 2, 0, 0, 0, 3, 8, 6, 0, 0, 0]);
  const animated = pngBytes([
    ["IHDR", ihdr],
    ["acTL", Buffer.from([0, 0, 0, 2, 0, 0, 0, 0])],
  ]);
  for (const side of ["ref", "go"]) writeFileSync(join(readings, side, "anim.png"), animated);
  const roots = { reference: join(readings, "ref"), candidate: join(readings, "go") };
  const seen = await parity(readTool(), readProxy(), { path: "anim.png" }, roots);
  assert.deepEqual(seen.outputTypes, ["text"]);
  assert.equal(seen.isError, false);
});

test("parity: read treats a jpeg with a 4th byte of f7 as text", { skip }, async () => {
  const odd = Buffer.from([0xff, 0xd8, 0xff, 0xf7, 0x00, 0x10, 0x4a, 0x46]);
  for (const side of ["ref", "go"]) writeFileSync(join(readings, side, "odd.jpg"), odd);
  const roots = { reference: join(readings, "ref"), candidate: join(readings, "go") };
  const seen = await parity(readTool(), readProxy(), { path: "odd.jpg" }, roots);
  assert.deepEqual(seen.outputTypes, ["text"]);
});

test("parity: read returns the raw lines and adds no line numbers", { skip }, async () => {
  const roots = seedBoth(readings, "numbered.txt", "alpha\nbeta\ngamma\n");
  const seen = await parity(readTool(), readProxy(), { path: "numbered.txt" }, roots);
  assert.equal(seen.text, "alpha\nbeta\ngamma\n");
  assert.doesNotMatch(seen.text, /^\s*\d+[|:]\s/m, "no line-number gutter on either path");
});

test("parity: read with a fractional offset and limit", { skip }, async () => {
  const roots = seedBoth(readings, "fractional.txt", "1\n2\n3\n4\n5\n");
  const seen = await parity(readTool(), readProxy(), { path: "fractional.txt", offset: 2.5, limit: 2.5 }, roots);
  // sliceIndex truncates: the window is lines 2-4, and the continuation arithmetic stays fractional.
  assert.equal(seen.text, "2\n3\n4");
  assert.equal(seen.details, undefined);
  assert.deepEqual(seen.diagnostics, [{ severity: "info", message: "2 more lines in file. Use offset=5 to continue." }]);
});

test("parity: read drops a byte order mark like the reference", { skip }, async () => {
  // A BOM is not exotic: editors on Windows and several export tools write one.
  const roots = seedBoth(readings, "bom.txt", "\uFEFFfirst\nsecond\n");
  const seen = await parity(readTool(), readProxy(), { path: "bom.txt" }, roots);
  assert.equal(seen.text, "first\nsecond\n");
  const truncated = await parity(readTool(), readProxy(), { path: "bom.txt", offset: 2, limit: 5 }, roots);
  assert.equal(truncated.text, "second\n");
});

test("parity: edit matches through a compatibility ligature", { skip }, async () => {
  // NFKC folds U+FB01 to "fi", so text typed in ASCII still finds the ligature in the file.
  const roots = seedBoth(writing, "lig.txt", "The \uFB01le is here\n");
  const seen = await parity(
    editTool(),
    editProxy(),
    { path: "lig.txt", edits: [{ oldText: "The file is here", newText: "changed" }] },
    roots,
  );
  assert.match(seen.text, /Successfully replaced 1 block/);
  assert.equal(readFileSync(join(roots.candidate, "lig.txt"), "utf8"), "changed\n");
  assert.equal(seen.details?.diff, "-1 The \uFB01le is here\n+1 changed");
});

test("parity: read finds a name written in NFD", { skip }, async () => {
  // macOS stores filenames decomposed, so a composed query has to be retried in NFD.
  const decomposed = "cafe\u0301.txt";
  const roots = seedBoth(readings, decomposed, "decomposed name\n");
  const seen = await parity(readTool(), readProxy(), { path: "caf\u00E9.txt" }, roots);
  assert.equal(seen.text, "decomposed name\n");
});

test("parity: read matches over generated files and windows", { skip }, async () => {
  // Deterministic fuzz over blank-line-heavy content, offsets and limits: the blank-first-line bug
  // was invisible to hand-written cases, so the comparison runs over generated ones too.
  let state = 0x2f6e2b1;
  const next = (bound: number) => {
    state = (state * 1103515245 + 12345) & 0x7fffffff;
    return state % bound;
  };
  for (let index = 0; index < 120; index++) {
    const lines = Array.from({ length: 1 + next(6) }, () => "x".repeat(next(4)));
    const content = `${lines.join("\n")}${next(2) === 0 ? "\n" : ""}`;
    const args: Record<string, unknown> = {};
    if (next(3) === 0) args.offset = 1 + next(5);
    if (next(3) === 0) args.limit = 1 + next(4);
    const name = `fuzz-${index}.txt`;
    const roots = seedBoth(readings, name, content);
    const seen = await parity(readTool(), readProxy(), { path: name, ...args }, roots);
    assert.ok(seen, `${name}: both sides must answer`);
  }
});

test("parity: read keeps a blank first line of the selection", { skip }, async () => {
  // An empty line is a line: the reference keeps its separator, so the text starts with "\n".
  const cases: Array<[string, string, Record<string, unknown>, string]> = [
    ["blank.txt", "\nhello\n", {}, "\nhello\n"],
    ["twoblank.txt", "\n\nhello\n", {}, "\n\nhello\n"],
    ["newline.txt", "\n", {}, "\n"],
    ["middle.txt", "hello\n\nworld\n", { offset: 2 }, "\nworld\n"],
    ["limited.txt", "\nhello\nworld\n", { limit: 2 }, "\nhello"],
  ];
  for (const [name, content, args, want] of cases) {
    const roots = seedBoth(readings, name, content);
    const seen = await parity(readTool(), readProxy(), { path: name, ...args }, roots);
    assert.equal(seen.text, want, `${name}: ${JSON.stringify(seen)}`);
  }
});

test("parity: read refuses a directory with the same text", { skip }, async () => {
  for (const side of ["ref", "go"]) mkdirSync(join(readings, side, "folder"), { recursive: true });
  const roots = { reference: join(readings, "ref"), candidate: join(readings, "go") };
  const seen = await parity(readTool(), readProxy(), { path: "folder" }, roots);
  // Node's read error carries no path, so the message names the operation only.
  assert.equal(seen.error, "EISDIR: illegal operation on a directory, read");
});

test("parity: edit matches a compatibility character the newer table knows", { skip }, async () => {
  // U+1E030 folds to a Cyrillic "а" only in the Unicode 17 tables the runtime uses.
  const roots = seedBoth(writing, "unicode17.txt", "the \u{1E030} letter\n");
  const seen = await parity(
    editTool(),
    editProxy(),
    { path: "unicode17.txt", edits: [{ oldText: "the \u0430 letter", newText: "matched" }] },
    roots,
  );
  assert.match(seen.text, /Successfully replaced 1 block/);
  assert.equal(readFileSync(join(roots.candidate, "unicode17.txt"), "utf8"), "matched\n");
});

test("parity: write creates parent directories and reports the path", { skip }, async () => {
  const roots = { reference: join(writing, "ref"), candidate: join(writing, "go") };
  const seen = await parity(writeTool(), writeProxy(), { path: "deep/nested/file.txt", content: "hello\n" }, roots);
  assert.match(seen.text, /^Successfully wrote to .*deep\/nested\/file\.txt$/);
  for (const root of [roots.reference, roots.candidate]) {
    assert.equal(readFileSync(join(root, "deep/nested/file.txt"), "utf8"), "hello\n");
  }
});

test("parity: write refuses an unusable path with the same text", { skip }, async () => {
  for (const side of ["ref", "go"]) mkdirSync(join(writing, side, "folder"), { recursive: true });
  const roots = { reference: join(writing, "ref"), candidate: join(writing, "go") };
  const seen = await parity(writeTool(), writeProxy(), { path: "folder", content: "x" }, roots);
  assert.match(seen.error ?? "", /^EISDIR: illegal operation on a directory, open '.*folder'$/);
});

test("parity: write under a file parent reports realpath like the reference", { skip }, async () => {
  for (const side of ["ref", "go"]) writeFileSync(join(writing, side, "blocker"), "x");
  const roots = { reference: join(writing, "ref"), candidate: join(writing, "go") };
  const seen = await parity(writeTool(), writeProxy(), { path: "blocker/deeper/file.txt", content: "hi" }, roots);
  assert.match(seen.error ?? "", /^ENOTDIR: not a directory, realpath '.*blocker\/deeper\/file\.txt'$/);
});

test("parity: edit replaces one block and reports the same diff", { skip }, async () => {
  const roots = seedBoth(writing, "edit.txt", "alpha\nbeta\ngamma\n");
  const seen = await parity(
    editTool(),
    editProxy(),
    { path: "edit.txt", edits: [{ oldText: "beta", newText: "BETA" }] },
    roots,
  );
  assert.match(seen.text, /^Successfully replaced 1 block\(s\) in /);
  const details = seen.details as { diff: string; patch: string; firstChangedLine?: number };
  assert.equal(details.diff, " 1 alpha\n-2 beta\n+2 BETA\n 3 gamma");
  assert.equal(details.firstChangedLine, 2);
  assert.match(details.patch, /^--- edit\.txt\n\+\+\+ edit\.txt\n@@ -1,3 \+1,3 @@/);
  for (const root of [roots.reference, roots.candidate]) {
    assert.equal(readFileSync(join(root, "edit.txt"), "utf8"), "alpha\nBETA\ngamma\n");
  }
});

test("parity: edit handles several blocks and a rewrite far down the file", { skip }, async () => {
  // Unique markers: "line 2" would also match "line 20", and the tool rejects ambiguous edits.
  const body = Array.from({ length: 40 }, (_, index) => `L${String(index + 1).padStart(2, "0")} body`).join("\n");
  const roots = seedBoth(writing, "many.txt", `${body}\n`);
  const seen = await parity(
    editTool(),
    editProxy(),
    {
      path: "many.txt",
      edits: [
        { oldText: "L02 body", newText: "L02 changed" },
        { oldText: "L30 body", newText: "L30 changed" },
      ],
    },
    roots,
  );
  const details = seen.details as { diff: string; firstChangedLine?: number };
  assert.equal(details.firstChangedLine, 2);
  assert.match(details.diff, /- 2 L02 body\n\+ 2 L02 changed/);
  assert.match(details.diff, /-30 L30 body\n\+30 L30 changed/);
  for (const root of [roots.reference, roots.candidate]) {
    const text = readFileSync(join(root, "many.txt"), "utf8");
    assert.match(text, /L02 changed\n/);
    assert.match(text, /L30 changed\n/);
  }
});

test("parity: edit keeps CRLF and a BOM", { skip }, async () => {
  const roots = seedBoth(writing, "crlf.txt", "\uFEFFone\r\ntwo\r\n");
  await parity(editTool(), editProxy(), { path: "crlf.txt", edits: [{ oldText: "two", newText: "TWO" }] }, roots);
  for (const root of [roots.reference, roots.candidate]) {
    assert.deepEqual(readFileSync(join(root, "crlf.txt")), Buffer.from("\uFEFFone\r\nTWO\r\n", "utf8"));
  }
});

test("parity: edit refuses text it cannot find", { skip }, async () => {
  const roots = seedBoth(writing, "nofound.txt", "abc\n");
  const seen = await parity(editTool(), editProxy(), { path: "nofound.txt", edits: [{ oldText: "zzz", newText: "y" }] }, roots);
  assert.match(seen.error ?? "", /Could not find the exact text in .*nofound\.txt/);
});

test("parity: edit refuses an ambiguous match", { skip }, async () => {
  const roots = seedBoth(writing, "dupe.txt", "same\nsame\n");
  const seen = await parity(editTool(), editProxy(), { path: "dupe.txt", edits: [{ oldText: "same", newText: "other" }] }, roots);
  assert.match(seen.error ?? "", /^Found 2 occurrences of the text in .*dupe\.txt\. The text must be unique\./);
});

test("parity: edit refuses overlapping edits", { skip }, async () => {
  const roots = seedBoth(writing, "overlap.txt", "abcdef\n");
  const seen = await parity(
    editTool(),
    editProxy(),
    {
      path: "overlap.txt",
      edits: [
        { oldText: "abc", newText: "x" },
        { oldText: "bcd", newText: "y" },
      ],
    },
    roots,
  );
  assert.match(seen.error ?? "", /overlap in .*overlap\.txt/);
});

test("parity: edit refuses an empty oldText and a no-op replacement", { skip }, async () => {
  const roots = seedBoth(writing, "empty.txt", "abc\n");
  const empty = await parity(editTool(), editProxy(), { path: "empty.txt", edits: [{ oldText: "", newText: "x" }] }, roots);
  assert.match(empty.error ?? "", /must not be empty in /);
  const noop = await parity(editTool(), editProxy(), { path: "empty.txt", edits: [{ oldText: "abc", newText: "abc" }] }, roots);
  assert.match(noop.error ?? "", /^No changes made to .*empty\.txt\./);
});

test("parity: edit refuses a missing file", { skip }, async () => {
  const roots = { reference: join(writing, "ref"), candidate: join(writing, "go") };
  const seen = await parity(editTool(), editProxy(), { path: "gone.txt", edits: [{ oldText: "a", newText: "b" }] }, roots);
  assert.match(seen.error ?? "", /^Could not edit file: .*gone\.txt\. Error code: not_found\./);
});

test("parity: the four tools are the four the harness offers", { skip }, async () => {
  const names = [readProxy().name, writeProxy().name, editProxy().name, createBashTool(toolbox).name];
  assert.deepEqual(names, ["read", "write", "edit", "bash"]);
});

test("parity: read refuses an image that is too large, larger than the model, or unmeasurable", { skip }, async () => {
  // Three answers the reference gives instead of sending the picture. The size
  // limit is reached with trailing bytes, which both sides measure before they
  // ever look at the header.
  const huge = Buffer.concat([
    pngBytes([["IHDR", Buffer.from([0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0])]]),
    Buffer.alloc(5 * 1024 * 1024),
  ]);
  const ihdr = Buffer.from([0, 0, 0x0b, 0xb8, 0, 0, 0x0b, 0xb8, 8, 6, 0, 0, 0]); // 3000x3000
  const wide = pngBytes([["IHDR", ihdr]]);
  const cases: Array<[string, Buffer, string]> = [
    ["huge.png", huge, "huge.png is an image (image/png) of 5.0MB, too large to send, and no image processor is configured to shrink it"],
    ["wide.png", wide, "wide.png is an image (image/png) of 3000x3000, larger than 2000x2000, and no image processor is configured to shrink it"],
  ];
  for (const [name, bytes, message] of cases) {
    for (const side of ["ref", "go"]) writeFileSync(join(readings, side, name), bytes);
    const roots = { reference: join(readings, "ref"), candidate: join(readings, "go") };
    const seen = await parity(readTool(), readProxy(), { path: name }, roots);
    assert.equal(seen.isError, true, `${name} should be refused`);
    assert.deepEqual(seen.diagnostics, [{ severity: "error", code: "unsupported_image", message }], name);
  }

  // A file that only starts like a PNG is text on both sides: the signature is
  // eight bytes, and the header after it decides.
  const truncated = Buffer.from([0x89, 0x50, 0x4e, 0x47]);
  for (const side of ["ref", "go"]) writeFileSync(join(readings, side, "odd.png"), truncated);
  const roots = { reference: join(readings, "ref"), candidate: join(readings, "go") };
  const seen = await parity(readTool(), readProxy(), { path: "odd.png" }, roots);
  assert.equal(seen.isError, false);
  assert.deepEqual(seen.outputTypes, ["text"]);
});
