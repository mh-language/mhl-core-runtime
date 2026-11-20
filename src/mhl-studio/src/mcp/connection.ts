// The two ways the test bench talks to `mhl serve mcp` (src-tauri/src/mcp.rs):
// stdio, one JSON-RPC message per line, replies arriving on the start
// channel; and Streamable HTTP, one POST /mcp per message, with the
// session (`Mcp-Session-Id`) and protocol version headers a client sends.
import { api } from "../lib/api";
import { parseMessage, RpcCore, toReply, type RequestOptions, type RpcReply, type TrafficEntry } from "./rpc";

export type Transport = "stdio" | "http";

export interface Connection {
  readonly transport: Transport;
  request(method: string, params?: unknown, opts?: RequestOptions): Promise<RpcReply>;
  notify(method: string, params?: unknown): Promise<void>;
  /** Records the protocol version initialize agreed on (HTTP sends it as a header). */
  negotiated(version: string): void;
  /** HTTP: the session initialize minted. */
  readonly session: string | null;
  /** The server process is gone: fail what is still waiting. */
  closed(reason: string): void;
}

export class StdioConnection implements Connection {
  readonly transport = "stdio";
  readonly session = null;
  private core: RpcCore;

  constructor(log: (t: TrafficEntry) => void) {
    this.core = new RpcCore((text) => api.mcpSend(text), log);
  }

  request(method: string, params?: unknown, opts?: RequestOptions) {
    return this.core.request(method, params, opts);
  }
  notify(method: string, params?: unknown) {
    return this.core.notify(method, params);
  }
  negotiated() {}
  /** A stdout line of the server. */
  receive(text: string) {
    this.core.receive(text);
  }
  closed(reason: string) {
    this.core.close(reason);
  }
}

const JSON_HEADERS: [string, string][] = [
  ["Content-Type", "application/json"],
  ["Accept", "application/json, text/event-stream"],
];

export class HttpConnection implements Connection {
  readonly transport = "http";
  session: string | null = null;
  private version: string | null = null;
  private next = 1;
  private gone: string | null = null;

  constructor(private readonly log: (t: TrafficEntry) => void) {}

  negotiated(version: string) {
    this.version = version;
  }

  private headers(): [string, string][] {
    const h = [...JSON_HEADERS];
    if (this.session) h.push(["Mcp-Session-Id", this.session]);
    if (this.version) h.push(["MCP-Protocol-Version", this.version]);
    return h;
  }

  async request(method: string, params?: unknown, opts: RequestOptions = {}): Promise<RpcReply> {
    if (this.gone) throw new Error(this.gone);
    const id = this.next++;
    const text = JSON.stringify({ jsonrpc: "2.0", id, method, ...(params === undefined ? {} : { params }) });
    this.log({ at: Date.now(), dir: "out", text, method, rpcId: id, quiet: opts.quiet });
    const started = performance.now();
    const r = await api.mcpHttp("POST", "/mcp", this.headers(), text);
    const ms = Math.round(performance.now() - started);
    this.log({ at: Date.now(), dir: "in", text: r.body, method, rpcId: id, ms, status: r.status, quiet: opts.quiet });
    if (method === "initialize") this.session = header(r.headers, "mcp-session-id") ?? this.session;
    const msg = parseMessage(r.body);
    if (msg && (msg.result !== undefined || msg.error)) return toReply(msg, ms);
    if (r.status === 404 && this.session) throw new Error("the server no longer knows this session (HTTP 404) — reconnect");
    throw new Error(`HTTP ${r.status}${r.body ? `: ${r.body.trim()}` : ""}`);
  }

  async notify(method: string, params?: unknown): Promise<void> {
    if (this.gone) throw new Error(this.gone);
    const text = JSON.stringify({ jsonrpc: "2.0", method, ...(params === undefined ? {} : { params }) });
    this.log({ at: Date.now(), dir: "out", text, method });
    const r = await api.mcpHttp("POST", "/mcp", this.headers(), text);
    if (r.status !== 202 && r.status !== 200) throw new Error(`HTTP ${r.status}${r.body ? `: ${r.body.trim()}` : ""}`);
  }

  closed(reason: string) {
    this.gone = reason;
  }
}

function header(headers: [string, string][], name: string): string | undefined {
  return headers.find(([k]) => k === name)?.[1];
}
