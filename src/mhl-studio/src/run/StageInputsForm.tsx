// The form for a run paused on entering a stage whose inputs were not given
// (`awaiting_inputs`): only that stage's missing inputs, and "Continue",
// which resumes the run passing just those as `--input`s.
import { useMemo } from "react";
import { startRun } from "../app/controller";
import { inputFields, stageInputPairs, useStore } from "../app/store";
import type { AwaitingInputs } from "../lib/types";
import { InputControl, useInputDecls } from "./InputsForm";

export function StageInputsForm({ awaiting }: { awaiting: AwaitingInputs }) {
  const inspection = useStore((s) => s.inspection);
  const graph = useStore((s) => s.graph);
  const fields = useMemo(
    () => inputFields(useStore.getState()).filter((f) => awaiting.inputs.includes(f.name)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [inspection, graph, awaiting],
  );
  const values = useStore((s) => s.runInputs);
  const decls = useInputDecls();
  const blank = awaiting.inputs.filter((n) => !(values[n] ?? "").trim());
  const submit = () => {
    if (!blank.length) void startRun(stageInputPairs(awaiting.stage, awaiting.inputs), { resume: true });
  };
  return (
    <div className="approval">
      <h4>
        Stage <span className="mono">{awaiting.stage}</span> needs its inputs
      </h4>
      <p className="muted small">
        The run stopped before {awaiting.step}, on entering the stage. Give it {awaiting.inputs.length === 1 ? "this input" : "these inputs"} to continue — only{" "}
        {awaiting.inputs.length === 1 ? "it is" : "they are"} sent.
      </p>
      <div
        className="inputs-form"
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.target as HTMLElement).tagName === "INPUT") submit();
        }}
      >
        {fields.map((f, i) => (
          <InputControl key={f.name} field={f} decls={decls} required autoFocus={i === 0} />
        ))}
      </div>
      <button className="btn small primary" disabled={!!blank.length} onClick={submit} title={`mhl run --resume ${awaiting.inputs.map((n) => `--input ${n}=…`).join(" ")}`}>
        Continue stage {awaiting.stage}
      </button>
    </div>
  );
}
