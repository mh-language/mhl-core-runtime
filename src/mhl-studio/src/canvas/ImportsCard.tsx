// The open file's imports, drawn over the diagram: each imported file (click
// to open it) and the names taken from it. Clicking a name — or a file, for
// all of its names — highlights the steps that call it and dims the rest.
import { useMemo, useState } from "react";
import { openFile } from "../app/controller";
import { currentWorkflow, set, useStore } from "../app/store";
import { importOrigins, type ImportOrigin } from "../sync/imports";

/** The import origins of the open file, by local name. */
export function useImportOrigins(): Map<string, ImportOrigin> {
  const graph = useStore((s) => s.graph);
  const activePath = useStore((s) => s.activePath);
  const library = useStore((s) => s.library);
  return useMemo(() => importOrigins(graph, activePath, library), [graph, activePath, library]);
}

const same = (a: string[] | null, b: string[]) => !!a && a.length === b.length && a.every((x) => b.includes(x));

export function ImportsCard() {
  const origins = useImportOrigins();
  const wf = useStore(currentWorkflow);
  const focus = useStore((s) => s.importFocus);
  const [open, setOpen] = useState(true);

  // How many calls in this workflow use each local name.
  const uses = useMemo(() => {
    const n = new Map<string, number>();
    for (const node of wf?.nodes ?? []) for (const c of node.callSites ?? []) n.set(c.receiver, (n.get(c.receiver) ?? 0) + 1);
    return n;
  }, [wf]);

  // Group the local names by the file they come from, in import order.
  const groups = useMemo(() => {
    const by = new Map<string, { from: string; path: string; locals: [string, ImportOrigin][] }>();
    for (const [local, o] of origins) {
      const g = by.get(o.from) ?? { from: o.from, path: o.path, locals: [] };
      g.locals.push([local, o]);
      by.set(o.from, g);
    }
    return [...by.values()];
  }, [origins]);

  if (groups.length === 0) return null;
  const toggle = (names: string[]) => set({ importFocus: same(focus, names) ? null : names });

  return (
    <div className="imports-card" onClick={(e) => e.stopPropagation()}>
      <button className="imports-head" onClick={() => setOpen(!open)} aria-expanded={open}>
        <span>Imports</span>
        <span className="muted">{groups.length}</span>
        <span className="spacer" />
        <span>{open ? "▾" : "▸"}</span>
      </button>
      {open &&
        groups.map((g) => {
          const names = g.locals.map(([l]) => l);
          return (
            <div key={g.from} className="imports-file">
              <div className="imports-file-row">
                <button className={`imports-from ${same(focus, names) ? "on" : ""}`} title={`Highlight the steps using ${g.from}`} onClick={() => toggle(names)}>
                  {g.from}
                </button>
                <button className="icon-btn" title={`Open ${g.from}`} onClick={() => void openFile(g.path)}>
                  ↗
                </button>
              </div>
              <div className="imports-names">
                {g.locals.map(([local, o]) => {
                  const n = uses.get(local) ?? 0;
                  return (
                    <button
                      key={local}
                      className={`imports-name ${n ? "" : "unused"} ${same(focus, [local]) ? "on" : ""}`}
                      title={`${o.name !== local ? `${o.name} as ${local}` : local} — ${n ? `${n} call${n > 1 ? "s" : ""} in this workflow` : "not called in this workflow"}`}
                      onClick={() => toggle([local])}
                    >
                      {local}
                      {n > 0 && <span className="imports-count">{n}</span>}
                    </button>
                  );
                })}
              </div>
            </div>
          );
        })}
      {focus && open && (
        <button className="link imports-clear" onClick={() => set({ importFocus: null })}>
          Clear highlight
        </button>
      )}
    </div>
  );
}
