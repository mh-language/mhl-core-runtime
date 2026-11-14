// The tool builder: edit a `tool` declaration — its name, methods,
// signatures and bodies — and test a method with real arguments. Every
// change is a text edit (sync/tools.ts) on the open file, like the canvas.
import { useEffect, useMemo, useState } from "react";
import { ask } from "@tauri-apps/plugin-dialog";
import { edit, evaluate, revealSpan } from "../app/controller";
import { currentWorkflow, selectTool, set, useStore } from "../app/store";
import type { Declaration, RunResult, ToolMethod } from "../lib/types";
import { Doc, insertState } from "../sync/edits";
import {
  addMethod,
  convertedBody,
  deleteMethod,
  deleteTool,
  getMethodBody,
  renameTool,
  setMethodBody,
  setSignature,
  signatureOf,
  testCall,
  type Signature,
} from "../sync/tools";
import { placeholder } from "../sync/templates";
import { Field } from "./Inspector";

const BUILTIN_TYPES = ["string", "number", "bool", "object", "any", "string[]", "number[]", "object[]"];

async function confirm(message: string): Promise<boolean> {
  try {
    return await ask(message, { title: "mhl studio", kind: "warning" });
  } catch {
    return window.confirm(message);
  }
}

/** Type names to suggest: the primitives plus the file's own types, enums and aliases. */
function TypeOptions({ doc }: { doc: Doc }) {
  const own = doc.graph.declarations.filter((d) => ["type", "enum", "alias", "schema"].includes(d.kind)).map((d) => d.name);
  return (
    <datalist id="mhl-types">
      {[...BUILTIN_TYPES, ...own].map((t) => (
        <option key={t} value={t} />
      ))}
    </datalist>
  );
}

export function ToolInspector({ doc, name, method }: { doc: Doc; name: string; method: string | null }) {
  const tool = doc.graph.declarations.find((d) => d.kind === "tool" && d.name === name);
  if (!tool) return <div className="inspector-body muted">Tool {name} is no longer in this file.</div>;
  const m = method ? tool.methods?.find((x) => x.name === method) : undefined;
  if (m) return <MethodInspector key={`${name}.${m.name}`} doc={doc} tool={tool} m={m} />;
  return <ToolOverview doc={doc} tool={tool} />;
}

function ToolOverview({ doc, tool }: { doc: Doc; tool: Declaration }) {
  const refs = tool.refs?.length ?? 0;
  return (
    <div className="inspector-body">
      <div className="inspector-kind">Tool</div>
      <Field
        label="Name"
        value={tool.name}
        mono
        onCommit={(v) => void edit((d) => renameTool(d, tool.name, v.trim())).then((ok) => ok && selectTool(v.trim()))}
      />
      <p className="muted small">
        {tool.exported ? "Exported — other files can import it. " : ""}
        {refs === 0 ? "Not called in this file yet." : `Called ${refs} ${refs === 1 ? "time" : "times"} in this file.`} A rename rewrites the calls in
        this file; files that import it are not changed.
      </p>

      <section>
        <h4>Methods</h4>
        {(tool.methods ?? []).length === 0 && <p className="muted small">No methods.</p>}
        {(tool.methods ?? []).map((m) => (
          <button key={m.name} className="link-row mono" onClick={() => selectTool(tool.name, m.name)}>
            {m.internal ? "internal " : ""}
            {m.name}({m.params.join(", ")}){m.returns ? `: ${m.returns}` : ""}
          </button>
        ))}
        <button
          className="btn small"
          onClick={() => {
            let added = "";
            void edit((d) => {
              const r = addMethod(d, tool.name);
              added = r.name;
              return r;
            }).then((ok) => ok && added && selectTool(tool.name, added));
          }}
        >
          + Add method
        </button>
      </section>

      <div className="button-row">
        <button className="btn small" onClick={() => revealSpan(tool.span)}>
          Show code
        </button>
        <button
          className="btn small danger"
          onClick={() =>
            void (async () => {
              const msg = refs
                ? `Delete tool ${tool.name}? It is still called ${refs} ${refs === 1 ? "time" : "times"} in this file, and those calls will become errors.`
                : `Delete tool ${tool.name}?`;
              if (!(await confirm(msg))) return;
              if (await edit((d) => deleteTool(d, tool.name))) set({ tool: null });
            })()
          }
        >
          Delete tool
        </button>
      </div>
      <TypeOptions doc={doc} />
    </div>
  );
}

