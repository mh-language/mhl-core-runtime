// The MCP test bench's state and actions: start the workspace as an MCP
// server, connect as a client (initialize → initialized → tools/list,
// resources/list), and drive it — tools/call, resources/read, the HTTP-only
// run/* family (start, poll status + logs, resume a paused run, cancel), or
// any raw request. Every message is kept in the traffic log.
import { create } from "zustand";
import { api, errorText, type McpEvent } from "../lib/api";
import { useStore as useApp } from "../app/store";
import { HttpConnection, StdioConnection, type Connection, type Transport } from "./connection";
import { hasRunMethods, isActive, pausePoints, resumeFields, supportsAsync, type McpResource, type McpTool, type RunStatus, type ServerHello } from "./model";
import { rawParams, type RpcReply, type Traffic, type TrafficEntry } from "./rpc";
import { buildArguments, fieldsFor, pausesFor, schemaFields } from "./schema";

export type McpStatus = "stopped" | "starting" | "ready" | "failed";
/** The request editor's tabs, the response pane's tabs, and how a body is shown. */
export type ReqTab = "arguments" | "json" | "schema" | "docs";
export type ResTab = "body" | "content" | "message";
export type BodyMode = "pretty" | "raw" | "table";
export type ToolMethod = "tools/call" | "run/start";
export type Selection = { kind: "tool" | "resource" | "run"; id: string } | { kind: "raw" };

/** A request the panel made and is showing the outcome of. */
export interface Call {
  /** The JSON-RPC method sent. */
  method?: string;
  pending: boolean;
  started: number;
  reply?: RpcReply;
  /** A transport failure (no JSON-RPC reply at all). */
  error?: string;
}

export interface RunView {
  status: RunStatus;
  logs: string;
  since: number;
  /** The last run/* request on it failed. */
  error?: string;
}

export interface McpState {
  transport: Transport;
  status: McpStatus;
  error: string | null;
  /** HTTP: the endpoint and its bearer token, to point another client at it. */
  url: string | null;
  token: string | null;
  hello: ServerHello | null;
  session: string | null;
  tools: McpTool[];
  resources: McpResource[];
  /** Files were saved, created or moved since the server loaded them. */
  stale: boolean;
  traffic: Traffic[];
  serverLog: string[];
  /** Show status/log polls in the traffic log. */
  showQuiet: boolean;
  selected: Selection | null;
  /** Each tool's chosen method, by tool name (tools/call when unset). */
  methods: Record<string, ToolMethod>;
  reqTab: ReqTab;
  resTab: ResTab;
  bodyMode: BodyMode;
  /** The console (traffic + server log) drawer is open. */
  consoleOpen: boolean;
  /** The sidebar's filter. */
  filter: string;
  /** Each tool's form values, by tool name. */
  forms: Record<string, Record<string, string>>;
  /** Each tool's last call, by tool name. */
  calls: Record<string, Call>;
  /** Each resource's last read, by URI. */
  reads: Record<string, Call>;
  runs: Record<string, RunView>;
  /** The resume form's values, by run id. */
  resumeForms: Record<string, Record<string, string>>;
  raw: { method: string; params: string; call?: Call };
  bottom: "traffic" | "log";
}

/** What a (re)start resets; the rest is the user's layout and drafts. */
type Persistent = "transport" | "showQuiet" | "bottom" | "raw" | "methods" | "reqTab" | "resTab" | "bodyMode" | "consoleOpen" | "filter" | "forms" | "traffic";

const initial = (): Omit<McpState, Persistent> => ({
  status: "stopped",
  error: null,
  url: null,
  token: null,
  hello: null,
  session: null,
  tools: [],
  resources: [],
  stale: false,
  serverLog: [],
  selected: null,
  calls: {},
  reads: {},
  runs: {},
  resumeForms: {},
});

export const useMcp = create<McpState>(() => ({
  ...initial(),
  transport: "stdio",
  showQuiet: false,
  bottom: "traffic",
  raw: { method: "ping", params: "" },
  methods: {},
  reqTab: "arguments",
  resTab: "body",
  bodyMode: "pretty",
  consoleOpen: false,
  filter: "",
  forms: {},
  traffic: [],
}));

const set = useMcp.setState;
const get = useMcp.getState;

/** How many log lines / traffic entries are kept. */
const KEEP = 2000;
const POLL_MS = 700;

let conn: Connection | null = null;
/** Bumped on every start/stop, so a stale server's events and replies are dropped. */
let gen = 0;
let seq = 1;
const polling = new Set<string>();

function logTraffic(g: number) {
  return (t: TrafficEntry) => {
    if (g !== gen) return;
    set((s) => ({ traffic: [...s.traffic.slice(-(KEEP - 1)), { ...t, seq: seq++ }] }));
  };
}

