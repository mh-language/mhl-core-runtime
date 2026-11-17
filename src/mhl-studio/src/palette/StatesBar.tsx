// The flow states, as a bar above the diagram: drag one onto the diagram,
// or click it to insert it after the selected step (at the end otherwise).
import { Fragment } from "react";
import { GripVertical } from "lucide-react";
import { edit } from "../app/controller";
import { currentWorkflow, useStore } from "../app/store";
import { DRAG_MIME } from "../canvas/WorkflowCanvas";
import { StateBadge } from "../lib/icons";
import { insertState, type Placement } from "../sync/edits";
import { stateAllowed, workflowOnlyReason } from "../sync/kinds";
import { PALETTE, type PaletteItem } from "../sync/templates";

/** The states by role: what shapes the flow, what waits on a person, what ends the run. */
const GROUPS: { label: string; kinds: PaletteItem["kind"][] }[] = [
  { label: "Flow", kinds: ["pass", "choice", "parallel"] },
  { label: "Gate", kinds: ["approval"] },
  { label: "End", kinds: ["succeed", "fail"] },
];

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
      {GROUPS.map((g, gi) => (
        <Fragment key={g.label}>
          {gi > 0 && <span className="tb-sep" />}
          <span className="states-group-label">{g.label}</span>
          {g.kinds.map((kind) => {
            const p = PALETTE.find((x) => x.kind === kind)!;
            // Not `disabled`: a disabled button shows no tooltip, and this one must say why.
            const blocked = !!wf && !stateAllowed(wf, p.kind);
            return (
              <button
                key={p.kind}
                className="state-chip"
                draggable={!disabled && !blocked}
                disabled={disabled}
                aria-disabled={blocked || undefined}
                title={blocked ? workflowOnlyReason(wf, p.label) : `${p.hint}\nDrag onto the diagram, or click to insert ${selection ? `after ${selection}` : "at the end"}.`}
                onDragStart={(e) => {
                  e.dataTransfer.setData(DRAG_MIME, p.kind);
                  e.dataTransfer.effectAllowed = "copy";
                }}
                onClick={() => !blocked && insert(p)}
              >
                <StateBadge kind={p.kind} />
                {p.label}
                <GripVertical className="state-chip-grip" size={12} aria-hidden="true" />
              </button>
            );
          })}
        </Fragment>
      ))}
    </div>
  );
}
