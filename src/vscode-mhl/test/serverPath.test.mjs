// Run with `npm test`, which first bundles src/serverPath.ts to out/test/.
import assert from "node:assert/strict";
import { test } from "node:test";
import { PATH_SERVER, bundledMatchesHost, resolveServerPath } from "../out/test/serverPath.mjs";

const BUNDLED = "/ext/bin/mhl";

test("resolveServerPath: an explicit setting wins over the bundled runtime", () => {
  assert.equal(resolveServerPath("/usr/local/bin/mhl", "/ws", true, BUNDLED), "/usr/local/bin/mhl");
  assert.equal(resolveServerPath("mhl", "/ws", true, BUNDLED), "mhl");
});

test("resolveServerPath: an empty setting uses the bundled runtime, else PATH", () => {
  assert.equal(resolveServerPath("", "/ws", true, BUNDLED), BUNDLED);
  assert.equal(resolveServerPath("  ", "/ws", false, BUNDLED), BUNDLED);
  assert.equal(resolveServerPath("", "/ws", true, undefined), PATH_SERVER);
});

test("resolveServerPath: ${workspaceFolder} expands only in a trusted workspace", () => {
  const setting = "${workspaceFolder}/src/mhl-runtime/dist/mhl";
  assert.equal(resolveServerPath(setting, "/ws", true, BUNDLED), "/ws/src/mhl-runtime/dist/mhl");
  assert.equal(resolveServerPath(setting, "/ws", false, BUNDLED), BUNDLED);
  assert.equal(resolveServerPath(setting, "/ws", false, undefined), PATH_SERVER);
  assert.equal(resolveServerPath(setting, undefined, true, BUNDLED), setting);
});

test("bundledMatchesHost", () => {
  assert.equal(bundledMatchesHost("darwin-arm64", "darwin", "arm64"), true);
  assert.equal(bundledMatchesHost("darwin-arm64", "darwin", "x64"), false);
  assert.equal(bundledMatchesHost("linux-x64", "linux", "x64"), true);
  assert.equal(bundledMatchesHost("alpine-x64", "linux", "x64"), true);
  assert.equal(bundledMatchesHost("linux-x64", "win32", "x64"), false);
  assert.equal(bundledMatchesHost("win32-arm64", "win32", "arm64"), true);
  assert.equal(bundledMatchesHost("undefined", "linux", "x64"), false);
});