function newToken(): string {
  const b = new Uint8Array(16);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}

/** Starts (or restarts) the server for the open folder and connects to it. */
export async function startServer(transport: Transport = get().transport): Promise<void> {
  const root = useApp.getState().root;
  if (!root) return;
  const g = ++gen;
  conn?.closed("the server was restarted");
  polling.clear();
  const token = transport === "http" ? newToken() : null;
  // A tool, resource or the raw request stays selected across a restart; a run belongs to the old server.
  set((s) => ({ ...initial(), transport, status: "starting", token, raw: { ...s.raw, call: undefined }, selected: s.selected?.kind === "run" ? null : s.selected }));
  const c: Connection = transport === "http" ? new HttpConnection(logTraffic(g)) : new StdioConnection(logTraffic(g));
  conn = c;
  const onEvent = (ev: McpEvent) => {
    if (g !== gen) return;
    if (ev.type === "message") (c as StdioConnection).receive(ev.text);
    else if (ev.type === "log") set((s) => ({ serverLog: [...s.serverLog.slice(-(KEEP - 1)), ev.text] }));
    else {
      c.closed("the server exited");
      const was = get().status;
      set({ status: was === "stopped" ? "stopped" : "failed", error: was === "stopped" ? null : "mhl serve mcp exited — see the server log", bottom: "log", consoleOpen: was !== "stopped" });
    }
  };
  try {
    const { url } = await api.mcpStart({ cwd: root, transport, token: token ?? undefined }, onEvent);
    if (g !== gen) return;
    set({ url });
    await connect(c, g);
  } catch (e) {
    if (g !== gen) return;
    set({ status: "failed", error: errorText(e), bottom: "log", consoleOpen: true });
  }
}

async function connect(c: Connection, g: number) {
  const init = await c.request("initialize", {
    protocolVersion: "2025-11-25",
    capabilities: {},
    clientInfo: { name: "mhl studio", version: useApp.getState().mhlVersion || "dev" },
  });
  if (g !== gen) return;
  if (!init.ok) throw new Error(`initialize: ${init.error.message}`);
  const hello = init.result as ServerHello;
  if (hello.protocolVersion) c.negotiated(hello.protocolVersion);
  await c.notify("notifications/initialized");
  set({ hello, session: c.session, status: "ready", error: null });
  await refreshLists();
}

/** tools/list and resources/list again (what the server serves doesn't change until it restarts). */
export async function refreshLists(): Promise<void> {
  const c = conn;
  const g = gen;
  if (!c) return;
  const tools = await c.request("tools/list");
  const resources = get().hello?.capabilities?.resources ? await c.request("resources/list") : null;
  if (g !== gen) return;
  set({
    tools: tools.ok ? ((tools.result as { tools?: McpTool[] }).tools ?? []) : [],
    resources: resources?.ok ? ((resources.result as { resources?: McpResource[] }).resources ?? []) : [],
  });
  const sel = get().selected;
  const gone = sel?.kind === "tool" ? !get().tools.some((t) => t.name === sel.id) : sel?.kind === "resource" ? !get().resources.some((r) => r.uri === sel.id) : !sel;
  if (gone) set({ selected: get().tools.length ? { kind: "tool", id: get().tools[0].name } : null });
}

export async function stopServer(): Promise<void> {
  gen++;
  conn?.closed("the server was stopped");
  conn = null;
  polling.clear();
  set({ status: "stopped", error: null, hello: null, session: null, url: null, token: null });
  await api.mcpStop().catch(() => undefined);
}

/** Runs `fn` against the live connection, recording a transport failure. */
async function withConn<T>(fn: (c: Connection) => Promise<T>): Promise<{ value?: T; error?: string; stale: boolean }> {
  const c = conn;
  const g = gen;
  if (!c || get().status !== "ready") return { error: "the server is not running", stale: false };
  try {
    const value = await fn(c);
    return { value, stale: g !== gen };
  } catch (e) {
    return { error: errorText(e), stale: g !== gen };
  }
}

function toolArgs(name: string): { args: Record<string, unknown>; errors: Record<string, string> } {
  const tool = get().tools.find((t) => t.name === name);
  return buildArguments(fieldsFor(schemaFields(tool?.inputSchema), toolMethod(get(), name)), get().forms[name] ?? {});
}

export function setFormValue(tool: string, field: string, value: string) {
  set((s) => ({ forms: { ...s.forms, [tool]: { ...s.forms[tool], [field]: value } } }));
}

