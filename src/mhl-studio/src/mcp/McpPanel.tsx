// The MCP test bench, laid out like an API client: what the server serves
// on the left (a filterable tree of tools, resources, runs), the selected
// request in the middle — method, address, Send, its arguments — the
// response under it (status, time, size; body as highlighted JSON, raw text
// or a table), and a console drawer with every JSON-RPC message exchanged
// and the server's own log.
import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronDown, ChevronRight, Copy, Loader, Pause, Play, RotateCw, Search, Send, Square, Terminal } from "lucide-react";
import { ICON } from "../lib/icons";
import { useStore as useApp } from "../app/store";
import {
  cancelRun,
  clearTraffic,
  listRuns,
  readResource,
  refreshLists,
  resumeRun,
  runResumable,
  sendRaw,
  sendTool,
  toolMethod,
  setFormValue,
  setResumeValue,
  startServer,
  stopServer,
  useMcp,
  type BodyMode,
  type Call,
  type ReqTab,
  type ResTab,
  type ToolMethod,
} from "./store";
import { hasRunMethods, isActive, isControlTool, pausedCall, runProgress, supportsAsync, type CallToolResult, type ContentBlock, type McpTool, type RunState } from "./model";
import { buildArguments, placeholder, schemaFields, valuesFor, type SchemaField } from "./schema";
import { rawParams, type Traffic } from "./rpc";
import { asTable, byteSize, cellText, decodeEmbedded, formatBytes, parseEmbedded } from "./json";
import { JsonView } from "./JsonView";

const set = useMcp.setState;

function clock(at: number) {
  const d = new Date(at);
  return d.toLocaleTimeString(undefined, { hour12: false }) + "." + String(d.getMilliseconds()).padStart(3, "0");
}

function copy(text: string) {
  void navigator.clipboard?.writeText(text);
}

/** Where requests go, shown in the address bar. */
function useBase(): string {
  const url = useMcp((s) => s.url);
  const transport = useMcp((s) => s.transport);
  return url ?? (transport === "stdio" ? "stdio://mhl serve mcp" : "http://127.0.0.1");
}

// ------------------------------------------------------------------ top bar

function TopBar() {
  const s = useMcp();
  const running = s.status === "ready" || s.status === "starting";
  const info = s.hello?.serverInfo;
  const crumbs = useCrumbs();
  return (
    <div className="mcp-top">
      <div className="mcp-crumbs">
        <span className="muted">mhl serve mcp</span>
        {crumbs.map((c, i) => (
          <span key={i} className={i === crumbs.length - 1 ? "mcp-crumb-last" : "muted"}>
            <span className="mcp-crumb-sep">›</span>
            {c}
          </span>
        ))}
      </div>
      <span className="spacer" />
      {s.status === "ready" && s.url && (
        <span className="mcp-copy-group">
          <button className="btn small ghost" title={`Copy ${s.url}`} onClick={() => copy(s.url!)}>
            <Copy {...ICON} size={13} /> URL
          </button>
          <button className="btn small ghost" title="Copy the bearer token" onClick={() => copy(s.token ?? "")}>
            Token
          </button>
          <button
            className="btn small ghost"
            title="Copy a curl command that lists the tools"
            onClick={() =>
              copy(
                `curl -s -H 'Authorization: Bearer ${s.token}' -H 'Content-Type: application/json' ${s.url} -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}'`,
              )
            }
          >
            curl
          </button>
        </span>
      )}
      <div className={`mcp-server ${s.status}`}>
        <span className="mcp-dot" />
        <div className="segmented" role="group" aria-label="Transport">
          {(["stdio", "http"] as const).map((t) => (
            <button
              key={t}
              className={s.transport === t ? "active" : ""}
              aria-pressed={s.transport === t}
              title={t === "stdio" ? "mhl serve mcp — JSON-RPC lines on stdin/stdout" : "mhl serve mcp --http — Streamable HTTP on a loopback port, with async runs"}
              onClick={() => (running ? void startServer(t) : set({ transport: t }))}
            >
              {t === "stdio" ? "stdio" : "HTTP"}
            </button>
          ))}
        </div>
        <span
          className="mcp-server-text"
          title={s.status === "ready" ? `${info?.name} ${info?.version} · protocol ${s.hello?.protocolVersion}${s.session ? ` · session ${s.session}` : ""}` : (s.error ?? undefined)}
        >
          {s.status === "ready" && `${info?.name} ${info?.version}`}
          {s.status === "starting" && "Starting…"}
          {s.status === "stopped" && "Stopped"}
          {s.status === "failed" && "Failed"}
        </span>
        {running ? (
          <>
            <button className="icon-btn" disabled={s.status === "starting"} title="Restart (the server loads the saved files when it starts)" onClick={() => void startServer()}>
              <RotateCw {...ICON} size={14} />
            </button>
            <button className="icon-btn" title="Stop" onClick={() => void stopServer()}>
              <Square {...ICON} size={14} />
            </button>
          </>
        ) : (
          <button className="icon-btn" title="Start the server" onClick={() => void startServer()}>
            <Play {...ICON} size={14} />
          </button>
        )}
      </div>
    </div>
  );
}

