// The real client classes against the vendored `mhl serve mcp` (skipped when
// `npm run sidecar` hasn't built it). The Tauri commands are stood in for
// by a child process (stdio) and Node's fetch (HTTP), so what is checked is
// the protocol: the handshake, session and version headers, run/*.
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { mkdtempSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";

const bin = (() => {
  const dir = join(__dirname, "../../src-tauri/binaries");
  try {
    const f = readdirSync(dir).find((n) => n.startsWith("mhl-"));
    return f ? join(dir, f) : null;
  } catch {
    return null;
  }
})();

const bridge: { http?: (method: string, path: string, headers: [string, string][], body: string) => Promise<unknown>; send?: (m: string) => Promise<void> } = {};
vi.mock("../lib/api", () => ({
  api: {
    mcpHttp: (m: string, p: string, h: [string, string][], b: string) => bridge.http!(m, p, h, b),
    mcpSend: (m: string) => bridge.send!(m),
  },
}));
const { HttpConnection, StdioConnection } = await import("./connection");
const { pausedCall } = await import("./model");

const PROGRAM = `workflow Greet {
  description: "Says hello"
  input name: string = "world"
  var greeting = ""
  step Hello {
    log("hi \${name}")
    greeting = "hello \${name}"
  }
}
workflow Gate {
  input approved: bool = false
  var ok = false
  step Ask {
    if (!approved) { pause("awaiting approval") }
    ok = true
  }
}
`;

function freePort(): Promise<number> {
  return new Promise((resolve) => {
    const s = createServer();
    s.listen(0, "127.0.0.1", () => {
      const port = (s.address() as { port: number }).port;
      s.close(() => resolve(port));
    });
  });
}

async function until<T>(fn: () => Promise<T | undefined>, ms = 10000): Promise<T> {
  const end = Date.now() + ms;
  for (;;) {
    const v = await fn().catch(() => undefined);
    if (v !== undefined) return v;
    if (Date.now() > end) throw new Error("timed out");
    await new Promise((r) => setTimeout(r, 100));
  }
}

describe.skipIf(!bin)("against the vendored mhl serve mcp", () => {
  let dir: string;
  const children: ChildProcessWithoutNullStreams[] = [];
  beforeAll(() => {
    dir = mkdtempSync(join(tmpdir(), "mhl-studio-mcp-"));
    writeFileSync(join(dir, "main.mh"), PROGRAM);
  });
  afterAll(() => {
    for (const c of children) c.kill();
    rmSync(dir, { recursive: true, force: true });
  });

  it("speaks stdio: initialize, tools/list, tools/call", async () => {
    const child = spawn(bin!, ["serve", "mcp", "."], { cwd: dir });
    children.push(child);
    const conn = new StdioConnection(() => undefined);
    bridge.send = async (m) => void child.stdin.write(m + "\n");
    createInterface({ input: child.stdout }).on("line", (l) => conn.receive(l));

    const init = await conn.request("initialize", { protocolVersion: "2025-11-25", capabilities: {} });
    expect(init).toMatchObject({ ok: true, result: { serverInfo: { name: "mhl" } } });
    await conn.notify("notifications/initialized");
    const list = await conn.request("tools/list");
    expect(list.ok && (list.result as { tools: { name: string }[] }).tools.map((t) => t.name).sort()).toEqual(["Gate", "Greet"]);
    const call = await conn.request("tools/call", { name: "Greet", arguments: { name: "bench" } });
    expect(call).toMatchObject({ ok: true, result: { isError: false, structuredContent: { greeting: "hello bench" } } });
    expect(call.ok && pausedCall(call.result)).toBeNull();

    // A workflow that stops at pause(): not an error, but marked paused.
    const gate = await conn.request("tools/call", { name: "Gate", arguments: {} });
    expect(gate).toMatchObject({ ok: true, result: { isError: false } });
    expect(gate.ok && pausedCall(gate.result)).toMatchObject({ reason: "awaiting approval", step: "Ask" });
  });

  it("speaks Streamable HTTP: session, run/start → paused → run/resume → completed", async () => {
    const port = await freePort();
    const child = spawn(bin!, ["serve", "mcp", "--http", "--addr", `127.0.0.1:${port}`, "."], { cwd: dir, env: { ...process.env, MHL_SERVE_TOKEN: "t" } });
    children.push(child);
    const sent: [string, string][][] = [];
    bridge.http = async (method, path, headers, body) => {
      sent.push(headers);
      const r = await fetch(`http://127.0.0.1:${port}${path}`, { method, headers: [...headers, ["Authorization", "Bearer t"]], body: method === "GET" ? undefined : body });
      return { status: r.status, headers: [...r.headers.entries()], body: await r.text() };
    };
    await until(async () => ((await fetch(`http://127.0.0.1:${port}/readyz`)).ok ? true : undefined));

    const conn = new HttpConnection(() => undefined);
    const init = await conn.request("initialize", { protocolVersion: "2025-11-25", capabilities: {} });
    expect(init.ok).toBe(true);
    expect(conn.session).toMatch(/^[0-9a-f]+$/);
    conn.negotiated("2025-11-25");
    await conn.notify("notifications/initialized");
    expect(sent[sent.length - 1]).toContainEqual(["Mcp-Session-Id", conn.session]);
    expect(sent[sent.length - 1]).toContainEqual(["MCP-Protocol-Version", "2025-11-25"]);

    const start = await conn.request("run/start", { name: "Gate", arguments: {} });
    expect(start.ok).toBe(true);
    const runId = (start as { result: { runId: string } }).result.runId;
    const status = async () => {
      const r = await conn.request("run/status", { runId });
      return r.ok ? (r.result as { state: string; reason?: string; vars?: Record<string, unknown> }) : undefined;
    };
    const paused = await until(async () => ((await status())?.state === "paused" ? status() : undefined));
    expect(paused.reason).toBe("awaiting approval");
    const resumed = await conn.request("run/resume", { runId, arguments: { approved: true } });
    expect(resumed.ok).toBe(true);
    const done = await until(async () => {
      const s = await status();
      return s?.state === "completed" ? s : undefined;
    });
    expect(done.vars).toMatchObject({ ok: true });

    const bad = await conn.request("run/status", { runId: "nope" });
    expect(bad).toMatchObject({ ok: false, error: { code: -32602 } });
  });
});