function SignatureEditor({ doc, tool, m }: { doc: Doc; tool: Declaration; m: ToolMethod }) {
  const original = useMemo(() => signatureOf(m), [m]);
  const [sig, setSig] = useState<Signature>(original);
  const key = JSON.stringify(original);
  useEffect(() => setSig(original), [key]); // eslint-disable-line react-hooks/exhaustive-deps
  const changed = JSON.stringify(sig) !== key;
  const setArg = (i: number, patch: Partial<Signature["args"][number]>) =>
    setSig((s) => ({ ...s, args: s.args.map((a, j) => (j === i ? { ...a, ...patch } : a)) }));
  const apply = () =>
    void edit((d) => setSignature(d, tool.name, m.name, sig)).then((ok) => ok && sig.name.trim() !== m.name && selectTool(tool.name, sig.name.trim()));
  return (
    <section
      onKeyDown={(e) => {
        if (e.key === "Enter" && changed) apply();
      }}
    >
      <label className="field">
        <span>Name</span>
        <input className="mono" value={sig.name} spellCheck={false} onChange={(e) => setSig({ ...sig, name: e.target.value })} />
      </label>
      <h4>Parameters</h4>
      {sig.args.length === 0 && <p className="muted small">No parameters.</p>}
      {sig.args.map((a, i) => (
        <div key={i} className="param-row">
          <input className="mono" placeholder="name" value={a.name} spellCheck={false} onChange={(e) => setArg(i, { name: e.target.value })} />
          <input className="mono" placeholder="type" list="mhl-types" value={a.type} spellCheck={false} onChange={(e) => setArg(i, { type: e.target.value })} />
          <input className="mono" placeholder="default" value={a.default} spellCheck={false} onChange={(e) => setArg(i, { default: e.target.value })} />
          <button className="icon-btn" title="Remove parameter" onClick={() => setSig({ ...sig, args: sig.args.filter((_, j) => j !== i) })}>
            ×
          </button>
        </div>
      ))}
      <button
        className="btn small"
        onClick={() => setSig({ ...sig, args: [...sig.args, { name: `arg${sig.args.length + 1}`, type: "string", default: "" }] })}
      >
        + Parameter
      </button>
      <label className="field">
        <span>Returns</span>
        <input className="mono" placeholder="(untyped)" list="mhl-types" value={sig.returns} spellCheck={false} onChange={(e) => setSig({ ...sig, returns: e.target.value })} />
      </label>
      <label className="check">
        <input type="checkbox" checked={sig.internal} onChange={(e) => setSig({ ...sig, internal: e.target.checked })} />
        <span>
          internal <span className="muted small">— only this tool (self.{sig.name || m.name}) and this file's tests may call it</span>
        </span>
      </label>
      {changed && (
        <div className="button-row">
          <button className="btn small primary" onClick={apply}>
            Apply signature
          </button>
          <button className="btn small" onClick={() => setSig(original)}>
            Revert
          </button>
        </div>
      )}
      <TypeOptions doc={doc} />
    </section>
  );
}

function BodyEditor({ doc, tool, m }: { doc: Doc; tool: Declaration; m: ToolMethod }) {
  const body = useMemo(() => getMethodBody(doc, tool.name, m.name), [doc, tool.name, m.name]);
  const switchKind = (kind: ToolMethod["bodyKind"]) => {
    if (kind === body.kind) return;
    void edit((d) => {
      const cur = getMethodBody(d, tool.name, m.name);
      const next = cur.text.trim() ? convertedBody(cur.kind, cur.text) : { kind, text: kind === "expr" ? "null" : "" };
      return setMethodBody(d, tool.name, m.name, next.kind, next.text);
    });
  };
  return (
    <section>
      <div className="body-head">
        <h4>Body</h4>
        <div className="segmented small">
          <button className={body.kind === "expr" ? "active" : ""} title="name(...) -> expression" onClick={() => switchKind("expr")}>
            Expression
          </button>
          <button className={body.kind === "block" ? "active" : ""} title="name(...) { statements; return value }" onClick={() => switchKind("block")}>
            Block
          </button>
        </div>
      </div>
      <Field
        label={body.kind === "expr" ? "-> value (native ops: fs, cmd, git, http, json, …)" : "{ statements } — end with return value"}
        value={body.text}
        multiline
        mono
        onCommit={(v) => void edit((d) => setMethodBody(d, tool.name, m.name, body.kind, v))}
      />
    </section>
  );
}

