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
  canPause,
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
import {
  hasRunMethods,
  isActive,
  pausePoints,
  isControlTool,
  pausedCall,
  resumeFields,
  runProgress,
  supportsAsync,
  type AwaitingDecision,
  type AwaitingInputs,
  type CallToolResult,
  type ContentBlock,
  type McpTool,
  type RunState,
  type RunStatus,
} from "./model";
import { buildArguments, fieldsFor, journeyOf, pausesFor, placeholder, schemaFields, valuesFor, type JourneyStep, type SchemaField } from "./schema";
import { rawParams, type Traffic } from "./rpc";
import { asTable, byteSize, cellText, decodeEmbedded, formatBytes, parseEmbedded } from "./json";
import { JsonView } from "./JsonView";
import { DecisionForm } from "../run/DecisionForm";

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

function Fields({
  fields,
  values,
  errors,
  onChange,
  disabled,
  stagePills = true,
}: {
  fields: SchemaField[];
  values: Record<string, string>;
  errors: Record<string, string>;
  onChange: (name: string, v: string) => void;
  disabled?: boolean;
  /** Tag each stage input with its stage — not where the stage is the section's heading. */
  stagePills?: boolean;
}) {
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
                {stagePills && f.stage && (
                  <span className="mcp-pill mcp-stage-pill" title={`Input of stage ${f.stage}: the run asks for it on entering the stage; give it now to run straight through`}>
                    {f.stage}
                  </span>
                )}
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

/**
 * A staged workflow's arguments as the run asks for them: what it needs to
 * start, then each stage in run order. With run/start a stage's inputs wait
 * for the stage (it asks for them when the run gets there, under Runs) and
 * are folded away; with tools/call, which can't resume a pause, every input a
 * stage would stop for is needed now.
 */
function Journey(props: {
  tool: string;
  steps: JourneyStep[];
  method: "tools/call" | "run/start";
  async: boolean;
  values: Record<string, string>;
  errors: Record<string, string>;
  onChange: (name: string, v: string) => void;
  disabled?: boolean;
}) {
  const { tool, steps, method, async, values, errors, onChange, disabled } = props;
  const [opened, setOpened] = useState<Record<string, boolean>>({});
  const stages = steps.filter((st) => st.stage);
  const asking = stages.filter((st) => st.fields.some(pausesFor));
  const toggle = (stage: string) => setOpened((o) => ({ ...o, [stage]: !o[stage] }));
  return (
    <div className="mcp-journey">
      <p className="small mcp-journey-intro">
        {method === "run/start" ? (
          <>
            This workflow runs in {stages.length} stage{stages.length === 1 ? "" : "s"}
            {asking.length > 0 && (
              <>
                {" "}
                and asks for inputs as it goes: fill in <b>what it needs to start</b>, then send. When the run reaches{" "}
                {asking.map((st, i) => (
                  <span key={st.stage}>
                    {i > 0 ? (i === asking.length - 1 ? " or " : ", ") : ""}
                    <span className="mono">{st.stage}</span>
                  </span>
                ))}
                , it pauses and asks for that stage's inputs under Runs
              </>
            )}
            .
          </>
        ) : (
          <>
            <b>tools/call</b> waits for the whole run and can't resume a pause, so every stage's inputs are needed now.
            {async && (
              <>
                {" "}
                To give them stage by stage, as the run reaches each one,{" "}
                <button className="link" onClick={() => set((s) => ({ methods: { ...s.methods, [tool]: "run/start" } }))}>
                  use run/start
                </button>
                .
              </>
            )}
          </>
        )}
      </p>
      <ol className="mcp-journey-steps">
        {steps.map((st, i) => {
          const n = i + 1;
          if (!st.stage)
            return (
              <li key="$start" className="mcp-journey-step now">
                <div className="mcp-journey-head">
                  <span className="mcp-journey-n">{n}</span>
                  <b>Start</b>
                  <span className="muted small">{st.fields.length ? "needed to start the run" : "nothing to provide"}</span>
                </div>
                {st.fields.length > 0 && <Fields fields={st.fields} values={values} errors={errors} onChange={onChange} disabled={disabled} stagePills={false} />}
              </li>
            );
          const stage = st.stage;
          const filled = st.fields.some((f) => (values[f.name] ?? "") !== "");
          const pauses = st.fields.some(pausesFor);
          const later = method === "run/start";
          const open = st.fields.length > 0 && (!later || filled || !!opened[stage]);
          const note = !st.fields.length
            ? "asks for nothing"
            : !later
              ? "needed now"
              : pauses
                ? filled
                  ? "given now — the run won't stop here"
                  : "asked when the run reaches it"
                : "all have defaults — optional";
          return (
            <li key={stage} className={`mcp-journey-step ${later && st.fields.length ? "later" : ""} ${st.fields.length ? "" : "empty"}`}>
              <div className="mcp-journey-head">
                <span className="mcp-journey-n">{n}</span>
                <b className="mono">{stage}</b>
                <span className="muted small">{note}</span>
                {later && st.fields.length > 0 && !filled && (
                  <button className="link small" onClick={() => toggle(stage)}>
                    {opened[stage] ? "hide" : pauses ? "fill in now instead" : "change defaults"}
                  </button>
                )}
              </div>
              {open && <Fields fields={st.fields} values={values} errors={errors} onChange={onChange} disabled={disabled} stagePills={false} />}
              {!open && st.fields.length > 0 && <p className="muted small mono mcp-journey-names">{st.fields.map((f) => f.name).join(", ")}</p>}
            </li>
          );
        })}
      </ol>
    </div>
  );
}

/** What each way to run a tool does — shown in the picker, and as the fixed method's tooltip. */
const METHODS: Record<ToolMethod, { text: string }> = {
  "tools/call": { text: "Waits for the run to finish and returns its result. Can't resume a pause — it is what a stock MCP client sends." },
  "run/start": { text: "Returns a runId at once; the run is followed under Runs, where a pause is answered and the run resumed." },
};

/** tools/call or run/start, each with what it does — for a workflow that never pauses, where both work. */
function MethodPicker({ value, onChange }: { value: ToolMethod; onChange: (m: ToolMethod) => void }) {
  const [at, setAt] = useState<{ top: number; left: number } | null>(null);
  return (
    <>
      <button
        className={`mcp-method-select mcp-method-picker ${value === "run/start" ? "m-ctrl" : "m-call"}`}
        aria-haspopup="menu"
        aria-expanded={!!at}
        title={METHODS[value].text}
        onClick={(e) => {
          const r = e.currentTarget.getBoundingClientRect();
          setAt(at ? null : { top: r.bottom + 4, left: r.left });
        }}
      >
        {value} <ChevronDown size={13} />
      </button>
      {at && (
        <>
          <div className="menu-backdrop" onClick={() => setAt(null)} />
          <div className="menu mcp-method-menu" role="menu" style={at}>
            {(["tools/call", "run/start"] as ToolMethod[]).map((m) => (
              <button
                key={m}
                role="menuitemradio"
                aria-checked={m === value}
                className={`menu-item ${m === value ? "checked" : ""}`}
                onClick={() => {
                  onChange(m);
                  setAt(null);
                }}
              >
                <span className="menu-icon">{m === value ? "✓" : ""}</span>
                <span>
                  <span className={`menu-label mono ${m === "run/start" ? "m-ctrl" : "m-call"}`}>{m}</span>
                  <span className="menu-hint">{METHODS[m].text}</span>
                </span>
              </button>
            ))}
          </div>
        </>
      )}
    </>
  );
}

/**
 * Why a workflow that may pause runs with run/start only: where it pauses,
 * and that a tools/call can't resume. Over stdio — tools/call only — offers to
 * restart the server on HTTP, where run/start exists.
 */
function PauseNote({ tool, async }: { tool: McpTool; async: boolean }) {
  const p = pausePoints(tool);
  const stages = [...new Set([...(p?.stages ?? []), ...schemaFields(tool.inputSchema).filter(pausesFor).map((f) => f.stage as string)])];
  const parts: React.ReactNode[] = [];
  if (p?.steps.length)
    parts.push(
      <span key="steps">
        at <span className="mono">{p.steps.join(", ")}</span> (a decision)
      </span>,
    );
  if (stages.length)
    parts.push(
      <span key="stages">
        on entering <span className="mono">{stages.join(", ")}</span> (its inputs)
      </span>,
    );
  const where = parts.length ? (
    <>
      {" "}
      {parts.reduce<React.ReactNode[]>((acc, x, i) => (i ? [...acc, " and ", x] : [x]), [])}
    </>
  ) : null;
  return async ? (
    <p className="small mcp-pause-note">
      <Pause {...ICON} size={13} /> <b>run/start only.</b> This workflow pauses{where}, and a <span className="mono">tools/call</span> can't resume a paused run
      — it would stop for good at the first pause. With run/start, each pause is answered under Runs.
    </p>
  ) : (
    <p className="small mcp-pause-note">
      <Pause {...ICON} size={13} /> This workflow may pause{where}. Over stdio only <span className="mono">tools/call</span> exists, and it can't resume a paused run.{" "}
      <button className="link" onClick={() => void startServer("http")}>
        Restart on HTTP
      </button>{" "}
      to run it with run/start and answer each pause.
    </p>
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
  const all = useMemo(() => schemaFields(tool.inputSchema), [tool.inputSchema]);
  const fields = useMemo(() => fieldsFor(all, m), [all, m]);
  const journey = useMemo(() => journeyOf(tool.inputSchema, fields), [tool.inputSchema, fields]);
  const { args, errors } = buildArguments(fields, values);
  const blocked = Object.keys(errors).length > 0;
  const async = supportsAsync(tool);
  const pauses = canPause(tool);
  return (
    <div className={`mcp-split ${call ? "" : "solo"}`}>
      <div className="mcp-req">
        <div className="mcp-title">
          <h2>{tool.name}</h2>
          {async && <span className="mcp-pill" title="_meta.mhl.run: run/start can run it asynchronously">async</span>}
        </div>
        <UrlBar
          method={
            async && !pauses ? (
              <MethodPicker value={m} onChange={(v) => set((s) => ({ methods: { ...s.methods, [tool.name]: v } }))} />
            ) : (
              <span className={`mcp-method-fixed ${m === "run/start" ? "m-ctrl" : "m-call"}`} title={METHODS[m].text}>
                {m}
              </span>
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
        {pauses && <PauseNote tool={tool} async={async} />}
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
              {journey ? (
                <Journey
                  tool={tool.name}
                  steps={journey}
                  method={m}
                  async={async}
                  values={values}
                  errors={errors}
                  onChange={(n, v) => setFormValue(tool.name, n, v)}
                  disabled={call?.pending}
                />
              ) : (
                <Fields fields={fields} values={values} errors={errors} onChange={(n, v) => setFormValue(tool.name, n, v)} disabled={call?.pending} />
              )}
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
              <p>{tool.description || <span className="muted">No description. Add one as `///` lines right above the workflow.</span>}</p>
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
    <div className={`mcp-split ${read ? "" : "solo"}`}>
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
    <div className={`mcp-split ${raw.call ? "" : "solo"}`}>
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
function PausedNotice({ tool, reason, step, awaitingInputs, awaiting }: { tool: string; reason: string; step?: string; awaitingInputs?: AwaitingInputs; awaiting?: AwaitingDecision }) {
  const http = useMcp((s) => hasRunMethods(s.hello));
  const async = useMcp((s) => supportsAsync(s.tools.find((t) => t.name === tool) ?? { name: tool }));
  return (
    <div className="mcp-callout warn mcp-paused">
      <div>
        <Pause {...ICON} size={14} /> <b>Paused{step ? <> at <span className="mono">{step}</span></> : null}</b>: {reason}
      </div>
      {awaitingInputs ? (
        <p className="small">
          It stopped on entering stage <span className="mono">{awaitingInputs.stage}</span>, which asks for{" "}
          <span className="mono">{awaitingInputs.inputs.join(", ")}</span>. The body below is its state there, not a result. A{" "}
          <span className="mono">tools/call</span> run can't be resumed: give {awaitingInputs.inputs.length === 1 ? "that input" : "those inputs"} in the arguments to run
          straight through, or run it with <span className="mono">run/start</span> and pass them when you resume it from Runs.
        </p>
      ) : (
        <p className="small">
          The workflow is waiting for a decision
          {awaiting ? (
            <>
              {" "}
              of type <span className="mono">{awaiting.type}</span>
            </>
          ) : null}
          . The body below is its state at the pause, not a result. A <span className="mono">tools/call</span> run can't be resumed: to{" "}
          {awaiting ? "answer the gate" : "approve it"}, run it with <span className="mono">run/start</span>, then resume it from Runs
          {awaiting ? " with a decision" : ""}.
        </p>
      )}
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
  // Nothing sent yet: no response pane at all — the request gets the whole height.
  if (!call) return null;
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
      {paused && tool && <PausedNotice tool={tool} reason={paused.reason} step={paused.step} awaitingInputs={paused.awaitingInputs} awaiting={paused.awaiting} />}
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

/**
 * Where a run of a staged workflow is: Start, then each stage in run order —
 * done, the one it is in (or paused at, waiting for you), and the ones still
 * ahead with what each will ask for. null for a workflow without stages.
 */
function RunJourney({ status: st, schema }: { status: RunStatus; schema: unknown }) {
  const steps = useMemo(() => journeyOf(schema), [schema]);
  if (!steps) return null;
  const stages = steps.filter((x) => x.stage).map((x) => x.stage as string);
  const current = st.awaiting_inputs?.stage ?? st.stage;
  const at = current ? stages.indexOf(current) : -1;
  const ended = st.state === "completed";
  return (
    <ol className="mcp-run-journey" aria-label="Stages">
      {steps.map((x, i) => {
        const idx = i - 1; // -1 is Start
        let cls = "ahead";
        let note = "";
        if (ended || (idx >= 0 && idx < at) || (idx === -1 && at >= 0)) cls = "done";
        else if (idx === at || (idx === -1 && at < 0)) {
          cls = st.state === "paused" ? "waiting" : st.state === "failed" || st.state === "canceled" ? "stopped" : "current";
          note =
            st.state === "paused"
              ? st.awaiting_inputs
                ? `needs ${st.awaiting_inputs.inputs.join(", ")}`
                : st.awaiting
                  ? `waits for a ${st.awaiting.type} decision`
                  : "paused"
              : cls === "stopped"
                ? st.state
                : "running";
        } else {
          const asks = x.fields.filter(pausesFor).map((f) => f.name);
          note = asks.length ? `will ask for ${asks.join(", ")}` : "";
        }
        return (
          <li key={x.stage ?? "$start"} className={`mcp-run-stage ${cls}`} title={note || undefined}>
            <span className="mcp-run-stage-mark">{cls === "done" ? "✓" : cls === "waiting" ? "⏸" : cls === "current" ? "●" : cls === "stopped" ? "✕" : i + 1}</span>
            <span className={x.stage ? "mono" : ""}>{x.stage ?? "Start"}</span>
            {note && <span className="mcp-run-stage-note small">{note}</span>}
          </li>
        );
      })}
    </ol>
  );
}

function RunDetail({ id }: { id: string }) {
  const run = useMcp((s) => s.runs[id]);
  const tool = useMcp((s) => s.tools.find((t) => t.name === run?.status.tool));
  const values = useMcp((s) => s.resumeForms[id]) ?? {};
  const [tab, setTab] = useState<"vars" | "logs" | "status">("vars");
  const scrollRef = useRef<HTMLDivElement>(null);
  const waiting = run?.status.state === "paused" ? run.status.awaiting_inputs : undefined;
  // A typed gate (`pause(..., decision: T)`): run/resume answers it with a decision, not arguments.
  const gate = run?.status.state === "paused" ? run.status.awaiting : undefined;
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const fields = useMemo(() => resumeFields(schemaFields(tool?.inputSchema), run?.status), [tool, run?.status.state, waiting]);
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
        <RunJourney status={st} schema={tool?.inputSchema} />
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
            {gate ? (
              <DecisionForm
                key={`${id}:${gate.step}#${gate.index}:${(st.reached ?? []).length}`}
                awaiting={gate}
                reason=""
                submitLabel="run/resume with decision"
                onSubmit={(decision) => void resumeRun(id, decision)}
              />
            ) : waiting ? (
              <p className="muted small">
                The run stopped before <span className="mono">{waiting.step}</span>, on entering stage <span className="mono">{waiting.stage}</span>. run/resume
                must carry {waiting.inputs.length === 1 ? "its input" : "its inputs"} — only {waiting.inputs.length === 1 ? "it is" : "they are"} sent.
              </p>
            ) : (
              <p className="muted small">run/resume re-enters the step with these arguments merged into its inputs; blank ones keep their value.</p>
            )}
            {!gate && (
              <>
                <Fields fields={fields} values={values} errors={errors} onChange={(n, v) => setResumeValue(id, n, v)} />
                <div>
                  <button className="btn small mcp-send" disabled={Object.keys(errors).length > 0} onClick={() => void resumeRun(id)}>
                    <Play {...ICON} size={13} /> run/resume
                  </button>
                </div>
              </>
            )}
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
