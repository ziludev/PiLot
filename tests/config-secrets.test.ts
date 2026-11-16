import assert from "node:assert/strict";
import { test } from "node:test";
import { dropSecrets } from "../src/config.ts";

/**
 * `config.ts` captures the secrets into `config` and then deletes them from the environment,
 * because every child process (bash, MCP servers, the aliyun CLI) inherits it. This pins the
 * dropping itself; that the module calls it is proven by the config import running at all.
 */

const env = (): Record<string, string | undefined> => ({
  PATH: "/usr/bin",
  FEISHU_APP_SECRET: "s3cret",
  MAGPIE_API_KEY: "magpie",
  AGENT_HTTP_TOKEN: "",
  AGENT_MODEL: "vendor/model-pro",
});

test("only the named variables are dropped", () => {
  const source = env();
  const dropped = dropSecrets(source, ["FEISHU_APP_SECRET", "MAGPIE_API_KEY"]);
  assert.deepEqual(dropped, ["FEISHU_APP_SECRET", "MAGPIE_API_KEY"]);
  assert.equal(source["FEISHU_APP_SECRET"], undefined);
  assert.equal(source["MAGPIE_API_KEY"], undefined);
  assert.equal(source["AGENT_MODEL"], "vendor/model-pro", "unrelated keys stay");
  assert.equal(source["PATH"], "/usr/bin");
});

test("an empty string is still a value, so it is dropped too", () => {
  const source = env();
  assert.deepEqual(dropSecrets(source, ["AGENT_HTTP_TOKEN"]), ["AGENT_HTTP_TOKEN"]);
});

test("a variable that was never set is not reported as dropped", () => {
  const source = env();
  assert.deepEqual(dropSecrets(source, ["ANTHROPIC_API_KEY"]), []);
  assert.equal(source["ANTHROPIC_API_KEY"], undefined);
});
