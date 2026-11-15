// The flow states, as a bar above the diagram: drag one onto the diagram,
// or click it to insert it after the selected step (at the end otherwise).
import { edit } from "../app/controller";
import { currentWorkflow, useStore } from "../app/store";
import { DRAG_MIME } from "../canvas/WorkflowCanvas";
import { insertState, type Placement } from "../sync/edits";
import { PALETTE, type PaletteItem } from "../sync/templates";

export const STATE_ICON: Record<string, string> = {
  pass: "▭",
  choice: "◇",
  parallel: "⫴",
  approval: "👤",
  succeed: "✓",
  fail: "✕",
};

export function StatesBar() {
  const wf = useStore(currentWorkflow);
  const selection = useStore((s) => s.selection);
  const stale = useStore((s) => s.stale);
  const disabled = !wf || stale;

  const placement = (kind: PaletteItem["kind"]): Placement => {
    const sel = wf?.nodes.find((n) => n.id === selection);
    if (sel?.kind === "parallel" && !["parallel", "choice"].includes(kind)) return { into: sel.id };
    if (sel?.parent && !["parallel", "choice"].includes(kind)) return { into: sel.parent };
    if (sel) return { after: sel.id };
    return { end: true };
  };
  const insert = (item: PaletteItem) => {
    if (!wf) return;
    void edit((doc) => insertState(doc, wf.name, item.kind, placement(item.kind)));
  };

  return (
    <div className="states-bar" role="toolbar" aria-label="Flow states">
      <span className="states-bar-title">States</span>
      {PALETTE.map((p) => (
        <button
          key={p.kind}
          className="state-chip"
          draggable={!disabled}
          disabled={disabled}
          title={`${p.hint}\nDrag onto the diagram, or click to insert ${selection ? `after ${selection}` : "at the end"}.`}
          onDragStart={(e) => {
            e.dataTransfer.setData(DRAG_MIME, p.kind);
            e.dataTransfer.effectAllowed = "copy";
          }}
          onClick={() => insert(p)}
        >
          <span className="state-chip-icon">{STATE_ICON[p.kind]}</span>
          {p.label}
        </button>
      ))}
    </div>
  );
}
