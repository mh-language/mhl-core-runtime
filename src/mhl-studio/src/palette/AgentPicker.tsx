// The ready-made agent chooser: one row per agent CLI, one button per role
// (sync/agentTemplates.ts). A CLI not found on PATH is still offered — the
// workflow may run elsewhere — but marked, so the choice is informed.
import { useEffect } from "react";
import { loadClis } from "../app/controller";
import { useStore } from "../app/store";
import { BACKENDS, ROLES, type Backend, type Role } from "../sync/agentTemplates";

export function AgentPicker({
  at,
  title,
  onPick,
  onClose,
}: {
  at: { top: number; left: number };
  title: string;
  onPick: (backend: Backend, role: Role) => void;
  onClose: () => void;
}) {
  const clis = useStore((s) => s.clis);
  useEffect(() => {
    void loadClis(true);
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  const left = Math.max(8, Math.min(at.left, window.innerWidth - 548));
  const top = Math.max(8, Math.min(at.top, window.innerHeight - 330));
  return (
    <>
      <div className="menu-backdrop" onClick={onClose} onContextMenu={(e) => (e.preventDefault(), onClose())} />
      <div className="agent-picker" role="dialog" aria-label={title} style={{ top, left }}>
        <div className="menu-title">{title}</div>
        <table>
          <thead>
            <tr>
              <th />
              {ROLES.map((r) => (
                <th key={r.id} title={r.hint}>
                  {r.label}
                  <span className="agent-picker-hint">{r.hint}</span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {BACKENDS.map((b) => {
              const found = clis?.[b.command];
              const missing = clis !== null && !found;
              return (
                <tr key={b.id} className={missing ? "missing" : ""}>
                  <th scope="row" title={found ? `${b.command}: ${found}` : missing ? `${b.command} is not on PATH: install it, or run the workflow where it is` : b.command}>
                    {b.label}
                    <span className={`agent-picker-cli ${found ? "found" : missing ? "missing" : ""}`}>
                      {found ? "✓ installed" : missing ? "not found" : "…"}
                    </span>
                  </th>
                  {ROLES.map((r) => (
                    <td key={r.id}>
                      <button className="btn small" onClick={() => onPick(b.id, r.id)} title={`${b.label} — ${r.label.toLowerCase()}: ${r.hint}`}>
                        {r.label}
                      </button>
                    </td>
                  ))}
                </tr>
              );
            })}
          </tbody>
        </table>
        <p className="agent-picker-foot muted small">
          Each template carries the flags that keep its role safe on that CLI, with the reason for each one. The prompt goes on stdin; model and effort are
          per-call inputs.
        </p>
      </div>
    </>
  );
}
