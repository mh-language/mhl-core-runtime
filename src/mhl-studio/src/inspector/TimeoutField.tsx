// A step's / parallel group's `timeout` as a whole number plus a unit button
// (h, m, s, ms), instead of free text: whatever it writes is a valid
// Duration token. Empty means no timeout clause.
import { useEffect, useState } from "react";
import { cleanAmount, formatDuration, parseDuration, UNITS, type Unit } from "../sync/duration";

export function TimeoutField(props: { value?: string; onCommit: (v: string) => void }) {
  const parsed = parseDuration(props.value);
  const [amount, setAmount] = useState(parsed?.amount ?? "");
  const [unit, setUnit] = useState<Unit>(parsed?.unit ?? "s");
  useEffect(() => {
    const p = parseDuration(props.value);
    setAmount(p?.amount ?? "");
    if (p) setUnit(p.unit);
  }, [props.value]);

  const commit = (a: string, u: Unit) => {
    const next = formatDuration(a, u);
    if (next !== (props.value ?? "")) props.onCommit(next);
  };
  // `d` is valid mhl but not offered; a value already using it keeps its button.
  const units: Unit[] = unit === "d" ? [...UNITS, "d"] : [...UNITS];

  return (
    <div className="field">
      <span>Timeout</span>
      <div className="timeout-field">
        <div className="timeout-control">
          <input
            className="mono"
            inputMode="numeric"
            placeholder="none"
            aria-label="Timeout amount"
            value={amount}
            onChange={(e) => setAmount(cleanAmount(e.target.value))}
            onBlur={() => commit(amount, unit)}
            onKeyDown={(e) => e.key === "Enter" && commit(amount, unit)}
          />
          <div className="segmented small" role="group" aria-label="Timeout unit">
            {units.map((u) => (
              <button
                key={u}
                className={u === unit ? "active" : ""}
                aria-pressed={u === unit}
                title={
                  {
                    h: "hours",
                    m: "minutes",
                    s: "seconds",
                    ms: "milliseconds",
                    d: "days",
                  }[u]
                }
                onClick={() => {
                  setUnit(u);
                  commit(amount, u);
                }}
              >
                {u}
              </button>
            ))}
          </div>
        </div>
        {amount && (
          <button
            className="icon-btn"
            title="Remove the timeout"
            onClick={() => {
              setAmount("");
              commit("", unit);
            }}
          >
            ×
          </button>
        )}
      </div>
    </div>
  );
}
