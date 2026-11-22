// Run / Resume / Cancel for the open workflow, in the top toolbar. Run is a
// split button: its ▾ opens the inputs popover (store.inputsOpen), whose
// values (store.runInputs) both Run and Resume use.
import { useEffect, useRef, useState } from "react";
import { ChevronDown, Play, RefreshCw, RotateCcw, Square } from "lucide-react";
import { cancelRun, startRun } from "../app/controller";
import { inputFields, runInputPairs, set, stageInputPairs, useStore } from "../app/store";
import { ICON } from "../lib/icons";
import { InputsForm, invalidInputs, missingInputs } from "./InputsForm";

/**
 * Starts the workflow from its first step in a new session, or (`resume`)
 * continues the last run. `fresh` also calls every agent again instead of
 * reusing a cached reply (`mhl run --no-cache`) — a run that failed on an
 * agent's bad answer otherwise gets the same answer back while it is cached.
 */
function run(resume = false, fresh = false) {
  set({ runTab: "execution", inputsOpen: false });
  // A run paused on entering a stage is resumed with that stage's inputs only.
  const res = useStore.getState().run.result;
  const stage = res?.awaiting_inputs?.stage ?? (res?.kind === "invalid_inputs" ? res.stage : undefined);
  void startRun(resume && stage ? stageInputPairs(stage) : runInputPairs(), resume ? { resume: true } : { noCache: fresh });
}

/** The inputs popover, anchored under the split button's right edge. */
function InputsPopover({ anchor, resumable }: { anchor: HTMLElement; resumable: boolean }) {
  const [at, setAt] = useState(() => place(anchor));
  const missing = useStore((s) => missingInputs(s).length);
  const invalid = useStore((s) => invalidInputs(s).length);
  const workflow = useStore((s) => s.workflow);
  const blocked = missing + invalid;
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
          if (e.key === "Enter" && (e.target as HTMLElement).tagName === "INPUT" && !blocked) run();
        }}
      >
        <InputsForm autoFocus />
        <div className="popover-foot">
          <span className="muted small">
            {missing ? `${missing} required input${missing > 1 ? "s" : ""} missing` : invalid ? `${invalid} input${invalid > 1 ? "s" : ""} to fix` : "Enter to run"}
          </span>
          <span className="spacer" />
          {resumable && (
            <button className="btn small" disabled={!!invalid} onClick={() => run(true)} title="Continue from the checkpoint with these inputs">
              <RotateCcw {...ICON} size={13} />
              Resume
            </button>
          )}
          <button
            className="btn small"
            disabled={!!blocked}
            onClick={() => run(false, true)}
            title="Start over in a new session, calling every agent again instead of reusing cached replies (mhl run --no-cache)"
          >
            <RefreshCw {...ICON} size={13} />
            Run fresh
          </button>
          <button className="btn small primary" disabled={!!blocked} onClick={() => run()} title="Start over from the first step in a new session">
            <Play {...ICON} size={13} fill="currentColor" />
            Run {workflow}
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
  const missing = useStore((s) => missingInputs(s).length + invalidInputs(s).length);
  const invalid = useStore((s) => invalidInputs(s).length);
  const split = useRef<HTMLDivElement>(null);
  const res = runState.result;
  // A resume refused for a stage's missing inputs leaves the run paused there: still resumable.
  const resumable = !runState.running && !!res && ((res.ok && !!res.paused) || (!res.ok && (res.kind !== "invalid_inputs" || !!res.stage)));

  if (runState.running) {
    return (
      <div className="run-controls">
        <button className="btn" onClick={() => void cancelRun(false)} title="Stop at the step boundary and save a resumable checkpoint (SIGINT)">
          <Square {...ICON} size={13} fill="currentColor" />
          Cancel
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
        <button
          className="btn"
          // A value the run would refuse opens the form to fix it instead.
          onClick={() => (invalid ? set({ inputsOpen: true }) : run(true))}
          title={invalid ? "Fix the inputs first" : "Continue from the checkpoint with the current inputs"}
        >
          <RotateCcw {...ICON} size={14} />
          Resume
        </button>
      )}
      <div className="split-btn" ref={split}>
        <button
          className="btn primary"
          disabled={!can}
          title={!can ? "This file has no workflow to run" : missing ? `Set or fix ${missing} input${missing > 1 ? "s" : ""} first` : `Run ${workflow} from the start, in a new session (▾ for Run fresh: no cached agent replies)`}
          // A run that would be refused for missing inputs opens the form instead.
          onClick={() => (missing ? set({ inputsOpen: true }) : run())}
        >
          <Play {...ICON} size={14} fill="currentColor" />
          Run
        </button>
        <button
          className={`btn primary split-arrow ${open ? "open" : ""}`}
          disabled={!can}
          aria-haspopup="dialog"
          aria-expanded={open}
          title={`Run inputs${count ? ` (${count})` : ""}…`}
          onClick={() => set({ inputsOpen: !open })}
        >
          <ChevronDown {...ICON} size={14} />
          {missing > 0 && <span className="split-dot" aria-label="required inputs missing" />}
        </button>
      </div>
      {open && split.current && <InputsPopover anchor={split.current} resumable={resumable} />}
    </div>
  );
}
