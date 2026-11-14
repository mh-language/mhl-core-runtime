import { useEffect, useMemo, useState } from "react";
import { edit, revealSpan } from "../app/controller";
import { currentWorkflow, set, useStore } from "../app/store";
import {
  addInput,
  connect,
  deleteNode,
  Doc,
  getBody,
  insertState,
  moveNode,
  removeEdge,
  removeInput,
  renameNode,
  renameWorkflow,
  setBody,
  setDescription,
  setTimeoutClause,
} from "../sync/edits";
import { END, FAIL, type GraphNode, type Workflow } from "../lib/types";
import { ToolInspector } from "./ToolInspector";

/** A text field that commits on Enter or blur, and resets when `value` changes. */
export function Field(props: {
  label: string;
  value: string;
  placeholder?: string;
  mono?: boolean;
  multiline?: boolean;
  disabled?: boolean;
  onCommit: (v: string) => void;
}) {
  const [v, setV] = useState(props.value);
  useEffect(() => setV(props.value), [props.value]);
  const commit = () => {
    if (v !== props.value) props.onCommit(v);
  };
  const common = {
    value: v,
    placeholder: props.placeholder,
    disabled: props.disabled,
    className: props.mono ? "mono" : undefined,
    onChange: (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setV(e.target.value),
    onBlur: commit,
  };
  return (
    <label className="field">
      <span>{props.label}</span>
      {props.multiline ? (
        <textarea
          {...common}
          rows={Math.min(18, Math.max(3, v.split("\n").length + 1))}
          spellCheck={false}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) commit();
          }}
        />
      ) : (
        <input {...common} spellCheck={false} onKeyDown={(e) => e.key === "Enter" && commit()} />
      )}
    </label>
  );
}

const EDGE_KIND: Record<string, string> = {
  next: "next",
  goto: "goto",
  match: "match",
  route: "route",
  end: "end",
  fail: "fail",
};

function targetLabel(id: string) {
  return id === END ? "End" : id === FAIL ? "Fail" : id;
}

function StepInspector({ wf, node, doc }: { wf: Workflow; node: GraphNode; doc: Doc }) {
  const [target, setTarget] = useState("");
  const body = useMemo(() => (node.kind === "step" ? getBody(doc, wf.name, node.id) : ""), [doc, wf.name, node]);
  const out = wf.edges.filter((e) => e.from === node.id);
  const tops = wf.nodes.filter((n) => !n.parent);
  const canGoto = node.kind === "step" && !node.parent;
  const isPipeline = wf.kind !== "workflow";
  return (
    <div className="inspector-body">
      <div className="inspector-kind">{node.kind === "parallel" ? "Parallel group" : node.parent ? `Branch of ${node.parent}` : "Step"}</div>
      <Field label="Name" value={node.name} mono onCommit={(v) => void edit((d) => renameNode(d, wf.name, node.id, v.trim()))} />
      <Field
        label="Timeout"
        value={node.timeout ?? ""}
        placeholder="e.g. 30s, 5m, 2h (empty = none)"
        mono
        onCommit={(v) => void edit((d) => setTimeoutClause(d, wf.name, node.id, v))}
      />

      {node.calls.length > 0 && (
        <div className="info-row">
          <span>Calls</span>
          <span className="mono">{node.calls.join(", ")}</span>
        </div>
      )}

      {node.kind === "parallel" && (
        <section>
          <h4>Branches</h4>
          {(node.children ?? []).map((c) => (
            <button key={c} className="link-row" onClick={() => set({ selection: c })}>
              ▸ {c}
            </button>
          ))}
          <button className="btn small" onClick={() => void edit((d) => insertState(d, wf.name, "pass", { into: node.id }))}>
            + Add branch
          </button>
        </section>
      )}

      {!node.parent && (
        <section>
          <h4>Transitions</h4>
          {out.length === 0 && <p className="muted small">None — this step ends the run.</p>}
          {out.map((e, i) => (
            <div key={i} className="transition">
              <span className={`pill kind-${e.kind}`}>{EDGE_KIND[e.kind]}</span>
              <span className="mono">{e.label ? `${e.label} → ` : ""}</span>
              <button className="link" onClick={() => e.to !== END && e.to !== FAIL && set({ selection: e.to })}>
                {targetLabel(e.to)}
              </button>
              <button className="icon-btn" title="Remove this transition" onClick={() => void edit((d) => removeEdge(d, wf.name, e))}>
                ×
              </button>
            </div>
          ))}
          {canGoto && (
            <div className="add-row">
              <select value={target} onChange={(e) => setTarget(e.target.value)}>
                <option value="">Add transition to…</option>
                {!isPipeline && tops.map((n) => <option key={n.id} value={n.id}>goto {n.id}</option>)}
                <option value={END}>End — complete()</option>
                <option value={FAIL}>Fail — fail(…)</option>
              </select>
              <button
                className="btn small"
                disabled={!target}
                onClick={() => {
                  void edit((d) => connect(d, wf.name, node.id, target));
                  setTarget("");
                }}
              >
                Add
              </button>
            </div>
          )}
        </section>
      )}

      {node.kind === "step" && (
        <Field label="Body" value={body} multiline mono onCommit={(v) => void edit((d) => setBody(d, wf.name, node.id, v))} />
      )}

      <div className="button-row">
        <button className="btn small" title="Move earlier" onClick={() => void edit((d) => moveNode(d, wf.name, node.id, -1))}>
          ↑ Up
        </button>
        <button className="btn small" title="Move later" onClick={() => void edit((d) => moveNode(d, wf.name, node.id, 1))}>
          ↓ Down
        </button>
        <button className="btn small" onClick={() => revealSpan(node.span)}>
          Show code
        </button>
        <button
          className="btn small danger"
          onClick={() => void edit((d) => deleteNode(d, wf.name, node.id)).then((ok) => ok && set({ selection: null }))}
        >
          Delete
        </button>
      </div>
    </div>
  );
}

