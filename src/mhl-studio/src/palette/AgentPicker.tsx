// The ready-made agent chooser: one row per agent CLI or HTTP API, one
// button per role it offers (sync/agentTemplates.ts). A backend whose
// executables are not on PATH is still offered — the workflow may run
// elsewhere — but marked, so the choice is informed.
import { useEffect } from "react";
import { loadClis } from "../app/controller";
import { useStore } from "../app/store";
import { BACKENDS, ROLES, type Backend, type BackendInfo, type Role } from "../sync/agentTemplates";

/** "✓ installed", or what is missing: "jq not found". */
function availability(b: BackendInfo, clis: Record<string, string> | null): { cls: string; text: string; title: string } {
  if (clis === null) return { cls: "", text: "…", title: b.requires.join(", ") };
  const missing = b.requires.filter((c) => !clis[c]);
  if (!missing.length) {
    const what = b.kind === "api" ? b.requires.join(" + ") : "installed";
    return { cls: "found", text: `✓ ${what}`, title: b.requires.map((c) => `${c}: ${clis[c]}`).join("\n") };
  }
  return {
    cls: "missing",
    text: `${missing.join(", ")} not found`,
    title: `${missing.join(", ")} ${missing.length > 1 ? "are" : "is"} not on PATH: install ${missing.length > 1 ? "them" : "it"}, or run the workflow where ${missing.length > 1 ? "they are" : "it is"}`,
  };
}

function BackendTable({ kind, onPick }: { kind: BackendInfo["kind"]; onPick: (backend: Backend, role: Role) => void }) {
  const clis = useStore((s) => s.clis);
  const backends = BACKENDS.filter((b) => b.kind === kind);
  const roles = ROLES.filter((r) => backends.some((b) => b.roles.includes(r.id)));
  return (
    <table>
      <thead>
        <tr>
          <th className="agent-picker-group">{kind === "api" ? "HTTP APIs (curl)" : "Agent CLIs"}</th>
          {roles.map((r) => (
            <th key={r.id} title={r.hint}>
              {r.label}
              <span className="agent-picker-hint">{r.hint}</span>
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {backends.map((b) => {
          const a = availability(b, clis);
          return (
            <tr key={b.id} className={a.cls === "missing" ? "missing" : ""}>
              <th scope="row" title={a.title}>
                {b.label}
                <span className={`agent-picker-cli ${a.cls}`}>{a.text}</span>
              </th>
              {roles.map((r) => (
                <td key={r.id}>
                  {b.roles.includes(r.id) && (
                    <button className="btn small" onClick={() => onPick(b.id, r.id)} title={`${b.label} — ${r.label.toLowerCase()}: ${r.hint}`}>
                      {r.label}
                    </button>
                  )}
                </td>
              ))}
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

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
  useEffect(() => {
    void loadClis(true);
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  const left = Math.max(8, Math.min(at.left, window.innerWidth - 548));
  const top = Math.max(8, Math.min(at.top, window.innerHeight - 520));
  return (
    <>
      <div className="menu-backdrop" onClick={onClose} onContextMenu={(e) => (e.preventDefault(), onClose())} />
      <div className="agent-picker" role="dialog" aria-label={title} style={{ top, left }}>
        <div className="menu-title">{title}</div>
        <BackendTable kind="cli" onPick={onPick} />
        <BackendTable kind="api" onPick={onPick} />
        <p className="agent-picker-foot muted small">
          Each template carries the flags that keep its role safe, with the reason for each one. The prompt goes on stdin; model and effort are per-call
          inputs. An API template is a curl request in sh: its key stays in the environment, never on the command line.
        </p>
      </div>
    </>
  );
}