/** tools/call: blocks until the workflow finishes. */
export async function callTool(name: string): Promise<void> {
  const { args, errors } = toolArgs(name);
  if (Object.keys(errors).length) return;
  set((s) => ({ calls: { ...s.calls, [name]: { method: "tools/call", pending: true, started: Date.now() } } }));
  const r = await withConn((c) => c.request("tools/call", { name, arguments: args }));
  if (r.stale) return;
  set((s) => ({ calls: { ...s.calls, [name]: { ...s.calls[name], pending: false, started: s.calls[name]?.started ?? Date.now(), reply: r.value, error: r.error } } }));
  // A control tool that returned a run status: follow that run like a run/start.
  const sc = r.value?.ok ? (r.value.result as { structuredContent?: Partial<RunStatus> }).structuredContent : undefined;
  if (name.startsWith("mhl_run_") && sc?.runId && sc.state) trackRun(sc as RunStatus);
}

/**
 * The method a tool's request goes out with: the one chosen for it, but
 * run/start only where the tool advertises it (`_meta.mhl.run`, HTTP only).
 * The choice is remembered across restarts, so a tool set to run/start on
 * HTTP must not send it over stdio, where run/* doesn't exist.
 */
export function toolMethod(s: Pick<McpState, "methods" | "tools">, name: string): ToolMethod {
  const tool = s.tools.find((t) => t.name === name);
  if (!tool || !supportsAsync(tool)) return "tools/call";
  // A workflow that may pause — a gate, or a stage asking for inputs — only
  // runs with run/start: a tools/call can't resume, so it would stop for good
  // at the first pause.
  if (canPause(tool)) return "run/start";
  return s.methods[name] ?? "tools/call";
}

/** The tool's run may suspend for a human: a step calls pause(...), or a stage asks for inputs. */
export function canPause(tool: McpTool): boolean {
  return !!pausePoints(tool) || schemaFields(tool.inputSchema).some(pausesFor);
}

/** Sends the tool's request with the method chosen for it. */
export function sendTool(name: string): Promise<void> {
  return toolMethod(get(), name) === "run/start" ? startRun(name) : callTool(name);
}

/**
 * Runs the tool where a pause can be approved: run/start, whose run keeps
 * its state and can be resumed (a tools/call's cannot). Over stdio there is
 * no run/*, so the server is restarted on HTTP first; the tool, its
 * arguments and the selection carry over.
 */
export async function runResumable(name: string): Promise<void> {
  set((s) => ({ methods: { ...s.methods, [name]: "run/start" } }));
  if (!hasRunMethods(get().hello)) {
    await startServer("http");
    if (get().status !== "ready" || !hasRunMethods(get().hello)) return;
  }
  await startRun(name);
}

/** run/start: returns at once with a runId, then polls it. */
export async function startRun(name: string): Promise<void> {
  const { args, errors } = toolArgs(name);
  if (Object.keys(errors).length) return;
  set((s) => ({ calls: { ...s.calls, [name]: { method: "run/start", pending: true, started: Date.now() } } }));
  const r = await withConn((c) => c.request("run/start", { name, arguments: args }));
  if (r.stale) return;
  set((s) => ({ calls: { ...s.calls, [name]: { ...s.calls[name], pending: false, started: s.calls[name]?.started ?? Date.now(), reply: r.value, error: r.error } } }));
  if (r.value?.ok) {
    const st = r.value.result as RunStatus;
    trackRun(st);
    set({ selected: { kind: "run", id: st.runId } });
  }
}

function trackRun(st: RunStatus) {
  set((s) => {
    const cur = s.runs[st.runId];
    return { runs: { ...s.runs, [st.runId]: { logs: cur?.logs ?? "", since: cur?.since ?? 0, status: st, error: undefined } } };
  });
  void poll(st.runId);
}

/** Follows a run's status and logs until it stops changing on its own. */
async function poll(id: string): Promise<void> {
  if (polling.has(id)) return;
  polling.add(id);
  const g = gen;
  try {
    for (;;) {
      await pullLogs(id);
      const r = await withConn((c) => c.request("run/status", { runId: id }, { quiet: true }));
      if (r.stale || g !== gen) return;
      if (r.error || !r.value?.ok) {
        updateRun(id, { error: r.error ?? (r.value && !r.value.ok ? r.value.error.message : "run/status failed") });
        return;
      }
      updateRun(id, { status: r.value.result as RunStatus, error: undefined });
      if (!isActive((r.value.result as RunStatus).state)) {
        await pullLogs(id);
        return;
      }
      await new Promise((res) => setTimeout(res, POLL_MS));
    }
  } finally {
    polling.delete(id);
  }
}

async function pullLogs(id: string) {
  const since = get().runs[id]?.since ?? 0;
  const r = await withConn((c) => c.request("run/logs", { runId: id, since }, { quiet: true }));
  if (r.stale || !r.value?.ok) return;
  const { text, nextSince } = r.value.result as { text?: string; nextSince?: number };
  const cur = get().runs[id];
  if (!cur) return;
  updateRun(id, { logs: cur.logs + (text ?? ""), since: nextSince ?? cur.since });
}