function useCrumbs(): string[] {
  const sel = useMcp((s) => s.selected);
  const tool = useMcp((s) => (sel?.kind === "run" ? s.runs[sel.id]?.status.tool : undefined));
  if (!sel) return [];
  if (sel.kind === "raw") return ["Raw request"];
  if (sel.kind === "tool") return [isControlTool(sel.id) ? "Run control" : "Tools", sel.id];
  if (sel.kind === "resource") return ["Resources", sel.id];
  return ["Runs", `${tool ?? "run"} ${sel.id.slice(0, 8)}`];
}

function Banners() {
  const stale = useMcp((s) => s.stale);
  const status = useMcp((s) => s.status);
  const error = useMcp((s) => s.error);
  const unsaved = useApp((s) => Object.values(s.dirty).filter(Boolean).length);
  return (
    <>
      {status === "failed" && error && (
        <div className="mcp-banner bad">
          {error}{" "}
          <button className="link" onClick={() => set({ consoleOpen: true, bottom: "log" })}>
            Server log
          </button>
        </div>
      )}
      {status === "ready" && stale && (
        <div className="mcp-banner warn">
          Files changed since the server started — it still serves what it loaded then.{" "}
          <button className="link" onClick={() => void startServer()}>
            Restart
          </button>
        </div>
      )}
      {unsaved > 0 && (
        <div className="mcp-banner">
          {unsaved} unsaved file{unsaved > 1 ? "s" : ""}: the server only reads what is saved.
        </div>
      )}
    </>
  );
}

// ------------------------------------------------------------------ sidebar

