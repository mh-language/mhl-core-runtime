import { describe, expect, it } from "vitest";
import { rawParams, RpcCore, type TrafficEntry } from "./rpc";

function core() {
  const sent: string[] = [];
  const log: TrafficEntry[] = [];
  let t = 0;
  const c = new RpcCore(
    async (text) => {
      sent.push(text);
    },
    (e) => log.push(e),
    () => (t += 5),
  );
  return { c, sent, log };
}

describe("RpcCore", () => {
  it("numbers requests and resolves replies by id, in any order", async () => {
    const { c, sent } = core();
    const a = c.request("tools/list");
    const b = c.request("tools/call", { name: "Greet" });
    expect(sent.map((s) => JSON.parse(s))).toEqual([
      { jsonrpc: "2.0", id: 1, method: "tools/list" },
      { jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "Greet" } },
    ]);
    c.receive('{"jsonrpc":"2.0","id":2,"result":{"content":[]}}');
    c.receive('{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}');
    expect(await b).toMatchObject({ ok: true, result: { content: [] } });
    expect(await a).toMatchObject({ ok: true, result: { tools: [] } });
    expect(c.waiting).toBe(0);
  });

  it("turns a JSON-RPC error into a reply, not an exception", async () => {
    const { c } = core();
    const p = c.request("nope");
    c.receive('{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found: nope"}}');
    expect(await p).toMatchObject({ ok: false, error: { code: -32601 } });
  });

  it("logs what is sent and received, with the request's method and timing on the reply", async () => {
    const { c, log } = core();
    const p = c.request("run/status", { runId: "x" }, { quiet: true });
    c.receive('{"jsonrpc":"2.0","id":1,"result":{}}');
    await p;
    expect(log.map((l) => [l.dir, l.method, l.rpcId, l.quiet])).toEqual([
      ["out", "run/status", 1, true],
      ["in", "run/status", 1, true],
    ]);
    expect(log[1].ms).toBe(5);
  });

  it("only logs what answers nothing pending (a notification, an unknown id, garbage)", () => {
    const { c, log } = core();
    c.receive('{"jsonrpc":"2.0","method":"notifications/message","params":{}}');
    c.receive('{"jsonrpc":"2.0","id":99,"result":{}}');
    c.receive("not json");
    expect(log.map((l) => l.method)).toEqual(["notifications/message", undefined, undefined]);
  });

  it("sends a notification without an id", async () => {
    const { c, sent } = core();
    await c.notify("notifications/initialized");
    expect(JSON.parse(sent[0])).toEqual({ jsonrpc: "2.0", method: "notifications/initialized" });
  });

  it("fails every pending request when the transport closes", async () => {
    const { c } = core();
    const p = c.request("tools/call");
    c.close("the server exited");
    await expect(p).rejects.toThrow("the server exited");
    expect(c.waiting).toBe(0);
  });

  it("fails a request whose write fails", async () => {
    const c = new RpcCore(
      () => Promise.reject(new Error("broken pipe")),
      () => undefined,
    );
    await expect(c.request("ping")).rejects.toThrow("broken pipe");
    expect(c.waiting).toBe(0);
  });
});

describe("rawParams", () => {
  it("accepts blank, an object or an array, and nothing else", () => {
    expect(rawParams("  ")).toEqual({});
    expect(rawParams('{"a":1}')).toEqual({ params: { a: 1 } });
    expect(rawParams("[1]")).toEqual({ params: [1] });
    expect(rawParams("3").error).toMatch(/object or array/);
    expect(rawParams("null").error).toMatch(/object or array/);
    expect(rawParams("{").error).toBeTruthy();
  });
});
