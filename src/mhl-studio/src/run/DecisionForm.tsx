import { useMemo, useState } from "react";
import type { Awaiting } from "../lib/types";
import { buildDecision, decisionFields, initialValue } from "./decision";

/**
 * The form a run paused at a gate (`pause(..., decision: T)`) is resumed from: one field per part of T.
 * `submitLabel` names the button (the run panel's "Resume with decision", the MCP bench's run/resume).
 */
export function DecisionForm({
  awaiting,
  reason,
  onSubmit,
  submitLabel = "Resume with decision",
}: {
  awaiting: Pick<Awaiting, "step" | "type" | "schema">;
  reason: string;
  onSubmit: (decision: unknown) => void;
  submitLabel?: string;
}) {
  const fields = useMemo(() => decisionFields(awaiting.schema), [awaiting.schema]);
  const [values, setValues] = useState<Record<string, string>>(() => Object.fromEntries(fields.map((f) => [f.name, initialValue(f)])));
  const [error, setError] = useState<string | null>(null);
  const setField = (name: string, v: string) => setValues((s) => ({ ...s, [name]: v }));
  const submit = () => {
    try {
      onSubmit(buildDecision(fields, values));
      setError(null);
    } catch (e) {
      setError((e as Error).message);
    }
  };
  return (
    <div className="approval decision-form">
      <h4>
        Waiting for a decision <span className="mono muted">· {awaiting.type}</span>
      </h4>
      <p>{reason || `The run paused at ${awaiting.step}.`}</p>
      {fields.map((f) => (
        <label key={f.name || "value"} className="decision-field">
          <span className="mono">
            {f.name || awaiting.type}
            {f.required ? "" : "?"}
          </span>
          {f.kind === "enum" ? (
            <select value={values[f.name]} onChange={(e) => setField(f.name, e.target.value)}>
              {f.options?.map((o) => (
                <option key={o}>{o}</option>
              ))}
            </select>
          ) : f.kind === "bool" ? (
            <input type="checkbox" checked={values[f.name] === "true"} onChange={(e) => setField(f.name, String(e.target.checked))} />
          ) : (
            <input
              className="mono"
              type={f.kind === "number" ? "number" : "text"}
              placeholder={f.kind === "json" ? "JSON" : f.required ? "required" : "optional"}
              value={values[f.name]}
              onChange={(e) => setField(f.name, e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && submit()}
            />
          )}
        </label>
      ))}
      {error && <p className="problem small">{error}</p>}
      <p className="muted small">The step resumes with this decision — it answers this gate only; the next gate the run reaches asks again.</p>
      <button className="btn small primary" onClick={submit}>
        {submitLabel}
      </button>
    </div>
  );
}