function Section({ title, count, children }: { title: string; count?: number; children: React.ReactNode }) {
  const [open, setOpen] = useState(true);
  return (
    <div className="mcp-section">
      <button className="mcp-section-head" onClick={() => setOpen(!open)}>
        {open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        {title}
        {count !== undefined && <span className="mcp-count">{count}</span>}
      </button>
      {open && children}
    </div>
  );
}

const BADGE: Record<string, string> = { tool: "CALL", control: "CTRL", resource: "READ", raw: "RPC" };
const STATE_BADGE: Partial<Record<RunState, string>> = { pending: "QUEUE", claimed: "QUEUE", queued: "QUEUE", working: "RUN", paused: "WAIT", completed: "DONE", failed: "FAIL", canceled: "STOP" };

function Sidebar() {
  const s = useMcp();
  const f = s.filter.trim().toLowerCase();
  const match = (x: string) => !f || x.toLowerCase().includes(f);
  const workflows = s.tools.filter((t) => !isControlTool(t.name) && match(t.name));
  const control = s.tools.filter((t) => isControlTool(t.name) && match(t.name));
  const resources = s.resources.filter((r) => match(r.uri));
  const runs = Object.values(s.runs)
    .reverse()
    .filter((r) => match(r.status.tool) || match(r.status.runId));
  const sel = s.selected;
  const isSel = (kind: string, id?: string) => !!sel && sel.kind === kind && (sel.kind === "raw" || ("id" in sel && sel.id === id));
  const row = (kind: "tool" | "resource" | "run", id: string, badge: React.ReactNode, label: React.ReactNode, title?: string) => (
    <button key={id} className={`mcp-row ${isSel(kind, id) ? "active" : ""}`} title={title} onClick={() => set({ selected: { kind, id } })}>
      {badge}
      <span className="mcp-row-label">{label}</span>
    </button>
  );
  const ready = s.status === "ready";
  return (
    <aside className="mcp-side">
      <div className="mcp-filter">
        <Search {...ICON} size={13} />
        <input placeholder="Filter" value={s.filter} onChange={(e) => set({ filter: e.target.value })} />
      </div>
      <div className="mcp-tree">
        {!ready && <p className="muted small pad">Start the server to see what it serves.</p>}
        {ready && (
          <>
            <Section title="Tools" count={workflows.length}>
              {workflows.map((t) => {
                const last = s.calls[t.name];
                const paused = last?.reply?.ok && pausedCall(last.reply.result);
                return row(
                  "tool",
                  t.name,
                  <span className="mcp-method m-call">{BADGE.tool}</span>,
                  <>
                    {t.name}
                    {paused && <Pause className="mcp-row-paused" size={12} aria-label="last call paused" />}
                  </>,
                  paused ? `Last call paused: ${paused.reason}` : t.description,
                );
              })}
              {workflows.length === 0 && <p className="muted small mcp-none">{f ? "No match." : "No workflow or pipeline in this folder."}</p>}
            </Section>
            {control.length > 0 && (
              <Section title="Run control" count={control.length}>
                {control.map((t) => row("tool", t.name, <span className="mcp-method m-ctrl">{BADGE.control}</span>, t.name, t.description))}
              </Section>
            )}
            <Section title="Resources" count={resources.length}>
              {resources.map((r) => row("resource", r.uri, <span className="mcp-method m-read">{BADGE.resource}</span>, r.uri.replace(/^mhl:\/\/workflow\//, ""), r.uri))}
            </Section>
            {hasRunMethods(s.hello) && (
              <Section title="Runs" count={runs.length}>
                <button className="mcp-row mcp-row-action" onClick={() => void listRuns()} title="run/list: the runs this session owns">
                  <RotateCw {...ICON} size={12} /> Refresh (run/list)
                </button>
                {runs.map((r) =>
                  row(
                    "run",
                    r.status.runId,
                    <span className={`mcp-method m-state ${r.status.state}`} title={r.status.state}>{STATE_BADGE[r.status.state] ?? r.status.state.toUpperCase()}</span>,
                    <>
                      {r.status.tool} <span className="muted mono">{r.status.runId.slice(0, 6)}</span>
                    </>,
                  ),
                )}
              </Section>
            )}
            <Section title="Protocol">
              <button className={`mcp-row ${isSel("raw") ? "active" : ""}`} onClick={() => set({ selected: { kind: "raw" } })}>
                <span className="mcp-method m-rpc">{BADGE.raw}</span>
                <span className="mcp-row-label">Raw request</span>
              </button>
            </Section>
          </>
        )}
      </div>
    </aside>
  );
}

// ------------------------------------------------------------------ request

function Elapsed({ since }: { since: number }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 100);
    return () => clearInterval(t);
  }, []);
  return <>{((now - since) / 1000).toFixed(1)} s</>;
}

function UrlBar({ method, path, onSend, pending, disabled, sendLabel = "Send" }: { method: React.ReactNode; path: React.ReactNode; onSend: () => void; pending?: boolean; disabled?: boolean; sendLabel?: string }) {
  const base = useBase();
  return (
    <div className="mcp-urlbar">
      <div className="mcp-url">
        <div className="mcp-url-method">{method}</div>
        <div className="mcp-url-path mono">
          <span className="mcp-url-base">{base}</span>
          {path}
        </div>
      </div>
      <button className="btn mcp-send" disabled={disabled || pending} onClick={onSend}>
        {pending ? <Loader {...ICON} size={14} className="spin" /> : <Send {...ICON} size={14} />} {sendLabel}
      </button>
    </div>
  );
}

function Tabs<T extends string>({ tabs, value, onChange }: { tabs: [T, React.ReactNode, boolean?][]; value: T; onChange: (t: T) => void }) {
  return (
    <div className="mcp-tabs-line">
      {tabs.map(([t, label, dot]) => (
        <button key={t} className={`mcp-tab ${value === t ? "active" : ""}`} onClick={() => onChange(t)}>
          {label}
          {dot && <span className="mcp-tab-dot" />}
        </button>
      ))}
    </div>
  );
}

function Fields({ fields, values, errors, onChange, disabled }: { fields: SchemaField[]; values: Record<string, string>; errors: Record<string, string>; onChange: (name: string, v: string) => void; disabled?: boolean }) {
  if (fields.length === 0) return <p className="muted small mcp-pad">This tool takes no arguments.</p>;
  return (
    <table className="mcp-kv">
      <thead>
        <tr>
          <th>Key</th>
          <th>Value</th>
          <th>Type</th>
          <th>Description</th>
        </tr>
      </thead>
      <tbody>
        {fields.map((f) => {
          const v = values[f.name] ?? "";
          const err = v !== "" ? errors[f.name] : undefined;
          return (
            <tr key={f.name} className={err ? "invalid" : ""}>
              <td className="mono">
                {f.name}
                {f.required && <span className="mcp-req-mark" title="required">*</span>}
              </td>
              <td>
                {f.kind === "boolean" || f.kind === "enum" ? (
                  <select className="mono" disabled={disabled} value={v} onChange={(e) => onChange(f.name, e.target.value)}>
                    <option value="">{placeholder(f) || "—"}</option>
                    {(f.kind === "boolean" ? ["true", "false"] : (f.variants ?? [])).map((x) => (
                      <option key={x} value={x}>
                        {x}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input className="mono" disabled={disabled} placeholder={placeholder(f)} value={v} onChange={(e) => onChange(f.name, e.target.value)} />
                )}
                {err && <div className="mcp-err small">{err}</div>}
              </td>
              <td className="mono muted">{f.type}</td>
              <td className="muted small">{f.description ?? ""}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

/** The arguments as editable JSON, kept in step with the form. */
function ArgsJson({ tool, fields }: { tool: string; fields: SchemaField[] }) {
  const values = useMcp((s) => s.forms[tool]) ?? {};
  const [text, setText] = useState(() => JSON.stringify(buildArguments(fields, values).args, null, 2));
  const [error, setError] = useState<string | null>(null);
  const onChange = (t: string) => {
    setText(t);
    const p = rawParams(t);
    if (p.error || Array.isArray(p.params)) return setError(p.error ?? "arguments must be a JSON object");
    const obj = (p.params ?? {}) as Record<string, unknown>;
    const unknown = Object.keys(obj).filter((k) => !fields.some((f) => f.name === k));
    setError(unknown.length ? `not in the inputSchema: ${unknown.join(", ")}` : null);
    set((s) => ({ forms: { ...s.forms, [tool]: valuesFor(fields, obj) } }));
  };
  return (
    <div className="mcp-pad">
      <textarea className={`mcp-code ${error ? "invalid" : ""}`} spellCheck={false} rows={8} value={text} onChange={(e) => onChange(e.target.value)} />
      {error && <p className="mcp-err small">{error}</p>}
    </div>
  );
}

function ToolView({ tool }: { tool: McpTool }) {
  const values = useMcp((s) => s.forms[tool.name]) ?? {};
  const call = useMcp((s) => s.calls[tool.name]);
  const m: ToolMethod = useMcp((s) => toolMethod(s, tool.name));
  const reqTab = useMcp((s) => s.reqTab);
  const fields = useMemo(() => schemaFields(tool.inputSchema), [tool.inputSchema]);
  const { args, errors } = buildArguments(fields, values);
  const blocked = Object.keys(errors).length > 0;
  const async = supportsAsync(tool);
  return (
    <div className="mcp-split">
      <div className="mcp-req">
        <div className="mcp-title">
          <h2>{tool.name}</h2>
          {async && <span className="mcp-pill" title="_meta.mhl.run: run/start can run it asynchronously">async</span>}
        </div>
        <UrlBar
          method={
            async ? (
              <select className={`mcp-method-select ${m === "run/start" ? "m-ctrl" : "m-call"}`} value={m} onChange={(e) => set((s) => ({ methods: { ...s.methods, [tool.name]: e.target.value as ToolMethod } }))}>
                <option value="tools/call">tools/call</option>
                <option value="run/start">run/start</option>
              </select>
            ) : (
              <span className="mcp-method-fixed m-call">tools/call</span>
            )
          }
          path={
            <>
              /tools/<b>{tool.name}</b>
            </>
          }
          onSend={() => void sendTool(tool.name)}
          pending={call?.pending}
          disabled={blocked}
        />
        <Tabs<ReqTab>
          tabs={[
            ["arguments", <>Arguments {fields.length > 0 && <span className="muted">{Object.keys(args).length}/{fields.length}</span>}</>, Object.keys(args).length > 0],
            ["json", "JSON"],
            ["schema", "Schema"],
            ["docs", "Docs"],
          ]}
          value={reqTab}
          onChange={(t) => set({ reqTab: t })}
        />
        <div className="mcp-req-body">
          {reqTab === "arguments" && (
            <>
              <Fields fields={fields} values={values} errors={errors} onChange={(n, v) => setFormValue(tool.name, n, v)} disabled={call?.pending} />
              {blocked && <p className="mcp-err small mcp-pad">Fill in: {Object.keys(errors).join(", ")}</p>}
            </>
          )}
          {reqTab === "json" && <ArgsJson key={tool.name} tool={tool.name} fields={fields} />}
          {reqTab === "schema" && (
            <div className="mcp-pad">
              <JsonView value={{ inputSchema: tool.inputSchema, ...(tool.outputSchema ? { outputSchema: tool.outputSchema } : {}), ...(tool._meta ? { _meta: tool._meta } : {}) }} />
            </div>
          )}
          {reqTab === "docs" && (
            <div className="mcp-pad mcp-docs">
              <p>{tool.description || <span className="muted">No description. Add one with `description: "…"` in the workflow.</span>}</p>
              <p className="muted small">
                <b>tools/call</b> waits for the whole run and returns its final variables.
                {async && (
                  <>
                    {" "}
                    <b>run/start</b> returns a <span className="mono">runId</span> at once; the run is then followed under Runs, where a paused run can be resumed.
                  </>
                )}
              </p>
            </div>
          )}
        </div>
      </div>
      <Response call={call} kind={call?.method === "run/start" ? "run" : "tool"} tool={tool.name} />
    </div>
  );
}

function ResourceView({ uri }: { uri: string }) {
  const res = useMcp((s) => s.resources.find((r) => r.uri === uri));
  const read = useMcp((s) => s.reads[uri]);
  useEffect(() => {
    if (!read) void readResource(uri);
  }, [uri, read]);
  return (
    <div className="mcp-split">
      <div className="mcp-req">
        <div className="mcp-title">
          <h2>{res?.name ?? uri}</h2>
          {res?.mimeType && <span className="mcp-pill">{res.mimeType}</span>}
        </div>
        <UrlBar method={<span className="mcp-method-fixed m-read">resources/read</span>} path={<b>{uri}</b>} onSend={() => void readResource(uri)} pending={read?.pending} />
        {res?.description && <p className="muted small mcp-pad">{res.description}</p>}
      </div>
      <Response call={read} kind="resource" />
    </div>
  );
}

const RAW_EXAMPLES = ["ping", "tools/list", "resources/list", "server/discover", "run/list"];

function RawView() {
  const raw = useMcp((s) => s.raw);
  const p = rawParams(raw.params);
  const setRaw = (patch: Partial<typeof raw>) => set((s) => ({ raw: { ...s.raw, ...patch } }));
  return (
    <div className="mcp-split">
      <div className="mcp-req">
        <div className="mcp-title">
          <h2>Raw request</h2>
        </div>
        <UrlBar
          method={
            <>
              <input className="mcp-method-input mono" list="mcp-raw-methods" value={raw.method} onChange={(e) => setRaw({ method: e.target.value })} aria-label="method" />
              <datalist id="mcp-raw-methods">
                {RAW_EXAMPLES.map((m) => (
                  <option key={m} value={m} />
                ))}
              </datalist>
            </>
          }
          path={<span className="muted">{" "}— any JSON-RPC method</span>}
          onSend={() => void sendRaw()}
          pending={raw.call?.pending}
          disabled={!!p.error || !raw.method.trim()}
        />
        <Tabs<"params"> tabs={[["params", "Params", !!raw.params.trim()]]} value="params" onChange={() => undefined} />
        <div className="mcp-pad">
          <textarea className={`mcp-code ${p.error ? "invalid" : ""}`} spellCheck={false} rows={6} placeholder='{"name": "…", "arguments": {}}' value={raw.params} onChange={(e) => setRaw({ params: e.target.value })} />
          {p.error && <p className="mcp-err small">{p.error}</p>}
        </div>
      </div>
      <Response call={raw.call} kind="raw" />
    </div>
  );
}

// ------------------------------------------------------------------ response

type Kind = "tool" | "run" | "resource" | "raw";

/** What the Body tab shows for a successful reply. */
function bodyOf(result: unknown, kind: Kind): unknown {
  if (kind === "tool") {
    const r = result as CallToolResult;
    if (r?.structuredContent !== undefined) return r.structuredContent;
    const texts = (r?.content ?? []).filter((c) => c.type === "text");
    if (texts.length === 1) return parseEmbedded(texts[0].text ?? "") ?? texts[0].text;
    return result;
  }
  if (kind === "resource") {
    const contents = (result as { contents?: { text?: string }[] })?.contents ?? [];
    if (contents.length === 1 && contents[0].text !== undefined) return parseEmbedded(contents[0].text) ?? contents[0].text;
    return result;
  }
  return result;
}

function statusOf(call: Call, kind: Kind): { cls: "ok" | "bad" | "warn"; text: string } {
  if (call.error) return { cls: "bad", text: "Failed" };
  const r = call.reply!;
  if (!r.ok) return { cls: "bad", text: `Error ${r.error.code}` };
  if (kind === "tool" && (r.result as CallToolResult)?.isError) return { cls: "bad", text: "isError" };
  if (kind === "tool" && pausedCall(r.result)) return { cls: "warn", text: "Paused" };
  return { cls: "ok", text: "OK" };
}

function Body({ value }: { value: unknown }) {
  const mode = useMcp((s) => s.bodyMode);
  const decoded = useMemo(() => decodeEmbedded(value), [value]);
  const table = useMemo(() => asTable(decoded), [decoded]);
  const shown: BodyMode = mode === "table" && !table ? "pretty" : mode;
  return (
    <>
      <div className="mcp-res-modes">
        <div className="segmented small">
          {(["pretty", "raw", "table"] as BodyMode[]).map((m) => (
            <button key={m} className={shown === m ? "active" : ""} disabled={m === "table" && !table} title={m === "table" && !table ? "Not tabular: needs an object or an array of objects" : undefined} onClick={() => set({ bodyMode: m })}>
              {m === "pretty" ? "Pretty" : m === "raw" ? "Raw" : "Table"}
            </button>
          ))}
        </div>
      </div>
      <div className="mcp-res-scroll">
        {shown === "pretty" && (typeof decoded === "string" ? <pre className="mcp-text">{decoded}</pre> : <JsonView value={decoded} />)}
        {shown === "raw" && <pre className="mcp-text">{typeof value === "string" ? value : JSON.stringify(value, null, 2)}</pre>}
        {shown === "table" && table && (
          <table className="mcp-table">
            <thead>
              <tr>
                {!table.keyed && <th className="mcp-idx"></th>}
                {table.columns.map((c) => (
                  <th key={c}>{c}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {table.rows.map((row, i) => (
                <tr key={i}>
                  {!table.keyed && <td className="mcp-idx">{i}</td>}
                  {row.map((cell, j) => (
                    <td key={j} className={table.keyed && j === 0 ? "mono mcp-keycol" : ""}>
                      {cellText(cell)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}

function ContentBlocks({ blocks }: { blocks: ContentBlock[] }) {
  return (
    <div className="mcp-res-scroll mcp-blocks">
      {blocks.map((c, i) => {
        const parsed = c.type === "text" ? parseEmbedded(c.text ?? "") : undefined;
        return (
          <div key={i} className="mcp-block">
            <div className="mcp-block-head mono">
              content[{i}] · {c.type}
            </div>
            {c.type !== "text" ? <JsonView value={c} /> : parsed !== undefined ? <JsonView value={decodeEmbedded(parsed)} /> : <pre className="mcp-text">{c.text}</pre>}
          </div>
        );
      })}
    </div>
  );
}

/**
 * A tools/call that stopped at pause(): its vars are partial state, and the
 * run can't be resumed. Offers to run it again with run/start, whose pause
 * can be approved (restarting the server on HTTP when it is on stdio).
 */
function PausedNotice({ tool, reason, step }: { tool: string; reason: string; step?: string }) {
  const http = useMcp((s) => hasRunMethods(s.hello));
  const async = useMcp((s) => supportsAsync(s.tools.find((t) => t.name === tool) ?? { name: tool }));
  return (
    <div className="mcp-callout warn mcp-paused">
      <div>
        <Pause {...ICON} size={14} /> <b>Paused{step ? <> at <span className="mono">{step}</span></> : null}</b>: {reason}
      </div>
      <p className="small">
        The workflow is waiting for a decision. The body below is its state at the pause, not a result. A <span className="mono">tools/call</span> run can't be
        resumed: to approve it, run it with <span className="mono">run/start</span>, then resume it from Runs.
      </p>
      <div>
        <button className="btn small mcp-send" onClick={() => void runResumable(tool)} disabled={http && !async}>
          <Play {...ICON} size={13} /> {http ? "Run with run/start" : "Restart on HTTP and run with run/start"}
        </button>
      </div>
    </div>
  );
}

function Response({ call, kind, tool }: { call?: Call; kind: Kind; tool?: string }) {
  const tab = useMcp((s) => s.resTab);
  if (!call)
    return (
      <div className="mcp-res empty">
        <Send {...ICON} size={28} strokeWidth={1.25} />
        <p>Click Send to get a response</p>
      </div>
    );
  if (call.pending)
    return (
      <div className="mcp-res empty">
        <Loader {...ICON} size={24} className="spin" />
        <p>
          Waiting for <span className="mono">{call.method}</span>… <Elapsed since={call.started} />
        </p>
      </div>
    );
  const st = statusOf(call, kind);
  const r = call.reply;
  const paused = kind === "tool" && r?.ok ? pausedCall(r.result) : null;
  const content = r?.ok && kind === "tool" ? ((r.result as CallToolResult)?.content ?? []) : [];
  const message = r ? (r.ok ? { jsonrpc: "2.0", result: r.result } : { jsonrpc: "2.0", error: r.error }) : undefined;
  const size = message ? byteSize(message) : 0;
  const shown: ResTab = tab === "content" && content.length === 0 ? "body" : tab === "message" && !message ? "body" : tab;
  return (
    <div className="mcp-res">
      <div className="mcp-res-head">
        <Tabs<ResTab>
          tabs={[["body", "Body"], ...(content.length ? ([["content", `Content (${content.length})`]] as [ResTab, string][]) : []), ...(message ? ([["message", "Message"]] as [ResTab, string][]) : [])]}
          value={shown}
          onChange={(t) => set({ resTab: t })}
        />
        <span className="spacer" />
        <span className={`mcp-status-chip ${st.cls}`}>{st.text}</span>
        {r && <span className="mcp-meta">{r.ms} ms</span>}
        {r && <span className="mcp-meta">{formatBytes(size)}</span>}
      </div>
      {paused && tool && <PausedNotice tool={tool} reason={paused.reason} step={paused.step} />}
      {call.error ? (
        <div className="mcp-res-scroll">
          <pre className="mcp-text mcp-err">{call.error}</pre>
        </div>
      ) : shown === "content" ? (
        <ContentBlocks blocks={content} />
      ) : shown === "message" ? (
        <div className="mcp-res-scroll">
          <JsonView value={message} />
        </div>
      ) : (
        <Body value={r!.ok ? bodyOf(r!.result, kind) : r!.error} />
      )}
    </div>
  );
}

// ------------------------------------------------------------------ runs

function RunDetail({ id }: { id: string }) {
  const run = useMcp((s) => s.runs[id]);
  const tool = useMcp((s) => s.tools.find((t) => t.name === run?.status.tool));
  const values = useMcp((s) => s.resumeForms[id]) ?? {};
  const [tab, setTab] = useState<"vars" | "logs" | "status">("vars");
  const scrollRef = useRef<HTMLDivElement>(null);
  const fields = useMemo(() => schemaFields(tool?.inputSchema).map((f) => ({ ...f, required: false })), [tool]);
  useEffect(() => {
    if (tab === "logs" && scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  }, [run?.logs, tab]);
  if (!run) return <p className="muted mcp-pad">This run is no longer known.</p>;
  const st = run.status;
  const { errors } = buildArguments(fields, values);
  const resumable = st.state === "paused" || (st.resumable && !isActive(st.state));
  const pct = runProgress(st);
  const reachedCount = new Set(st.reached ?? []).size;
  return (
    <div className="mcp-split">
      <div className="mcp-req">
        <div className="mcp-title">
          <h2>{st.tool}</h2>
          <StateChip state={st.state} />
          <span className="muted small mono" title={st.runId}>
            {st.runId}
          </span>
          <span className="spacer" />
          {(isActive(st.state) || st.state === "paused") && (
            <button className="btn small danger" onClick={() => void cancelRun(id)}>
              <Square {...ICON} size={13} /> run/cancel
            </button>
          )}
        </div>
        <div className="mcp-progress">
          <div className={`mcp-progress-bar ${st.state}`} style={{ width: `${pct}%` }} />
        </div>
        <div className="mcp-steps">
          {(st.reached ?? []).map((s, i) => (
            <span key={i} className="mcp-step">
              {i > 0 && <span className="muted">→</span>}
              <span className={`mono ${s === st.step ? "current" : ""}`}>{s}</span>
            </span>
          ))}
          {st.stepTotal ? (
            <span className="muted small" title="Distinct steps this run has executed, of the workflow's declared steps (a goto can skip some)">
              {reachedCount} of {st.stepTotal} steps run
            </span>
          ) : null}
          {st.startedAt && <span className="muted small">· started {new Date(st.startedAt).toLocaleTimeString(undefined, { hour12: false })}</span>}
        </div>
        {run.error && <div className="mcp-banner bad">{run.error}</div>}
        {st.error && (
          <div className="mcp-callout bad">
            <b>{st.state}</b> <span className="mono small">{st.error}</span>
          </div>
        )}
        {resumable && (
          <div className="mcp-callout warn">
            <div>
              <b>{st.state === "paused" ? "Paused" : "Stopped — resumable"}</b>
              {st.reason && <span>: {st.reason}</span>}
            </div>
            <p className="muted small">run/resume re-enters the step with these arguments merged into its inputs; blank ones keep their value.</p>
            <Fields fields={fields} values={values} errors={errors} onChange={(n, v) => setResumeValue(id, n, v)} />
            <div>
              <button className="btn small mcp-send" disabled={Object.keys(errors).length > 0} onClick={() => void resumeRun(id)}>
                <Play {...ICON} size={13} /> run/resume
              </button>
            </div>
          </div>
        )}
      </div>
      <div className="mcp-res">
        <div className="mcp-res-head">
          <Tabs<"vars" | "logs" | "status">
            tabs={[
              ["vars", "Vars", !!st.vars],
              ["logs", "Logs", !!run.logs],
              ["status", "Status"],
            ]}
            value={tab}
            onChange={setTab}
          />
          <span className="spacer" />
          {isActive(st.state) && (
            <span className="mcp-meta">
              <Loader {...ICON} size={12} className="spin" /> polling
            </span>
          )}
        </div>
        <div className="mcp-res-scroll" ref={scrollRef}>
          {tab === "vars" && (st.vars ? <JsonView value={decodeEmbedded(st.vars)} /> : <p className="muted small mcp-pad">The variables appear when the run completes or pauses.</p>)}
          {tab === "logs" && <pre className="mcp-text">{run.logs || "No output yet."}</pre>}
          {tab === "status" && <JsonView value={st} />}
        </div>
      </div>
    </div>
  );
}

function StateChip({ state }: { state: RunState }) {
  return <span className={`mcp-state ${state}`}>{state}</span>;
}

// ------------------------------------------------------------------ main

function Detail() {
  const sel = useMcp((s) => s.selected);
  const tool = useMcp((s) => (sel?.kind === "tool" ? s.tools.find((t) => t.name === sel.id) : undefined));
  const status = useMcp((s) => s.status);
  if (status !== "ready") return <Idle />;
  if (sel?.kind === "raw") return <RawView />;
  if (sel?.kind === "tool" && tool) return <ToolView key={tool.name} tool={tool} />;
  if (sel?.kind === "resource") return <ResourceView key={sel.id} uri={sel.id} />;
  if (sel?.kind === "run") return <RunDetail key={sel.id} id={sel.id} />;
  return <div className="mcp-res empty">Pick a tool, a resource or a run.</div>;
}

function Idle() {
  const status = useMcp((s) => s.status);
  const transport = useMcp((s) => s.transport);
  return (
    <div className="mcp-idle">
      <h2>Test this folder as an MCP server</h2>
      <p className="muted">
        Every workflow and pipeline in the folder becomes an MCP tool, exactly as <span className="mono">mhl serve mcp</span> serves it to Claude Desktop, VS Code or any other MCP
        client. Call the tools, read the workflow manifests and — over HTTP — run workflows asynchronously, approve a paused run or cancel one.
      </p>
      {status === "stopped" || status === "failed" ? (
        <button className="btn mcp-send" onClick={() => void startServer()}>
          <Play {...ICON} size={14} /> Start over {transport === "stdio" ? "stdio" : "HTTP"}
        </button>
      ) : (
        <p className="muted">
          <Loader {...ICON} size={14} className="spin" /> Starting…
        </p>
      )}
    </div>
  );
}

// ------------------------------------------------------------------ console

function TrafficRow({ t }: { t: Traffic }) {
  const [open, setOpen] = useState(false);
  const isErr = t.dir === "in" && (t.text.includes('"error"') || (t.status !== undefined && t.status >= 400));
  const value = useMemo(() => (open ? decodeEmbedded(parseEmbedded(t.text) ?? t.text) : null), [open, t.text]);
  return (
    <>
      <tr className={`${isErr ? "bad" : ""} clickable`} onClick={() => setOpen(!open)}>
        <td className="mono">{clock(t.at)}</td>
        <td className={`mono mcp-dir ${t.dir}`}>{t.dir === "out" ? "→" : "←"}</td>
        <td className="mono">{t.method ?? ""}</td>
        <td className="mono">{t.rpcId ?? ""}</td>
        <td className="mono">{t.status ?? ""}</td>
        <td className="mono">{t.ms !== undefined ? `${t.ms} ms` : ""}</td>
        <td className="mono">{formatBytes(byteSize(t.text))}</td>
        <td className="mono mcp-trunc">{t.text || "(empty body)"}</td>
      </tr>
      {open && (
        <tr className="mcp-expanded">
          <td colSpan={8}>{typeof value === "string" ? <pre className="mcp-text">{value || "(empty body)"}</pre> : <JsonView value={value} maxHeight={320} />}</td>
        </tr>
      )}
    </>
  );
}

function Console() {
  const open = useMcp((s) => s.consoleOpen);
  const bottom = useMcp((s) => s.bottom);
  const traffic = useMcp((s) => s.traffic);
  const log = useMcp((s) => s.serverLog);
  const showQuiet = useMcp((s) => s.showQuiet);
  const rows = showQuiet ? traffic : traffic.filter((t) => !t.quiet);
  const logRef = useRef<HTMLPreElement>(null);
  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [log.length, bottom, open]);
  const tab = (b: "traffic" | "log", label: string) => (
    <button className={`mcp-tab ${open && bottom === b ? "active" : ""}`} onClick={() => set({ bottom: b, consoleOpen: !(open && bottom === b) })}>
      {label}
    </button>
  );
  return (
    <div className={`mcp-console ${open ? "open" : ""}`}>
      <div className="mcp-console-bar">
        <Terminal {...ICON} size={14} />
        <span className="small">Console</span>
        {tab("traffic", `Traffic (${rows.length})`)}
        {tab("log", `Server log (${log.length})`)}
        <span className="spacer" />
        {open && bottom === "traffic" && (
          <label className="small muted mcp-check">
            <input type="checkbox" checked={showQuiet} onChange={(e) => set({ showQuiet: e.target.checked })} /> show status polls
          </label>
        )}
        {open && (
          <button className="btn small ghost" onClick={clearTraffic}>
            Clear
          </button>
        )}
        <button className="icon-btn" title={open ? "Hide the console" : "Show the console"} onClick={() => set({ consoleOpen: !open })}>
          {open ? <ChevronDown size={14} /> : <ChevronRight size={14} style={{ transform: "rotate(-90deg)" }} />}
        </button>
      </div>
      {open && (
        <div className="mcp-console-body">
          {bottom === "traffic" ? (
            <table className="events">
              <thead>
                <tr>
                  <th>Time</th>
                  <th></th>
                  <th>Method</th>
                  <th>id</th>
                  <th>HTTP</th>
                  <th>Took</th>
                  <th>Size</th>
                  <th>Message</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((t) => (
                  <TrafficRow key={t.seq} t={t} />
                ))}
              </tbody>
            </table>
          ) : (
            <pre className="logs" ref={logRef}>
              {log.join("\n") || "Nothing logged yet."}
            </pre>
          )}
        </div>
      )}
    </div>
  );
}

export function McpPanel() {
  const status = useMcp((s) => s.status);
  useEffect(() => {
    // The lists are fetched on connect; this catches up when the panel is reopened.
    if (status === "ready") void refreshLists();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return (
    <div className="mcp-panel">
      <div className="mcp-shell">
        <Sidebar />
        <div className="mcp-work">
          <TopBar />
          <Banners />
          <Detail />
        </div>
      </div>
      <Console />
    </div>
  );
}