function updateRun(id: string, patch: Partial<RunView>) {
  set((s) => (s.runs[id] ? { runs: { ...s.runs, [id]: { ...s.runs[id], ...patch } } } : {}));
}

/** run/list: the runs this session owns (picks up any started through a control tool). */
export async function listRuns(): Promise<void> {
  const r = await withConn((c) => c.request("run/list"));
  if (r.stale || !r.value?.ok) return;
  for (const st of (r.value.result as { runs?: RunStatus[] }).runs ?? []) {
    if (get().runs[st.runId]) updateRun(st.runId, { status: st });
    else trackRun(st);
  }
}

export function setResumeValue(run: string, field: string, value: string) {
  set((s) => ({ resumeForms: { ...s.resumeForms, [run]: { ...s.resumeForms[run], [field]: value } } }));
}

/**
 * run/resume's params for a run: `{runId, decision}` answering a typed gate,
 * else `{runId, arguments}` from the resume form (only a waited-for stage's
 * inputs, when it stopped on entering one); null while the form has an error.
 */
export function resumeParams(status: RunStatus, tool: McpTool | undefined, form: Record<string, string>, decision?: unknown): Record<string, unknown> | null {
  if (decision !== undefined) return { runId: status.runId, decision };
  const { args, errors } = buildArguments(resumeFields(schemaFields(tool?.inputSchema), status), form);
  if (Object.keys(errors).length) return null;
  return { runId: status.runId, arguments: args };
}

/**
 * run/resume: continues a paused (or failed, resumable) run — with the resume
 * form's arguments merged in, or, for a run waiting at a typed gate, with
 * `decision` answering it (a decision the server refuses leaves it paused).
 */
export async function resumeRun(id: string, decision?: unknown): Promise<void> {
  const run = get().runs[id];
  if (!run) return;
  const tool = get().tools.find((t) => t.name === run.status.tool);
  const params = resumeParams(run.status, tool, get().resumeForms[id] ?? {}, decision);
  if (!params) return;
  const r = await withConn((c) => c.request("run/resume", params));
  if (r.stale) return;
  if (!r.value?.ok) {
    updateRun(id, { error: r.error ?? (r.value && !r.value.ok ? r.value.error.message : "run/resume failed") });
    return;
  }
  updateRun(id, { status: r.value.result as RunStatus, error: undefined });
  void poll(id);
}

export async function cancelRun(id: string): Promise<void> {
  const r = await withConn((c) => c.request("run/cancel", { runId: id }));
  if (r.stale) return;
  if (!r.value?.ok) updateRun(id, { error: r.error ?? (r.value && !r.value.ok ? r.value.error.message : "run/cancel failed") });
  else updateRun(id, { status: r.value.result as RunStatus });
  void poll(id);
}

export async function readResource(uri: string): Promise<void> {
  set((s) => ({ reads: { ...s.reads, [uri]: { method: "resources/read", pending: true, started: Date.now() } } }));
  const r = await withConn((c) => c.request("resources/read", { uri }));
  if (r.stale) return;
  set((s) => ({ reads: { ...s.reads, [uri]: { ...s.reads[uri], pending: false, started: s.reads[uri]?.started ?? Date.now(), reply: r.value, error: r.error } } }));
}

export async function sendRaw(): Promise<void> {
  const { method, params } = get().raw;
  const p = rawParams(params);
  if (p.error || !method.trim()) return;
  set((s) => ({ raw: { ...s.raw, call: { method: method.trim(), pending: true, started: Date.now() } } }));
  const r = await withConn((c) => c.request(method.trim(), p.params));
  if (r.stale) return;
  set((s) => ({ raw: { ...s.raw, call: { ...s.raw.call, pending: false, started: s.raw.call?.started ?? Date.now(), reply: r.value, error: r.error } } }));
}

export function clearTraffic() {
  set({ traffic: [], serverLog: [] });
}

// Keep the bench in step with the workspace: a new folder stops its
// server; saving, creating or moving a file marks the running one stale
// (it loaded the files when it started); opening a file leaves the bench.
useApp.subscribe((s, prev) => {
  if (s.root !== prev.root) {
    if (get().status !== "stopped") void stopServer();
    set({ ...initial(), raw: { method: "ping", params: "" }, forms: {}, traffic: [] });
    return;
  }
  if (s.mode === "mcp" && s.activePath !== prev.activePath && prev.activePath !== null) useApp.setState({ mode: "editor" });
  const running = get().status === "ready" || get().status === "starting";
  if (!running || get().stale) return;
  const saved = Object.keys(s.dirty).some((p) => prev.dirty[p] && !s.dirty[p]);
  if (saved || s.files !== prev.files) set({ stale: true });
});
