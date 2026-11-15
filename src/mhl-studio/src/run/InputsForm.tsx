// The run inputs form: one field per workflow input (from the dry-run, else
// the graph), Validate (a dry-run with these values) and its problems. The
// values live in store.runInputs, so Run and Resume use them wherever they
// are clicked. Shown in the popover under the Run button's ▾.
import { useMemo } from "react";
import { inspect } from "../app/controller";
import { inputFields, runInputPairs, set, useStore, type State } from "../app/store";

/**
 * Required inputs still left blank: a run would be refused. The dry-run is
 * the authority (it knows a typed signature's `name?: T` is optional); its
 * missing_inputs lists what was unsupplied when it ran, so a name filled in
 * since then no longer counts. Before any dry-run, an input with no default.
 */
export function missingInputs(s: State): string[] {
  const blank = (name: string) => !(s.runInputs[name] ?? "").trim();
  if (s.inspection) return (s.inspection.missing_inputs ?? []).filter(blank);
  return inputFields(s)
    .filter((f) => f.def === undefined && f.type !== "bool" && blank(f.name))
    .map((f) => f.name);
}

export function InputsForm({ autoFocus }: { autoFocus?: boolean }) {
  const graph = useStore((s) => s.graph);
  const inspection = useStore((s) => s.inspection);
  const workflow = useStore((s) => s.workflow);
  const values = useStore((s) => s.runInputs);
  const required = useStore((s) => s.inspection?.missing_inputs);
  const setValues = (v: Record<string, string>) => set({ runInputs: v });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const fields = useMemo(() => inputFields(useStore.getState()), [inspection, graph, workflow]);

  return (
    <div className="inputs-form">
      <h4>
        Inputs {inspection?.pipeline ? <span className="muted small">for {inspection.pipeline}</span> : null}
      </h4>
      {fields.length === 0 && <p className="muted small">This workflow takes no inputs.</p>}
      {fields.map((f, i) => (
        <label key={f.name} className="field inline">
          <span className="mono">
            {f.name}: <span className="muted">{f.type}</span>
          </span>
          {f.type === "bool" ? (
            <select autoFocus={autoFocus && i === 0} value={values[f.name] ?? ""} onChange={(e) => setValues({ ...values, [f.name]: e.target.value })}>
              <option value="">{f.def ? `default (${f.def})` : "—"}</option>
              <option value="true">true</option>
              <option value="false">false</option>
            </select>
          ) : (
            <input
              autoFocus={autoFocus && i === 0}
              className="mono"
              placeholder={f.def ? `default ${f.def}` : !inspection || required?.includes(f.name) ? "required" : "optional"}
              value={values[f.name] ?? ""}
              onChange={(e) => setValues({ ...values, [f.name]: e.target.value })}
            />
          )}
        </label>
      ))}
      {fields.length > 0 && (
        <div className="button-row">
          <button className="btn small" onClick={() => void inspect(runInputPairs())} title="Check the inputs with a dry run (nothing executes)">
            Validate
          </button>
          {Object.values(values).some((v) => v !== "") && (
            <button className="btn small" onClick={() => setValues({})} title="Clear every value (defaults apply)">
              Reset
            </button>
          )}
        </div>
      )}
      {inspection && !inspection.ok && (
        <div className="problems">
          {inspection.error && <p className="problem small">{inspection.error}</p>}
          {inspection.missing_inputs?.map((m) => <p key={m} className="problem small">missing input: {m}</p>)}
          {inspection.invalid_inputs?.map((m) => <p key={m} className="problem small">{m}</p>)}
          {inspection.unknown_inputs?.map((m) => <p key={m} className="problem small">unknown input: {m}</p>)}
        </div>
      )}
      {inspection?.warnings?.map((w) => <p key={w} className="problem small">{w}</p>)}
    </div>
  );
}
