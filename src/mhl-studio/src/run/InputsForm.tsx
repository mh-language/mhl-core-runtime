// The run inputs form: one field per workflow input (from the dry-run, else
// the graph) — a stage's inputs grouped under it, optional at the start —
// Validate (a dry-run with these values) and its problems. The
// values live in store.runInputs, so Run and Resume use them wherever they
// are clicked. Shown in the popover under the Run button's ▾.
import { useMemo } from "react";
import { importedDeclarations, inspect } from "../app/controller";
import { enumVariants, inputFields, runInputPairs, set, useStore, type InputField, type State } from "../app/store";
import type { Declaration } from "../lib/types";
import { inputProblem } from "../lib/inputs";

/**
 * Inputs whose typed value the run would refuse (lib/inputs.ts inputProblem):
 * a list given as plain text, a number that isn't one — caught here, where
 * the field can say what it wants, rather than as the run's error.
 */
export function invalidInputs(s: State, names?: string[]): string[] {
  return inputFields(s)
    .filter((f) => (!names || names.includes(f.name)) && inputProblem(f.type, s.runInputs[f.name] ?? ""))
    .map((f) => f.name);
}

/**
 * Required inputs still left blank: a run would be refused. The dry-run is
 * the authority (it knows a typed signature's `name?: T` is optional); its
 * missing_inputs lists what was unsupplied when it ran, so a name filled in
 * since then no longer counts. Before any dry-run, an input with no default.
 */
export function missingInputs(s: State): string[] {
  const blank = (name: string) => !(s.runInputs[name] ?? "").trim();
  if (s.inspection) return (s.inspection.missing_inputs ?? []).filter(blank);
  // A stage input is asked for on entering its stage: never missing at the start.
  return inputFields(s)
    .filter((f) => !f.stage && f.def === undefined && f.type !== "bool" && blank(f.name))
    .map((f) => f.name);
}

/**
 * The enums an input can be typed with: this file's and what it imports
 * first, then any other workspace file's — a partial workflow's input may be
 * declared in another fragment, which imports its enum itself.
 */
export function useInputDecls(): Declaration[] {
  const graph = useStore((s) => s.graph);
  const library = useStore((s) => s.library);
  return useMemo(
    () => [...importedDeclarations(graph), ...(graph?.declarations ?? []), ...library.flatMap((l) => l.declarations.filter((d) => d.kind === "enum"))],
    [graph, library],
  );
}

/**
 * One input's control — an enum's variants, a bool's true/false, else a text
 * field — bound to store.runInputs. `required` words the empty choice.
 */
export function InputControl({ field: f, decls, required, autoFocus }: { field: InputField; decls: Declaration[]; required: boolean; autoFocus?: boolean }) {
  const values = useStore((s) => s.runInputs);
  const setValue = (v: string) => set({ runInputs: { ...values, [f.name]: v } });
  const variants = enumVariants(f.type, decls);
  const value = values[f.name] ?? "";
  const problem = variants ? null : inputProblem(f.type, value);
  const list = f.type.replace(/\?$/, "").endsWith("[]");
  const control = (
    <label className="field inline" title={f.doc}>
      <span className="mono">
        {f.name}: <span className="muted">{f.type}</span>
      </span>
      {variants ? (
        <select className="mono" autoFocus={autoFocus} value={value} onChange={(e) => setValue(e.target.value)}>
          {/* The default as a variant: `Mode.Existing` in the source is `Existing` here, like the options. */}
          <option value="">{f.def ? `default (${f.def.replace(`${f.type.replace(/\?$/, "")}.`, "")})` : required ? "— required —" : "—"}</option>
          {variants.map((v) => (
            <option key={v} value={v}>
              {v}
            </option>
          ))}
          {/* A value typed before the enum was known (or no longer a variant) stays visible, to fix by hand. */}
          {value && !variants.includes(value) && <option value={value}>{value} (not a variant)</option>}
        </select>
      ) : f.type === "bool" ? (
        <select autoFocus={autoFocus} value={value} onChange={(e) => setValue(e.target.value)}>
          <option value="">{f.def ? `default (${f.def})` : "—"}</option>
          <option value="true">true</option>
          <option value="false">false</option>
        </select>
      ) : (
        <input
          autoFocus={autoFocus}
          className={`mono ${problem ? "invalid" : ""}`}
          aria-invalid={!!problem}
          placeholder={f.def ? `default ${f.def}` : list ? `JSON list${required ? ", required" : ""}, e.g. ["a", "b"]` : required ? "required" : "optional"}
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
      )}
    </label>
  );
  // What the value must look like, when it doesn't — with a one-click fix when one is likely.
  const problemRow = problem && (
    <p className="field-doc field-problem small">
      Must be {problem.message}.
      {problem.fix && (
        <>
          {" "}
          <button className="link mono" onClick={() => setValue(problem.fix!)}>
            Use {problem.fix}
          </button>
        </>
      )}
    </p>
  );
  // The input's `///` doc, under its field.
  return f.doc || problemRow ? (
    <div className="field-with-doc">
      {control}
      {problemRow}
      {f.doc && <p className="field-doc muted small">{f.doc}</p>}
    </div>
  ) : (
    control
  );
}