const TYPES = ["string", "number", "bool", "object", "string[]", "number[]"];

function WorkflowInspector({ wf }: { wf: Workflow }) {
  const [name, setName] = useState("");
  const [type, setType] = useState("string");
  const [def, setDef] = useState("");
  const diagnostics = useStore((s) => s.diagnostics);
  return (
    <div className="inspector-body">
      <div className="inspector-kind">
        {wf.loop ? "loop " : ""}
        {wf.kind}
        {wf.partial ? " (partial)" : ""}
        {wf.max ? ` · max ${wf.max}` : ""}
      </div>
      <Field label="Name" value={wf.name} mono onCommit={(v) => void edit((d) => renameWorkflow(d, wf.name, v.trim())).then((ok) => ok && set({ workflow: v.trim() }))} />
      <Field label="Description" value={wf.description ?? ""} multiline onCommit={(v) => void edit((d) => setDescription(d, wf.name, v.trim()))} />

      <section>
        <h4>Inputs</h4>
        {wf.param ? (
          <p className="mono small">
            ({wf.param.name}: {wf.param.type}){wf.returns ? `: ${wf.returns}` : ""}
          </p>
        ) : (
          <>
            {wf.inputs.length === 0 && <p className="muted small">No inputs.</p>}
            {wf.inputs.map((i) => (
              <div key={i.name} className="transition">
                <span className="mono">
                  {i.name}: {i.type}
                  {i.default ? ` = ${i.default}` : ""}
                </span>
                <button className="icon-btn" title="Remove input" onClick={() => void edit((d) => removeInput(d, wf.name, i.name))}>
                  ×
                </button>
              </div>
            ))}
            <div className="add-input">
              <input placeholder="name" value={name} onChange={(e) => setName(e.target.value)} className="mono" />
              <select value={type} onChange={(e) => setType(e.target.value)}>
                {TYPES.map((t) => (
                  <option key={t}>{t}</option>
                ))}
              </select>
              <input placeholder='default (e.g. "x")' value={def} onChange={(e) => setDef(e.target.value)} className="mono" />
              <button
                className="btn small"
                disabled={!name.trim()}
                onClick={() =>
                  void edit((d) => addInput(d, wf.name, name.trim(), type, def)).then((ok) => {
                    if (ok) {
                      setName("");
                      setDef("");
                    }
                  })
                }
              >
                Add
              </button>
            </div>
          </>
        )}
      </section>

      {wf.routes.length > 0 && (
        <section>
          <h4>Routes</h4>
          {wf.routes.map((r) => (
            <button key={r.name} className="link-row mono" onClick={() => revealSpan(r.span)}>
              route {r.name}({r.params.join(", ")}) · {r.arms.length} arms
            </button>
          ))}
        </section>
      )}

      {wf.props.some((p) => p.name !== "description") && (
        <section>
          <h4>Properties</h4>
          {wf.props
            .filter((p) => p.name !== "description")
            .map((p) => (
              <button key={p.name} className="link-row mono" onClick={() => revealSpan(p.span)}>
                {p.name}: …
              </button>
            ))}
        </section>
      )}

      <section>
        <h4>Problems</h4>
        {diagnostics.length === 0 ? (
          <p className="muted small">No problems.</p>
        ) : (
          diagnostics.map((d, i) => (
            <p key={i} className="problem small">
              {d.file ? `${d.file}:` : ""}
              {d.line}:{d.column} {d.message}
            </p>
          ))
        )}
      </section>
      <p className="muted small">Select a step on the diagram to edit it. Drag states from the bar above the diagram onto it.</p>
    </div>
  );
}

export function Inspector() {
  const wf = useStore(currentWorkflow);
  const graph = useStore((s) => s.graph);
  const graphSource = useStore((s) => s.graphSource);
  const selection = useStore((s) => s.selection);
  const stale = useStore((s) => s.stale);
  const tool = useStore((s) => s.tool);
  const doc = useMemo(() => (graph ? new Doc(graphSource, graph) : null), [graph, graphSource]);
  if (doc && tool) {
    return (
      <fieldset className="inspector" disabled={stale}>
        <ToolInspector doc={doc} name={tool.name} method={tool.method} />
      </fieldset>
    );
  }
  if (!wf || !doc) return <div className="inspector-body muted">Nothing selected. Pick a tool on the left to edit it.</div>;
  const node = wf.nodes.find((n) => n.id === selection);
  return (
    <fieldset className="inspector" disabled={stale}>
      {node ? <StepInspector key={node.id} wf={wf} node={node} doc={doc} /> : <WorkflowInspector wf={wf} />}
    </fieldset>
  );
}