/** Pretty-prints a result value. */
function show(v: unknown): string {
  if (typeof v === "string") return JSON.stringify(v);
  try {
    return JSON.stringify(v, null, 2) ?? "null";
  } catch {
    return String(v);
  }
}

/** The run log without the session/step lines every run prints. */
function userLog(log: string | undefined): string {
  return (log ?? "")
    .split("\n")
    .filter((l) => l && !/^(session|step): /.test(l))
    .join("\n");
}

function TestPanel({ tool, m }: { tool: Declaration; m: ToolMethod }) {
  const [values, setValues] = useState<string[]>(() => m.args.map((a) => (a.default !== undefined ? "" : placeholder(a.type))));
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ call: string; res: RunResult } | null>(null);
  const [error, setError] = useState("");
  if (m.internal) {
    return (
      <section>
        <h4>Test</h4>
        <p className="muted small">An internal method can only be called through a public one, or from a test in this file.</p>
      </section>
    );
  }
  const run = async () => {
    let call: string;
    try {
      call = testCall(tool.name, m, values);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      return;
    }
    setError("");
    setBusy(true);
    const res = await evaluate(call);
    setBusy(false);
    if (res) setResult({ call, res });
  };
  const log = userLog((result?.res as RunResult & { log?: string } | undefined)?.log);
  return (
    <section
      onKeyDown={(e) => {
        if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) void run();
      }}
    >
      <h4>Test</h4>
      <p className="muted small">Saves the file and calls the method for real — native ops run (files, commands, HTTP). Arguments are mhl values: "text", 3, true, [1, 2].</p>
      {m.args.map((a, i) => (
        <label key={a.name} className="field inline">
          <span className="mono">
            {a.name}
            {a.type ? `: ${a.type}` : ""}
          </span>
          <input
            className="mono"
            value={values[i] ?? ""}
            placeholder={a.default !== undefined ? `default ${a.default}` : placeholder(a.type)}
            spellCheck={false}
            onChange={(e) => setValues((vs) => vs.map((v, j) => (j === i ? e.target.value : v)))}
          />
        </label>
      ))}
      <div className="button-row">
        <button className="btn small primary" disabled={busy} onClick={() => void run()}>
          {busy ? "Running…" : "▶ Run method"}
        </button>
      </div>
      {error && <p className="problem small">{error}</p>}
      {result && (
        <div className={`test-result ${result.res.ok ? "ok" : "failed"}`}>
          <div className="mono small muted">{result.call}</div>
          {result.res.ok ? <pre>{show(result.res.vars?.result ?? null)}</pre> : <pre className="problem">{cleanError(result.res.error)}</pre>}
          {log && (
            <>
              <div className="small muted">log</div>
              <pre>{log}</pre>
            </>
          )}
        </div>
      )}
    </section>
  );
}

/** Strips the throwaway program's step prefix and temp-file positions from an error. */
export function cleanError(e: string | undefined): string {
  return (e ?? "failed")
    .replace(/^runtime: step "call" failed: /, "")
    .replace(/\S*mhl-studio-eval[^\s:]*:\d+:\d+: /g, "");
}

function MethodInspector({ doc, tool, m }: { doc: Doc; tool: Declaration; m: ToolMethod }) {
  const wf = useStore(currentWorkflow);
  return (
    <div className="inspector-body">
      <div className="inspector-kind">
        <button className="link" onClick={() => selectTool(tool.name)}>
          Tool {tool.name}
        </button>{" "}
        › Method
      </div>
      <SignatureEditor doc={doc} tool={tool} m={m} />
      <BodyEditor doc={doc} tool={tool} m={m} />
      <TestPanel key={JSON.stringify(m.args)} tool={tool} m={m} />
      <div className="button-row">
        {wf && !m.internal && (
          <button
            className="btn small"
            title={`Add a step to ${wf.name} that calls this method (or drag the method from the Tools list onto the diagram)`}
            onClick={() => void edit((d) => insertState(d, wf.name, "tool", { end: true }, { tool: tool.name, method: m }))}
          >
            + Step calling it
          </button>
        )}
        <button className="btn small" onClick={() => revealSpan(m.span)}>
          Show code
        </button>
        <button
          className="btn small danger"
          onClick={() =>
            void (async () => {
              if (!(await confirm(`Delete method ${tool.name}.${m.name}?`))) return;
              if (await edit((d) => deleteMethod(d, tool.name, m.name))) selectTool(tool.name);
            })()
          }
        >
          Delete method
        </button>
      </div>
    </div>
  );
}
