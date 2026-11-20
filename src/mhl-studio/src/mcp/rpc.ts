// The transport-independent half of the MCP client: request ids, matching
// replies to requests, and the traffic log. A transport (connection.ts)
// supplies `write` and feeds whatever the server sends to `receive`.

export interface RpcError {
  code: number;
  message: string;
  data?: unknown;
}

/** A request's outcome. A JSON-RPC error is a reply, not an exception: only a transport failure throws. */
export type RpcReply = { ok: true; result: unknown; ms: number } | { ok: false; error: RpcError; ms: number };

/** One line of the traffic log (`seq` is assigned by the log). */
export interface Traffic {
  seq: number;
  at: number;
  dir: "out" | "in";
  /** The JSON-RPC message, as sent or received. */
  text: string;
  method?: string;
  rpcId?: number | string;
  /** On a reply: how long its request took. */
  ms?: number;
  /** HTTP status, when the transport has one. */
  status?: number;
  /** A status/log poll: hidden from the log unless asked for. */
  quiet?: boolean;
}

/** A traffic line before the log numbers it. */
export type TrafficEntry = Omit<Traffic, "seq">;

export interface RequestOptions {
  quiet?: boolean;
}

interface Pending {
  method: string;
  started: number;
  quiet: boolean;
  resolve: (r: RpcReply) => void;
  reject: (e: Error) => void;
}

/** What `receive` makes of one incoming message. */
export interface Parsed {
  id?: number | string;
  method?: string;
  result?: unknown;
  error?: RpcError;
}

export function parseMessage(text: string): Parsed | null {
  try {
    const v = JSON.parse(text);
    return v && typeof v === "object" && !Array.isArray(v) ? (v as Parsed) : null;
  } catch {
    return null;
  }
}

export function toReply(msg: Parsed, ms: number): RpcReply {
  if (msg.error) return { ok: false, error: msg.error, ms };
  return { ok: true, result: msg.result, ms };
}

/**
 * Message framing for a stream transport (stdio): `request` writes and
 * waits for the reply with the same id; `receive` resolves it. Replies may
 * arrive in any order.
 */
export class RpcCore {
  private next = 1;
  private pending = new Map<number, Pending>();

  constructor(
    private readonly write: (text: string) => Promise<void>,
    private readonly log: (t: TrafficEntry) => void,
    private readonly now: () => number = () => performance.now(),
  ) {}

  nextId(): number {
    return this.next++;
  }

  request(method: string, params?: unknown, opts: RequestOptions = {}): Promise<RpcReply> {
    const id = this.nextId();
    const text = JSON.stringify({ jsonrpc: "2.0", id, method, ...(params === undefined ? {} : { params }) });
    return new Promise<RpcReply>((resolve, reject) => {
      this.pending.set(id, { method, started: this.now(), quiet: !!opts.quiet, resolve, reject });
      this.log({ at: Date.now(), dir: "out", text, method, rpcId: id, quiet: opts.quiet });
      this.write(text).catch((e: unknown) => {
        this.pending.delete(id);
        reject(e instanceof Error ? e : new Error(String(e)));
      });
    });
  }

  async notify(method: string, params?: unknown): Promise<void> {
    const text = JSON.stringify({ jsonrpc: "2.0", method, ...(params === undefined ? {} : { params }) });
    this.log({ at: Date.now(), dir: "out", text, method });
    await this.write(text);
  }

  /** One message from the server. Anything that is not a reply to a pending request is only logged. */
  receive(text: string): void {
    const msg = parseMessage(text);
    const id = typeof msg?.id === "number" ? msg.id : undefined;
    const p = id !== undefined && (msg?.result !== undefined || msg?.error) ? this.pending.get(id) : undefined;
    const ms = p ? Math.round(this.now() - p.started) : undefined;
    this.log({ at: Date.now(), dir: "in", text, method: p?.method ?? msg?.method, rpcId: msg?.id, ms, quiet: p?.quiet });
    if (!p || id === undefined || !msg) return;
    this.pending.delete(id);
    p.resolve(toReply(msg, ms ?? 0));
  }

  /** The transport is gone: every request still waiting fails. */
  close(reason: string): void {
    for (const p of this.pending.values()) p.reject(new Error(reason));
    this.pending.clear();
  }

  get waiting(): number {
    return this.pending.size;
  }
}

/** Typed-in params: absent when blank, else a JSON object or array (JSON-RPC allows nothing else). */
export function rawParams(text: string): { params?: unknown; error?: string } {
  if (!text.trim()) return {};
  try {
    const v: unknown = JSON.parse(text);
    if (v === null || typeof v !== "object") return { error: "params must be a JSON object or array" };
    return { params: v };
  } catch (e) {
    return { error: e instanceof Error ? e.message : String(e) };
  }
}