export function InputsForm({ autoFocus }: { autoFocus?: boolean }) {
  const graph = useStore((s) => s.graph);
  const inspection = useStore((s) => s.inspection);
  const workflow = useStore((s) => s.workflow);
  const values = useStore((s) => s.runInputs);
  const required = useStore((s) => s.inspection?.missing_inputs);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const fields = useMemo(() => inputFields(useStore.getState()), [inspection, graph, workflow]);
  const decls = useInputDecls();
  // The workflow's own inputs first, then each stage's — asked for when the run enters it.
  const own = fields.filter((f) => !f.stage);
  const stages = [...new Set(fields.filter((f) => f.stage).map((f) => f.stage as string))];
  const control = (f: InputField, i: number, stage: boolean) => (
    <InputControl
      key={f.name}
      field={f}
      decls={decls}
      autoFocus={autoFocus && i === 0}
      required={!stage && (!inspection || !!required?.includes(f.name))}
    />
  );

  return (
    <div className="inputs-form">
      <h4>
        Inputs {inspection?.pipeline ? <span className="muted small">for {inspection.pipeline}</span> : null}
      </h4>
      {fields.length === 0 && <p className="muted small">This workflow takes no inputs.</p>}
      {own.map((f, i) => control(f, i, false))}
      {stages.length > 0 && (
        <>
          <p className="muted small stage-inputs-intro">
            Later in the run — each stage asks for its own inputs when the run reaches it. Open one to give them now instead.
          </p>
          {stages.map((st) => {
            const mine = fields.filter((f) => f.stage === st);
            const waits = mine.filter((f) => f.def === undefined).map((f) => f.name);
            const filled = mine.some((f) => (values[f.name] ?? "") !== "");
            return (
              <details key={st} className="stage-inputs" open={filled || undefined}>
                <summary className="small">
                  Stage <span className="mono">{st}</span>{" "}
                  <span className="muted">
                    · {filled ? "given now — the run won't stop there" : waits.length ? `will ask for ${waits.join(", ")}` : "all have defaults"}
                  </span>
                </summary>
                {mine.map((f) => control(f, own.length ? 1 : 0, true))}
              </details>
            );
          })}
        </>
      )}
      {fields.length > 0 && (
        <div className="button-row">
          <button className="btn small" onClick={() => void inspect(runInputPairs())} title="Check the inputs with a dry run (nothing executes)">
            Validate
          </button>
          {Object.values(values).some((v) => v !== "") && (
            <button className="btn small" onClick={() => set({ runInputs: {} })} title="Clear every value (defaults apply)">
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
