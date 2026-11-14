// The left sidebar's toolbox: one outline of everything a workflow can call
// or use — tools (with their methods), agents, prompts, MCP servers, memory,
// types — grouped by kind, whether declared in the open file or in another
// file of the folder. Items of another file show that file's name and are
// imported on use: dragging one onto the diagram adds the import (and marks
// it `export` in its own file when needed); a hover button imports it alone.
import { useMemo, useState } from "react";
import { edit, openFile, revealSpan } from "../app/controller";
import { addAgentStep, addToolCallStep, importDecl } from "../app/library";
import { currentWorkflow, selectTool, useStore, type LibraryFile } from "../app/store";
import { AGENT_MIME, CALL_MIME, DRAG_MIME, PROMPT_MIME, SCHEMA_MIME } from "../canvas/WorkflowCanvas";
import { dropPrompt } from "../app/prompts";
import { dropSchema } from "../app/schemas";
import { insertAgentTemplate } from "../app/controller";
import { AgentPicker } from "./AgentPicker";
import type { Declaration, ToolMethod } from "../lib/types";
import { insertState, type Placement } from "../sync/edits";
import { IMPORTABLE, isImported } from "../sync/imports";
import { addTool } from "../sync/tools";
import { callText } from "../sync/templates";

// A store selector must return a stable value: a fresh `[]` on every read
// makes React's useSyncExternalStore loop forever.
const NO_FILES: LibraryFile[] = [];

const KIND_ICON: Record<string, string> = {
  tool: "🛠",
  agent: "⚙",
  router: "⇄",
  prompt: "❝",
  extension: "⧉",
  memory: "▤",
  type: "{}",
  enum: "≡",
  alias: "=",
  schema: "§",
  skill: "✦",
};

const GROUPS: { title: string; kinds: Declaration["kind"][] }[] = [
  { title: "Tools", kinds: ["tool"] },
  { title: "Agents", kinds: ["agent", "router"] },
  { title: "Prompts", kinds: ["prompt", "skill"] },
  { title: "MCP & extensions", kinds: ["extension"] },
  { title: "Memory", kinds: ["memory"] },
  { title: "Types", kinds: ["type", "enum", "alias", "schema"] },
];

/** One declaration, from the open file (no `file`) or another one. */
interface Item {
  decl: Declaration;
  file?: LibraryFile;
  /** Usable here already: declared in this file, or imported. */
  available: boolean;
}

function basename(rel: string) {
  return rel.split("/").pop() ?? rel;
}

function useItems(): Item[] {
  const graph = useStore((s) => s.graph);
  const activePath = useStore((s) => s.activePath);
  const library = useStore((s) => s.library ?? NO_FILES);
  return useMemo(() => {
    const local: Item[] = (graph?.declarations ?? []).filter((d) => IMPORTABLE.has(d.kind)).map((decl) => ({ decl, available: true }));
    const taken = new Set(local.map((i) => i.decl.name));
    const other: Item[] = [];
    for (const f of library) {
      if (f.path === activePath) continue;
      for (const decl of f.declarations) {
        if (!IMPORTABLE.has(decl.kind) || taken.has(decl.name)) continue;
        other.push({ decl, file: f, available: !!graph && !!activePath && isImported(graph, activePath, decl.name, f.path) });
      }
    }
    // This file first, then what is already imported, then the rest.
    const rank = (i: Item) => (i.file ? (i.available ? 1 : 2) : 0);
    return [...local, ...other].sort((a, b) => rank(a) - rank(b) || a.decl.name.localeCompare(b.decl.name));
  }, [graph, activePath, library]);
}

