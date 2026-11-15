// The open file's imports, drawn over the diagram: each imported file (click
// to open it) and the names taken from it. Clicking a name — or a file, for
// all of its names — highlights the steps that call it and dims the rest.
// A .mh file dragged from the explorer and dropped here is imported.
import { useMemo, useState } from "react";
import { openFile } from "../app/controller";
import { importFile } from "../app/library";
import { currentWorkflow, get, set, useStore } from "../app/store";
import { importOrigins, type ImportOrigin } from "../sync/imports";
import { PATH_MIME } from "./mime";

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
  const dragFile = useStore((s) => s.dragFile);
  const [over, setOver] = useState(false);

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

  if (groups.length === 0 && !dragFile) return null;
  const toggle = (names: string[]) => set({ importFocus: same(focus, names) ? null : names });

  return (
    <div
      className={`imports-card ${dragFile ? "drop-ready" : ""} ${over ? "drop-over" : ""}`}
      onClick={(e) => e.stopPropagation()}
      onDragOver={(e) => {
        // Only a .mh file from the explorer (dragFile); the diagram never sees it.
        if (!get().dragFile || !e.dataTransfer.types.includes(PATH_MIME)) return;
        e.preventDefault();
        e.stopPropagation();
        e.dataTransfer.dropEffect = "copy";
        if (!over) setOver(true);
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOver(false);
      }}
      onDrop={(e) => {
        const path = get().dragFile;
        setOver(false);
        if (!path || !e.dataTransfer.types.includes(PATH_MIME)) return;
        e.preventDefault();
        e.stopPropagation();
        set({ dragFile: null });
        const file = get().library.find((l) => l.path === path);
        if (file) void importFile(file);
      }}
    >
      <button className="imports-head" onClick={() => setOpen(!open)} aria-expanded={open}>
        <span>Imports</span>
        <span className="muted">{groups.length}</span>
        <span className="spacer" />
        <span>{open ? "▾" : "▸"}</span>
      </button>
      {dragFile && <div className="imports-drop-hint">Drop to import {dragFile.split(/[\\/]/).pop()}</div>}
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
