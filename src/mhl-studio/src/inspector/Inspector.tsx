import { useEffect, useMemo, useState } from "react";
import { edit, openFile, revealSpan } from "../app/controller";
import { currentWorkflow, set, useStore } from "../app/store";
import {
  addInput,
  connect,
  deleteNode,
  Doc,
  insertState,
  moveNode,
  removeEdge,
  removeInput,
  renameNode,
  renameWorkflow,
  setDescription,
  setTimeoutClause,
} from "../sync/edits";
import { END, FAIL, type GraphNode, type Workflow } from "../lib/types";
import { ToolInspector } from "./ToolInspector";
import { ChoiceEditor } from "./ChoiceEditor";
import { TimeoutField } from "./TimeoutField";
import { setLoop, setMax } from "../sync/header";
import { addGroupVar, addMerge, removeGroupVar, removeMerge, sharedWrites } from "../sync/merge";

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
  const out = wf.edges.filter((e) => e.from === node.id);
  const tops = wf.nodes.filter((n) => !n.parent);
  // A group transfers through its merge (sync/edits.ts connect).
  const canGoto = !node.parent;
  const isPipeline = wf.kind !== "workflow";
  return (
    <div className="inspector-body">
      <div className="inspector-kind">
        {node.kind === "parallel" ? "Parallel group" : node.parent ? `Branch of ${node.parent}` : node.choice ? "Choice" : "Step"}
        {node.stage ? ` · stage ${node.stage}` : ""}
      </div>
      <Field label="Name" value={node.name} mono onCommit={(v) => void edit((d) => renameNode(d, wf.name, node.id, v.trim()))} />
      <TimeoutField value={node.timeout} onCommit={(v) => void edit((d) => setTimeoutClause(d, wf.name, node.id, v))} />

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

      {node.kind === "parallel" && <GroupVarsSection wf={wf} node={node} doc={doc} />}
      {node.kind === "parallel" && <MergeSection wf={wf} node={node} doc={doc} />}

      {node.choice && !node.parent && <ChoiceEditor wf={wf} node={node} choice={node.choice} />}

      {!node.parent && !node.choice && (
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
  const addIt = () => {
    if (!name.trim()) return;
    void edit((d) => addInput(d, wf.name, name.trim(), type, def)).then((ok) => {
      if (ok) {
        setName("");
        setDef("");
      }
    });
  };
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
      <LoopSection wf={wf} />

      <section>
        <h4>Inputs</h4>
        {wf.param ? (
          <p className="mono small">
            ({wf.param.name}: {wf.param.type}){wf.returns ? `: ${wf.returns}` : ""}
          </p>
        ) : (
          <>
            <table className="io-table">
              <colgroup>
                <col className="io-name" />
                <col className="io-type" />
                <col />
                <col className="io-action" />
              </colgroup>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Type</th>
                  <th>Default</th>
                  <th aria-label="Actions" />
                </tr>
              </thead>
              <tbody>
                {wf.inputs.length === 0 && (
                  <tr>
                    <td colSpan={4} className="muted small">
                      No inputs.
                    </td>
                  </tr>
                )}
                {wf.inputs.map((i) => (
                  <tr key={i.name}>
                    <td className="mono" title={i.name}>
                      {i.name}
                    </td>
                    <td className="mono io-muted" title={i.type}>
                      {i.type}
                    </td>
                    <td className="mono" title={i.default ? i.default : "required: no default"}>
                      {i.default ?? <span className="io-required">required</span>}
                    </td>
                    <td>
                      {i.file ? (
                        <button className="icon-btn" title={`declared in ${i.file} — click to show it`} onClick={() => void revealSpan(i.span, i.file)}>
                          ↗
                        </button>
                      ) : (
                        <button className="icon-btn" title={`Remove input ${i.name}`} onClick={() => void edit((d) => removeInput(d, wf.name, i.name))}>
                          ×
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
              <tfoot>
                <tr>
                  <td>
                    <input placeholder="name" aria-label="New input name" value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === "Enter" && addIt()} className="mono" />
                  </td>
                  <td>
                    <select aria-label="New input type" value={type} onChange={(e) => setType(e.target.value)}>
                      {TYPES.map((t) => (
                        <option key={t}>{t}</option>
                      ))}
                    </select>
                  </td>
                  <td>
                    <input placeholder="optional" aria-label="New input default" value={def} onChange={(e) => setDef(e.target.value)} onKeyDown={(e) => e.key === "Enter" && addIt()} className="mono" />
                  </td>
                  <td>
                    <button className="icon-btn io-add" title="Add input" aria-label="Add input" disabled={!name.trim()} onClick={addIt}>
                      +
                    </button>
                  </td>
                </tr>
              </tfoot>
            </table>
          </>
        )}
      </section>

      {wf.routes.length > 0 && (
        <section>
          <h4>Routes</h4>
          {wf.routes.map((r) => (
            <button key={r.name} className="link-row mono" onClick={() => void revealSpan(r.span, r.file)}>
              {r.entry ? "entry " : ""}route {r.name}({r.params.join(", ")}) · {r.arms.length} arms
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
              <button key={p.name} className="link-row mono" onClick={() => void revealSpan(p.span, p.file)}>
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
  if (node?.file) return <ForeignInspector node={node} />;
  return (
    <fieldset className="inspector" disabled={stale}>
      {node ? <StepInspector key={node.id} wf={wf} node={node} doc={doc} /> : <WorkflowInspector wf={wf} />}
    </fieldset>
  );
}

/** A step another fragment of a merged partial workflow declares: shown, edited in its own file. */
function ForeignInspector({ node }: { node: GraphNode }) {
  const file = node.file!;
  return (
    <div className="inspector-body">
      <div className="inspector-kind">{node.kind} · another fragment</div>
      <h3 className="mono">{node.name}</h3>
      <p className="muted small">
        Declared in <span className="mono">{file}</span>, another fragment of this partial workflow. It is shown here so the whole flow
        reads in one diagram; edit it in its own file.
      </p>
      <div className="button-row">
        <button className="btn small" onClick={() => void openFile(file)}>
          Open file
        </button>
        <button className="btn small" onClick={() => void revealSpan(node.span, file)}>
          Show in code
        </button>
      </div>
    </div>
  );
}

/**
 * A parallel group's merge: what happens to the branches' writes at the
 * barrier. Without one, branches must write different vars; with one, it
 * combines a var several branches wrote (sync/merge.ts).
 */
function MergeSection({ wf, node, doc }: { wf: Workflow; node: GraphNode; doc: Doc }) {
  const shared = useMemo(() => sharedWrites(doc, wf, node), [doc, wf, node]);
  const m = node.merge;
  return (
    <section className="merge-section">
      <h4>Merge</h4>
      {m ? (
        <>
          {m.param ? (
            <p className="muted small">
              Runs once after every branch, in the workflow's scope. Each branch's vars are{" "}
              <span className="mono">
                {m.param}.&lt;Step&gt;.&lt;var&gt;
              </span>
              ; a var several branches wrote must be assigned here.
            </p>
          ) : (
            <p className="muted small">
              Runs once after every branch, in the workflow's scope: it reads the group's vars as <span className="mono">self.&lt;var&gt;</span>.
            </p>
          )}
          {m.calls.length > 0 && (
            <div className="info-row">
              <span>Calls</span>
              <span className="mono">{m.calls.join(", ")}</span>
            </div>
          )}
          <div className="add-row">
            <button className="btn small" onClick={() => revealSpan(m.span)}>
              Show in code
            </button>
            <button className="btn small" onClick={() => void edit((d) => removeMerge(d, wf.name, node.id))}>
              Remove merge
            </button>
          </div>
        </>
      ) : (
        <>
          {shared.length > 0 ? (
            <p className="choice-warn small">
              {shared.map((s) => `${s.name} (${s.by.join(", ")})`).join("; ")}: more than one branch assigns it, which fails the run at
              the barrier. Add a merge to combine them.
            </p>
          ) : (
            <p className="muted small">None: each var a branch writes lands as-is; two branches writing one var is an error.</p>
          )}
          <button className="btn small" onClick={() => void edit((d) => addMerge(d, wf.name, node.id))}>
            + Add merge
          </button>
        </>
      )}
    </section>
  );
}

/**
 * A parallel group's own vars: fresh each time the group starts, visible to
 * its branches and merge, gone after it — the place for what one branch
 * hands the merge (sync/merge.ts addGroupVar).
 */
function GroupVarsSection({ wf, node, doc }: { wf: Workflow; node: GraphNode; doc: Doc }) {
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const add = () => {
    if (!name.trim()) return;
    void edit((d) => addGroupVar(d, wf.name, node.id, name, value)).then((ok) => ok && (setName(""), setValue("")));
  };
  const vars = node.vars ?? [];
  // The initial value is the declaration's own text after `=`.
  const initial = (span: [number, number]) => doc.text(span).replace(/^var\s+\w+\s*=\s*/, "").trim();
  return (
    <section>
      <h4>Group vars</h4>
      <table className="io-table">
        <colgroup>
          <col className="io-name" />
          <col />
          <col className="io-action" />
        </colgroup>
        <thead>
          <tr>
            <th>Name</th>
            <th>Initial value</th>
            <th aria-label="Actions" />
          </tr>
        </thead>
        <tbody>
          {vars.length === 0 && (
            <tr>
              <td colSpan={3} className="muted small io-empty">
                None. A group var lives only while the group runs: give each branch one to write, and read them in the merge.
              </td>
            </tr>
          )}
          {vars.map((v) => {
            const init = initial(v.span);
            return (
              <tr key={v.name}>
                <td className="mono" title={v.name}>
                  {v.name}
                </td>
                <td className="mono" title={init}>
                  {init}
                </td>
                <td>
                  <button className="icon-btn" title={`Remove var ${v.name}`} onClick={() => void edit((d) => removeGroupVar(d, wf.name, node.id, v.name))}>
                    ×
                  </button>
                </td>
              </tr>
            );
          })}
        </tbody>
        <tfoot>
          <tr>
            <td>
              <input className="mono" placeholder="name" aria-label="New group var name" value={name} spellCheck={false} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === "Enter" && add()} />
            </td>
            <td>
              <input className="mono" placeholder='""' aria-label="New group var initial value" value={value} spellCheck={false} onChange={(e) => setValue(e.target.value)} onKeyDown={(e) => e.key === "Enter" && add()} />
            </td>
            <td>
              <button className="icon-btn io-add" title="Add group var" aria-label="Add group var" disabled={!name.trim()} onClick={add}>
                +
              </button>
            </td>
          </tr>
        </tfoot>
      </table>
    </section>
  );
}

/**
 * `loop workflow X max N`: whether the steps run again after the last one,
 * and the iteration ceiling (sync/header.ts). A loop with no max ends only
 * through `repeat { stop_when }`, a `break` or `complete()`.
 */
function LoopSection({ wf }: { wf: Workflow }) {
  const [max, setMaxText] = useState(wf.max ?? "");
  useEffect(() => setMaxText(wf.max ?? ""), [wf.max]);
  const hasRepeat = wf.props.some((p) => p.name === "repeat");
  const commit = () => max !== (wf.max ?? "") && void edit((d) => setMax(d, wf.name, max));
  return (
    <section className="loop-section">
      <label className="toggle-row">
        <input type="checkbox" checked={!!wf.loop} onChange={(e) => void edit((d) => setLoop(d, wf.name, e.target.checked))} />
        <span>
          <b>Loop</b>
          <span className="muted small"> — after the last step, run the steps again</span>
        </span>
      </label>
      {wf.loop && (
        <>
          <label className="field">
            <span>Max iterations</span>
            <input
              className="mono loop-max"
              inputMode="numeric"
              placeholder="no limit"
              value={max}
              onChange={(e) => setMaxText(e.target.value.replace(/\D/g, "").replace(/^0+/, ""))}
              onBlur={commit}
              onKeyDown={(e) => e.key === "Enter" && commit()}
            />
          </label>
          {!wf.max && !hasRepeat && (
            <p className="choice-warn small">No max and no repeat {"{ stop_when }"}: the loop only ends at a break or complete().</p>
          )}
          {hasRepeat && <p className="muted small">This {wf.kind} also has a repeat {"{ … }"} block; set max here or its max_iterations, not both.</p>}
        </>
      )}
    </section>
  );
}
