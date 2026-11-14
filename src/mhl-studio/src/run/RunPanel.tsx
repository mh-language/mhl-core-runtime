import { useEffect, useRef } from "react";
import { clearRun, inspect } from "../app/controller";
import { set, useStore, type RunTab as Tab } from "../app/store";
import type { RunEvent } from "../lib/types";

function fmt(v: unknown): string {
  if (v === undefined || v === null) return "";
  return typeof v === "string" ? v : JSON.stringify(v);
}

function time(ts?: string) {
  if (!ts) return "";
  const d = new Date(ts);
  return d.toLocaleTimeString(undefined, { hour12: false }) + "." + String(d.getMilliseconds()).padStart(3, "0");
}

function eventRow(ev: RunEvent) {
  switch (ev.type) {
    case "run_start":
      return { ts: ev.ts, kind: "RunStarted", step: "", detail: `${ev.pipeline} · session ${ev.session}` };
    case "step_start":
      return { ts: ev.ts, kind: "StepEntered", step: ev.step, detail: `#${ev.index}` };
    case "step_end":
      return {
        ts: ev.ts,
        kind: ev.status === "failed" ? "StepFailed" : ev.status === "pause" ? "StepPaused" : "StepExited",
        step: ev.step,
        detail: `${ev.status}${ev.target ? ` → ${ev.target}` : ""} · ${ev.ms} ms${ev.error ? ` · ${ev.error}` : ""}`,
      };
    case "result":
      return {
        ts: undefined,
        kind: ev.ok ? (ev.paused ? "RunPaused" : "RunSucceeded") : "RunFailed",
        step: ev.step ?? "",
        detail: ev.ok ? fmt(ev.pause_reason ?? ev.break_reason) : `${ev.kind}: ${ev.error}`,
      };
    case "exit":
      return { ts: undefined, kind: "ProcessExited", step: "", detail: `code ${ev.code}` };
    default:
      return null;
  }
}

export function RunPanel() {
  const tab = useStore((s) => s.runTab);
  const setTab = (t: Tab) => set({ runTab: t });
  const run = useStore((s) => s.run);
  const activePath = useStore((s) => s.activePath);
  const bottomOpen = useStore((s) => s.bottomOpen);
  const logRef = useRef<HTMLPreElement>(null);

  const workflow = useStore((s) => s.workflow);
  useEffect(() => {
    if (activePath) void inspect([]);
  }, [activePath, workflow]);

  useEffect(() => {
    if (tab === "logs" && logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [run.logs.length, tab]);

  const res = run.result;
  const paused = !run.running && res?.ok && res.paused;
  const rows = run.events.map(eventRow).filter(Boolean) as NonNullable<ReturnType<typeof eventRow>>[];

  let status: { cls: string; text: string } = { cls: "", text: "Not started" };
  if (run.running) status = { cls: "running", text: "Running…" };
  else if (res && !res.ok) status = { cls: "failed", text: `Failed${res.step ? ` at ${res.step}` : ""} — ${res.kind}` };
  else if (paused) status = { cls: "paused", text: `Paused at ${run.visits[run.visits.length - 1] ?? "?"}: ${fmt(res?.pause_reason)}` };
  else if (res?.ok) status = { cls: "succeeded", text: res.broke ? `Stopped by break${res.break_reason ? `: ${fmt(res.break_reason)}` : ""}` : "Succeeded" };
  else if (run.exitCode != null && run.exitCode !== 0) status = { cls: "failed", text: `mhl exited with code ${run.exitCode}` };

  return (
    <div className={`run-panel ${bottomOpen ? "" : "collapsed"}`}>
      <div className="tabs">
        {(["execution", "events", "logs", "variables"] as Tab[]).map((t) => (
          <button key={t} className={`tab ${tab === t ? "active" : ""}`} onClick={() => { setTab(t); set({ bottomOpen: true }); }}>
            {t === "execution" ? "Execution" : t === "events" ? `Events (${rows.length})` : t === "logs" ? "Logs" : "Variables"}
          </button>
        ))}
        <span className={`run-status ${status.cls}`}>{status.text}</span>
        <span className="spacer" />
        {res && !run.running && (
          <button className="btn small" onClick={clearRun} title="Clear the execution view">
            Clear
          </button>
        )}
        <button className="icon-btn" title={bottomOpen ? "Collapse" : "Expand"} onClick={() => set({ bottomOpen: !bottomOpen })}>
          {bottomOpen ? "▾" : "▴"}
        </button>
      </div>
      {bottomOpen && (
        <div className="tab-body">
          {tab === "execution" && (
            <div className="execution">
              <div className="execution-summary">
                {paused && (
                  <div className="approval">
                    <h4>Waiting for a decision</h4>
                    <p>{fmt(res?.pause_reason) || "The run paused."}</p>
                    <p className="muted small">Set the inputs the gate reads (e.g. approved = true), then Resume. The paused step runs again with them.</p>
                    <button className="btn small" onClick={() => set({ inputsOpen: true })}>
                      Set inputs and resume…
                    </button>
                  </div>
                )}
                {res && !res.ok && (
                  <div className="failure">
                    <h4>{res.kind}</h4>
                    {res.kind === "invalid_inputs" && (
                      <button className="btn small" onClick={() => set({ inputsOpen: true })}>
                        Fix the inputs…
                      </button>
                    )}
                    <p className="mono small">{res.error}</p>
                    {res.hint && <p className="muted small">{res.hint}</p>}
                  </div>
                )}
                {!res && !run.running && (
                  <p className="muted small">
                    Not run yet. Set the inputs with the{" "}
                    <button className="link" onClick={() => set({ inputsOpen: true })}>
                      ▾ next to Run
                    </button>
                    .
                  </p>
                )}
                {run.session && <p className="muted small mono">session {run.session}</p>}
                {res?.executed && <p className="small">Executed: {res.executed.join(" → ")}</p>}
                {res?.skipped?.length ? <p className="small muted">Skipped (resumed): {res.skipped.join(", ")}</p> : null}
              </div>
            </div>
          )}
          {tab === "events" && (
            <table className="events">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Event</th>
                  <th>Step</th>
                  <th>Detail</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r, i) => (
                  <tr key={i} className={r.kind.includes("Fail") ? "bad" : r.kind.includes("Paus") ? "warn" : ""}>
                    <td className="mono">{time(r.ts)}</td>
                    <td>{r.kind}</td>
                    <td className="mono">
                      {r.step && (
                        <button className="link" onClick={() => set({ selection: r.step })}>
                          {r.step}
                        </button>
                      )}
                    </td>
                    <td className="mono">{r.detail}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          {tab === "logs" && (
            <pre className="logs" ref={logRef}>
              {run.logs.join("\n") || "No output yet."}
            </pre>
          )}
          {tab === "variables" && <pre className="logs">{res?.vars ? JSON.stringify(res.vars, null, 2) : "Variables appear when the run ends."}</pre>}
        </div>
      )}
    </div>
  );
}
