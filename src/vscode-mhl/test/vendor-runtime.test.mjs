import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { goTarget, hostTarget, pinnedRuntime } from "../scripts/vendor-runtime.mjs";

test("goTarget maps every target publish-extension.yml builds", () => {
  const workflow = readFileSync(new URL("../../../.github/workflows/publish-extension.yml", import.meta.url), "utf8");
  const targets = [...workflow.matchAll(/^\s+- target: (\S+)$/gm)].map((m) => m[1]).filter((t) => t !== "universal");
  assert.ok(targets.length > 0, "no targets found in the workflow matrix");
  for (const target of targets) {
    assert.doesNotThrow(() => goTarget(target), target);
  }
});

test("goTarget", () => {
  assert.deepEqual(goTarget("darwin-arm64"), { GOOS: "darwin", GOARCH: "arm64", ext: "" });
  assert.deepEqual(goTarget("win32-x64"), { GOOS: "windows", GOARCH: "amd64", ext: ".exe" });
  assert.deepEqual(goTarget("alpine-arm64"), { GOOS: "linux", GOARCH: "arm64", ext: "" });
  assert.throws(() => goTarget("linux-armhf"), /unsupported/);
  assert.throws(() => goTarget("web"), /unsupported/);
});

test("hostTarget", () => {
  assert.equal(hostTarget("darwin", "arm64"), "darwin-arm64");
});

test("pinnedRuntime", () => {
  assert.equal(pinnedRuntime({ mhlRuntime: "v1.11.1" }), "v1.11.1");
  assert.equal(pinnedRuntime({ mhlRuntime: "v2.0.0-beta.1" }), "v2.0.0-beta.1");
  assert.throws(() => pinnedRuntime({}), /mhlRuntime/);
  assert.throws(() => pinnedRuntime({ mhlRuntime: "1.11.1" }), /mhlRuntime/);
  const pkg = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));
  assert.doesNotThrow(() => pinnedRuntime(pkg));
});