export function Palette() {
  const wf = useStore(currentWorkflow);
  const selection = useStore((s) => s.selection);
  const stale = useStore((s) => s.stale);
  const openTool = useStore((s) => s.tool);
  const items = useItems();
  const [filter, setFilter] = useState("");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [agentPick, setAgentPick] = useState<{ top: number; left: number } | null>(null);
  const canDrop = !!wf && !stale;

  const placement = (): Placement => {
    const sel = wf?.nodes.find((n) => n.id === selection);
    if (sel?.kind === "parallel") return { into: sel.id };
    if (sel?.parent) return { into: sel.parent };
    if (sel) return { after: sel.id };
    return { end: true };
  };

  const q = filter.trim().toLowerCase();
  const matches = (i: Item) =>
    !q || i.decl.name.toLowerCase().includes(q) || (i.decl.methods ?? []).some((m) => m.name.toLowerCase().includes(q));

  const open = (i: Item, method: string | null = null) => {
    if (i.file) {
      void openFile(i.file.path).then(() => i.decl.kind === "tool" && selectTool(i.decl.name, method));
    } else if (i.decl.kind === "tool") {
      selectTool(i.decl.name, method);
    } else {
      revealSpan(i.decl.span);
    }
  };

  const newStepCalling = (i: Item, m?: ToolMethod) => {
    if (!wf) return;
    if (i.file) {
      if (m) void addToolCallStep(i.file, i.decl.name, m.name, wf.name, placement());
      else void addAgentStep(i.file, i.decl.name, wf.name, placement());
    } else if (m) {
      void edit((doc) => insertState(doc, wf.name, "tool", placement(), { tool: i.decl.name, method: m }));
    } else {
      void edit((doc) => insertState(doc, wf.name, "agent", placement(), undefined, i.decl.name));
    }
  };

  const newTool = () => {
    let name = "";
    void edit((doc) => {
      const r = addTool(doc);
      name = r.name;
      return r;
    }).then((ok) => ok && name && selectTool(name));
  };

  const row = (i: Item) => {
    const d = i.decl;
    const isAgent = d.kind === "agent";
    const isPrompt = d.kind === "prompt";
    const isSchema = d.kind === "schema";
    const draggable = (isAgent || isPrompt || isSchema) && canDrop;
    const active = !i.file && openTool?.name === d.name && !openTool.method;
    const methods = d.kind === "tool" ? (d.methods ?? []).filter((m) => !i.file || !m.internal) : [];
    return (
      <div key={`${i.file?.path ?? ""}:${d.kind}:${d.name}`} className="tb-entry">
        <div
          className={`tb-item kind-${d.kind} ${active ? "active" : ""} ${i.file ? "external" : ""}`}
          draggable={draggable}
          title={
            (isAgent ? "Drag onto a step to call it there, or onto the diagram for a new step. " : "") +
            (isPrompt ? "Drag onto a step to make its agent call use it, or onto the diagram for a new agent step. " : "") +
            (isSchema ? "Drag onto a step to shape its agent call's answer (schema:), or onto the diagram for a new agent step. " : "") +
            (i.file ? `Declared in ${i.file.rel}${i.available ? " (imported)" : " — imported when you use it"}` : `Declared in this file`)
          }
          onClick={() => open(i)}
          onDoubleClick={() => {
            if (isAgent) newStepCalling(i);
            else if (isPrompt && wf && canDrop) void dropPrompt({ prompt: d.name, file: i.file?.path }, wf.name, { placement: placement() });
            else if (isSchema && wf && canDrop) void dropSchema({ schema: d.name, file: i.file?.path }, wf.name, { placement: placement() });
          }}
          onDragStart={(e) => {
            e.dataTransfer.setData(DRAG_MIME, "agent");
            if (isPrompt) e.dataTransfer.setData(PROMPT_MIME, JSON.stringify({ prompt: d.name, file: i.file?.path }));
            else if (isSchema) e.dataTransfer.setData(SCHEMA_MIME, JSON.stringify({ schema: d.name, file: i.file?.path }));
            else e.dataTransfer.setData(AGENT_MIME, JSON.stringify(i.file ? { agent: d.name, file: i.file.path } : { agent: d.name }));
            e.dataTransfer.effectAllowed = "copy";
          }}
        >
          <span className="tb-icon">{KIND_ICON[d.kind] ?? "·"}</span>
          <span className="tb-name">{d.name}</span>
          {d.kind === "extension" && d.detail && <span className="tb-detail">{d.detail}</span>}
          {i.file && (
            <span className="tb-file" title={i.file.rel}>
              {basename(i.file.rel)}
            </span>
          )}
          {i.file &&
            (i.available ? (
              <span className="tb-imported" title={`Imported from ${i.file.rel}`}>
                ✓
              </span>
            ) : (
              <button
                className="tb-action"
                aria-label={`Import ${d.name}`}
                title={d.exported ? `Import ${d.name} into this file` : `Mark ${d.name} export in ${i.file.rel}, then import it`}
                disabled={stale}
                onClick={(e) => {
                  e.stopPropagation();
                  void importDecl(i.file!, d.name);
                }}
              >
                ⤓
              </button>
            ))}
        </div>
        {methods.map((m) => {
          const usable = !m.internal && canDrop;
          return (
            <div
              key={m.name}
              className={`tb-method ${!i.file && openTool?.name === d.name && openTool.method === m.name ? "active" : ""} ${m.internal ? "internal" : ""}`}
              draggable={usable}
              title={
                m.internal
                  ? "internal — only this tool (self.) and this file's tests may call it"
                  : `${callText(d.name, m)}\nDrag onto a step to call it there, or onto the diagram for a new step${i.file && !i.available ? ` (imports ${d.name})` : ""}.`
              }
              onClick={() => open(i, m.name)}
              onDoubleClick={() => usable && newStepCalling(i, m)}
              onDragStart={(e) => {
                e.dataTransfer.setData(DRAG_MIME, "tool");
                e.dataTransfer.setData(CALL_MIME, JSON.stringify({ tool: d.name, method: m.name, ...(i.file ? { file: i.file.path } : {}) }));
                e.dataTransfer.effectAllowed = "copy";
              }}
            >
              <span className="tb-method-name">{m.name}</span>
              <span className="tb-method-params">({m.params.join(", ")})</span>
              {m.internal && <span className="tb-detail">internal</span>}
            </div>
          );
        })}
      </div>
    );
  };

  const visible = items.filter(matches);
  return (
    <div className="toolbox">
      <div className="tb-filter">
        <input placeholder="Filter tools, agents, prompts…" value={filter} spellCheck={false} onChange={(e) => setFilter(e.target.value)} />
      </div>
      {GROUPS.map((g) => {
        const rows = visible.filter((i) => g.kinds.includes(i.decl.kind));
        const isTools = g.title === "Tools";
        const isAgents = g.title === "Agents";
        if (rows.length === 0 && !((isTools || isAgents) && !q)) return null;
        const closed = collapsed[g.title];
        return (
          <section key={g.title} className="tb-group">
            <div className="tb-group-head">
              <button className="tb-toggle" onClick={() => setCollapsed((c) => ({ ...c, [g.title]: !closed }))}>
                <span className="tb-chevron">{closed ? "▸" : "▾"}</span>
                {g.title}
                <span className="tb-count">{rows.length}</span>
              </button>
              {isTools && (
                <button className="icon-btn" title="New tool" disabled={stale} onClick={newTool}>
                  +
                </button>
              )}
              {isAgents && (
                <button
                  className="icon-btn"
                  title="Add a ready-made agent to this file…"
                  disabled={stale}
                  onClick={(e) => {
                    const r = e.currentTarget.getBoundingClientRect();
                    setAgentPick({ top: r.bottom + 4, left: r.left });
                  }}
                >
                  +
                </button>
              )}
            </div>
            {!closed && rows.map(row)}
            {!closed && isAgents && rows.length === 0 && (
              <p className="tb-empty">
                No agents yet. + adds a ready-made one (Claude, Codex, Copilot, Antigravity or Devin).
              </p>
            )}
            {!closed && isTools && rows.length === 0 && (
              <p className="tb-empty">
                No tools yet.{" "}
                <button className="link" onClick={newTool}>
                  Create one
                </button>{" "}
                here, or a tools file with + in the explorer.
              </p>
            )}
          </section>
        );
      })}
      {q && visible.length === 0 && <p className="tb-empty">Nothing matches “{filter}”.</p>}
      {agentPick && (
        <AgentPicker
          at={agentPick}
          title="Add a ready-made agent to this file"
          onClose={() => setAgentPick(null)}
          onPick={(backend, role) => {
            setAgentPick(null);
            void insertAgentTemplate(backend, role);
          }}
        />
      )}
    </div>
  );
}
