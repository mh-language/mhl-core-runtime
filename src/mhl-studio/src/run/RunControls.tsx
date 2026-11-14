// Run / Resume / Cancel for the open workflow, in the top toolbar. Run is a
// split button: its ▾ opens the inputs popover (store.inputsOpen), whose
// values (store.runInputs) both Run and Resume use.
import { useEffect, useRef, useState } from "react";
import { cancelRun, startRun } from "../app/controller";
import { inputFields, runInputPairs, set, useStore } from "../app/store";
import { InputsForm, missingInputs } from "./InputsForm";

function run(resume = false) {
  set({ runTab: "execution", inputsOpen: false });
  void startRun(runInputPairs(), resume ? { resume: true } : {});
}

/** The inputs popover, anchored under the split button's right edge. */
function InputsPopover({ anchor, resumable }: { anchor: HTMLElement; resumable: boolean }) {
  const [at, setAt] = useState(() => place(anchor));
  const missing = useStore((s) => missingInputs(s).length);
  const workflow = useStore((s) => s.workflow);
  useEffect(() => {
    const close = (e: KeyboardEvent) => e.key === "Escape" && set({ inputsOpen: false });
    const move = () => setAt(place(anchor));
    window.addEventListener("keydown", close);
    window.addEventListener("resize", move);
    return () => {
      window.removeEventListener("keydown", close);
      window.removeEventListener("resize", move);
    };
  }, [anchor]);
  return (
    <>
      <div className="menu-backdrop" onClick={() => set({ inputsOpen: false })} />
      <div
        className="inputs-popover"
        role="dialog"
        aria-label="Run inputs"
        style={at}
        onKeyDown={(e) => {
          // Enter in a field runs, like submitting a form.
          if (e.key === "Enter" && (e.target as HTMLElement).tagName === "INPUT" && !missing) run();
        }}
      >
        <InputsForm autoFocus />
        <div className="popover-foot">
          <span className="muted small">{missing ? `${missing} required input${missing > 1 ? "s" : ""} missing` : "Enter to run"}</span>
          <span className="spacer" />
          {resumable && (
            <button className="btn small" onClick={() => run(true)} title="Continue from the checkpoint with these inputs">
              ⟳ Resume
            </button>
          )}
          <button className="btn small primary" disabled={!!missing} onClick={() => run()}>
            ▶ Run {workflow}
          </button>
        </div>
      </div>
    </>
  );
}

function place(el: HTMLElement): { top: number; right: number } {
  const r = el.getBoundingClientRect();
  return { top: r.bottom + 6, right: Math.max(8, window.innerWidth - r.right) };
}

export function RunControls() {
  const activePath = useStore((s) => s.activePath);
  const workflow = useStore((s) => s.workflow);
  const runState = useStore((s) => s.run);
  const open = useStore((s) => s.inputsOpen);
  const count = useStore((s) => inputFields(s).length);
  const missing = useStore((s) => missingInputs(s).length);
  const split = useRef<HTMLDivElement>(null);
  const res = runState.result;
  const resumable = !runState.running && !!res && ((res.ok && !!res.paused) || (!res.ok && res.kind !== "invalid_inputs"));

  if (runState.running) {
    return (
      <div className="run-controls">
        <button className="btn" onClick={() => void cancelRun(false)} title="Stop at the step boundary and save a resumable checkpoint (SIGINT)">
          ■ Cancel
        </button>
        <button className="btn danger" onClick={() => void cancelRun(true)} title="Kill the run immediately">
          Kill
        </button>
      </div>
    );
  }
  const can = !!activePath && !!workflow;
  return (
    <div className="run-controls">
      {resumable && (
        <button className="btn" onClick={() => run(true)} title="Continue from the checkpoint with the current inputs">
          ⟳ Resume
        </button>
      )}
      <div className="split-btn" ref={split}>
        <button
          className="btn primary"
          disabled={!can}
          title={!can ? "This file has no workflow to run" : missing ? `Set the ${missing} required input${missing > 1 ? "s" : ""} first` : `Run ${workflow}`}
          // A run that would be refused for missing inputs opens the form instead.
          onClick={() => (missing ? set({ inputsOpen: true }) : run())}
        >
          ▶ Run
        </button>
        <button
          className={`btn primary split-arrow ${open ? "open" : ""}`}
          disabled={!can}
          aria-haspopup="dialog"
          aria-expanded={open}
          title={`Run inputs${count ? ` (${count})` : ""}…`}
          onClick={() => set({ inputsOpen: !open })}
        >
          ▾{missing > 0 && <span className="split-dot" aria-label="required inputs missing" />}
        </button>
      </div>
      {open && split.current && <InputsPopover anchor={split.current} resumable={resumable} />}
    </div>
  );
}
