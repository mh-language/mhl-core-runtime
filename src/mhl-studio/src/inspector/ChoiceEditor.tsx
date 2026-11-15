// A Choice step (`goto match subject { arms }`) as a Step Functions-style
// rule list: what it branches on, one row per rule (value → step), and the
// default. Each control rewrites one span of the statement (sync/choice.ts).
import { useEffect, useMemo, useState } from "react";
import { edit, importedDeclarations } from "../app/controller";
import { set, useStore } from "../app/store";
import { addArm, choiceValues, nextPattern, removeArm, setArmPattern, setArmTarget, setSubject, subjectPatterns } from "../sync/choice";
import { FAIL, type Choice, type GraphNode, type Workflow } from "../lib/types";

/** An input that commits on Enter or blur. */
function Inline(props: { value: string; list?: string; placeholder?: string; onCommit: (v: string) => void; className?: string }) {
  const [v, setV] = useState(props.value);
  useEffect(() => setV(props.value), [props.value]);
  const commit = () => v.trim() !== props.value && props.onCommit(v);
  return (
    <input
      className={`mono ${props.className ?? ""}`}
      value={v}
      list={props.list}
      placeholder={props.placeholder}
      spellCheck={false}
      onChange={(e) => setV(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => e.key === "Enter" && commit()}
    />
  );
}

function Targets({ value, tops, onChange }: { value: string; tops: string[]; onChange: (to: string) => void }) {
  return (
    <select value={value} onChange={(e) => onChange(e.target.value)}>
      {!tops.includes(value) && value !== FAIL && <option value={value}>{value}</option>}
      {tops.map((t) => (
        <option key={t} value={t}>
          {t}
        </option>
      ))}
      <option value={FAIL}>Fail — fail(…)</option>
    </select>
  );
}

/** A constant subject: the same rule wins on every run. */
const CONSTANT = /^("([^"\\]|\\.)*"|-?\d+(\.\d+)?|true|false|null)$/;

export function ChoiceEditor({ wf, node, choice }: { wf: Workflow; node: GraphNode; choice: Choice }) {
  const graph = useStore((s) => s.graph);
  const decls = useMemo(() => (graph ? [...graph.declarations, ...importedDeclarations(graph)] : []), [graph]);
  const values = choiceValues(wf);
  const patterns = subjectPatterns(decls, wf, choice.subject);
  const tops = wf.nodes.filter((n) => !n.parent).map((n) => n.id);
  const rules = choice.arms.map((a, i) => ({ a, i })).filter(({ a }) => !a.wildcard);
  const def = choice.arms.findIndex((a) => a.wildcard);
  const missing = patterns?.filter((p) => !choice.arms.some((a) => a.pattern === p)) ?? [];
  const firstForward = tops[tops.indexOf(node.id) + 1] ?? tops.find((t) => t !== node.id) ?? FAIL;
  const [to, setTo] = useState(firstForward);
  const suggestion = nextPattern(decls, wf, choice, to);
  const [pattern, setPattern] = useState("");
  const listId = `choice-values-${node.id}`;
  const patId = `choice-patterns-${node.id}`;
  const run = (f: Parameters<typeof edit>[0]) => void edit(f);
  const add = () => {
    const p = pattern.trim() || suggestion;
    void edit((d) => addArm(d, wf.name, node.id, p, to)).then((ok) => ok && setPattern(""));
  };

  return (
    <section className="choice-editor">
      <h4>Choice</h4>
      <label className="field">
        <span>Branch on</span>
        <Inline value={choice.subject} list={listId} placeholder="an input or a var" onCommit={(v) => run((d) => setSubject(d, wf.name, node.id, v))} />
        <datalist id={listId}>
          {values.map((v) => (
            <option key={v.expr} value={v.expr}>
              {v.source === "input" ? `input${v.type ? `: ${v.type}` : ""}` : "workflow var"}
            </option>
          ))}
        </datalist>
      </label>
      {CONSTANT.test(choice.subject) && (
        <p className="choice-warn small">
          {choice.subject} is a constant, so every run takes the same rule.{" "}
          {values.length ? (
            <>
              Branch on{" "}
              {values.slice(0, 3).map((v, k) => (
                <span key={v.expr}>
                  {k > 0 && ", "}
                  <button className="link mono" onClick={() => run((d) => setSubject(d, wf.name, node.id, v.expr))}>
                    {v.expr}
                  </button>
                </span>
              ))}
              {values.length > 3 ? ", …" : ""} instead.
            </>
          ) : (
            "Add an input to the workflow, or a var an earlier step sets (self.x = ...), and branch on it."
          )}
        </p>
      )}
      {patterns && <datalist id={patId}>{patterns.map((p) => <option key={p} value={p} />)}</datalist>}

      <div className="choice-rules">
        {rules.map(({ a, i }) => (
          <div key={i} className="choice-rule">
            <Inline value={a.pattern} list={patterns ? patId : undefined} onCommit={(v) => run((d) => setArmPattern(d, wf.name, node.id, i, v))} />
            <span className="choice-arrow">→</span>
            <Targets value={a.fail ? FAIL : (a.target ?? FAIL)} tops={tops} onChange={(t) => run((d) => setArmTarget(d, wf.name, node.id, i, t))} />
            {a.target && (
              <button className="icon-btn" title={`Select ${a.target}`} onClick={() => set({ selection: a.target! })}>
                ↗
              </button>
            )}
            <button className="icon-btn" title="Remove this rule" onClick={() => run((d) => removeArm(d, wf.name, node.id, i))}>
              ×
            </button>
          </div>
        ))}
        <div className="choice-rule default">
          <span className="choice-otherwise">otherwise</span>
          <span className="choice-arrow">→</span>
          {def >= 0 ? (
            <>
              <Targets
                value={choice.arms[def].fail ? FAIL : (choice.arms[def].target ?? FAIL)}
                tops={tops}
                onChange={(t) => run((d) => setArmTarget(d, wf.name, node.id, def, t))}
              />
              <button className="icon-btn" title="Remove the default rule" onClick={() => run((d) => removeArm(d, wf.name, node.id, def))}>
                ×
              </button>
            </>
          ) : (
            <button className="btn small" title="Add `_ -> fail(…)`" onClick={() => run((d) => addArm(d, wf.name, node.id, "_", FAIL))}>
              + Default
            </button>
          )}
        </div>
        {def < 0 && <p className="muted small">No default: a value no rule matches fails the run.</p>}
      </div>

      {missing.length > 0 && (
        <p className="choice-warn small">
          Not covered: {missing.join(", ")} — {def >= 0 ? "they take the default." : "they fail the run."}{" "}
          <button className="link" onClick={() => setPattern(missing[0])}>
            Add a rule for {missing[0]}
          </button>
        </p>
      )}

      <div className="choice-add">
        <input
          className="mono"
          value={pattern}
          list={patterns ? patId : undefined}
          placeholder={suggestion}
          spellCheck={false}
          onChange={(e) => setPattern(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && add()}
        />
        <span className="choice-arrow">→</span>
        <Targets value={to} tops={tops} onChange={setTo} />
        <button className="btn small" onClick={add}>
          + Rule
        </button>
      </div>
    </section>
  );
}
