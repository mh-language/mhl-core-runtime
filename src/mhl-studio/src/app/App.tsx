import { useEffect } from "react";
import { ChevronDown, Code, Columns2, FolderOpen, Plus, Redo2, Save, Undo2, Workflow, type LucideIcon } from "lucide-react";
import { edit, openFolder, redo, save, undo } from "./controller";
import { hasNoWorkflow, isMh, set, useStore, type View } from "./store";
import { addWorkflow } from "../sync/edits";
import { newWorkflowSource } from "../sync/templates";
import { WorkflowCanvas } from "../canvas/WorkflowCanvas";
import { CodeEditor } from "../editor/CodeEditor";
import { StatesBar } from "../palette/StatesBar";
import { Inspector } from "../inspector/Inspector";
import { RunPanel } from "../run/RunPanel";
import { RunControls } from "../run/RunControls";
import { togglePanel, type Panel } from "./panels";
import { FileTree } from "./FileTree";
import { ICON } from "../lib/icons";
import logo from "../assets/mhl-logo.png";

const MOD = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent) ? "⌘" : "Ctrl+";
const ALT = MOD === "⌘" ? "⌥" : "Alt+";

/** A VS Code-style layout icon: a window with the toggled region filled when open. */
function PanelIcon({ panel, open }: { panel: Panel; open: boolean }) {
  const region = { left: { x: 1.5, y: 1.5, width: 4, height: 11 }, right: { x: 10.5, y: 1.5, width: 4, height: 11 }, bottom: { x: 1.5, y: 8.5, width: 13, height: 4 } }[panel];
  return (
    <svg width="16" height="14" viewBox="0 0 16 14" aria-hidden="true">
      <rect x="1" y="1" width="14" height="12" rx="1.5" fill="none" stroke="currentColor" />
      <rect {...region} fill={open ? "currentColor" : "none"} stroke="currentColor" strokeWidth={open ? 0 : 0.8} />
    </svg>
  );
}

function PanelToggle({ panel, open, label, keys }: { panel: Panel; open: boolean; label: string; keys: string }) {
  return (
    <button
      className={`tb-btn panel-toggle ${open ? "on" : ""}`}
      title={`${open ? "Hide" : "Show"} ${label} (${keys})`}
      aria-pressed={open}
      onClick={() => togglePanel(panel)}
    >
      <PanelIcon panel={panel} open={open} />
    </button>
  );
}

function LayoutToggles() {
  const root = useStore((s) => s.root);
  const view = useStore((s) => s.view);
  const noFlow = useStore(hasNoWorkflow);
  const activePath = useStore((s) => s.activePath);
  const leftOpen = useStore((s) => s.leftOpen);
  const rightOpen = useStore((s) => s.rightOpen);
  const bottomOpen = useStore((s) => s.bottomOpen);
  if (!root) return null;
  return (
    <span className="tb-group">
      <PanelToggle panel="left" open={leftOpen} label="files" keys={`${MOD}B`} />
      {activePath && !noFlow && <PanelToggle panel="bottom" open={bottomOpen} label="run panel" keys={`${MOD}J`} />}
      {isMh(activePath) && view !== "code" && <PanelToggle panel="right" open={rightOpen} label="inspector" keys={`${ALT}${MOD}B`} />}
    </span>
  );
}

function Toolbar() {
  const root = useStore((s) => s.root);
  const view = useStore((s) => s.view);
  const noFlow = useStore(hasNoWorkflow);
  const graph = useStore((s) => s.graph);
  const workflow = useStore((s) => s.workflow);
  const activePath = useStore((s) => s.activePath);
  const dirty = useStore((s) => (s.activePath ? s.dirty[s.activePath] : false));
  const mhlVersion = useStore((s) => s.mhlVersion);
  const views: [View, string, LucideIcon][] = [
    ["design", "Design", Workflow],
    ["split", "Split", Columns2],
    ["code", "Code", Code],
  ];
  const hasWorkflows = !!graph && graph.workflows.length > 0;
  return (
    <header className="toolbar">
      <img className="brand" src={logo} alt="mhl studio" title="mhl studio" />
      <span className="tb-group">
        <button className="tb-btn" onClick={() => void openFolder()} title={`Open folder… (${MOD}O)`} aria-label="Open folder">
          <FolderOpen {...ICON} />
        </button>
        {activePath && (
          <button className="tb-btn" disabled={!dirty} onClick={() => void save()} title={`Save (${MOD}S)`} aria-label="Save">
            <Save {...ICON} />
            {dirty && <span className="tb-dirty" aria-label="unsaved changes" />}
          </button>
        )}
      </span>
      {activePath && (
        <span className="tb-group">
          <button className="tb-btn" onClick={undo} title={`Undo (${MOD}Z)`} aria-label="Undo">
            <Undo2 {...ICON} />
          </button>
          <button className="tb-btn" onClick={redo} title={`Redo (⇧${MOD}Z)`} aria-label="Redo">
            <Redo2 {...ICON} />
          </button>
        </span>
      )}
      {(hasWorkflows || isMh(activePath)) && (
        <span className="wf-picker">
          {hasWorkflows && graph && (
            <label className="wf-select-wrap" title="The workflow the diagram shows and Run runs">
              <Workflow {...ICON} />
              <select className="wf-select" aria-label="Workflow" value={workflow ?? ""} onChange={(e) => set({ workflow: e.target.value, selection: null })}>
                {graph.workflows.map((w) => (
                  <option key={w.name} value={w.name}>
                    {w.kind} {w.name}
                  </option>
                ))}
              </select>
              <ChevronDown {...ICON} size={14} />
            </label>
          )}
          {isMh(activePath) && (
            <button
              className="tb-btn wf-add"
              title="New workflow in this file"
              aria-label="New workflow"
              onClick={() => {
                let added = "";
                void edit((doc) => {
                  const r = addWorkflow(doc, newWorkflowSource);
                  added = r.name;
                  return r;
                }).then((ok) => ok && added && set({ workflow: added, selection: null }));
              }}
            >
              <Plus {...ICON} />
            </button>
          )}
        </span>
      )}
      <span className="spacer" />
      {activePath && !noFlow && <RunControls />}
      {root && !noFlow && (
        <div className="view-switch" role="group" aria-label="View">
          {views.map(([v, label, Icon]) => (
            <button key={v} className={view === v ? "active" : ""} aria-pressed={view === v} aria-label={label} title={label} onClick={() => set({ view: v })}>
              <Icon {...ICON} />
            </button>
          ))}
        </div>
      )}
      <LayoutToggles />
      <span className="muted small" title="the bundled mhl runtime">
        {mhlVersion}
      </span>
    </header>
  );
}

function Welcome() {
  return (
    <div className="welcome">
      <h1>mhl studio</h1>
      <p>Design mhl workflows visually — every change is written as `.mh` code, and every code change redraws the diagram.</p>
      <button className="btn primary" onClick={() => void openFolder()}>
        Open a folder
      </button>
    </div>
  );
}

function Toasts() {
  const toast = useStore((s) => s.toast);
  if (!toast) return null;
  return (
    <div className={`toast ${toast.kind}`} onClick={() => set({ toast: null })}>
      {toast.text}
    </div>
  );
}

export function App() {
  const root = useStore((s) => s.root);
  const view = useStore((s) => s.view);
  const noFlow = useStore(hasNoWorkflow);
  const activePath = useStore((s) => s.activePath);
  const leftOpen = useStore((s) => s.leftOpen);
  const rightOpen = useStore((s) => s.rightOpen);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;
      if (!mod) return;
      const inEditor = (e.target as HTMLElement).closest?.(".monaco-editor, input, textarea");
      if (e.key === "s") {
        e.preventDefault();
        void save();
      } else if (e.key === "o" && !e.shiftKey) {
        e.preventDefault();
        void openFolder();
      } else if (e.key.toLowerCase() === "z" && !inEditor) {
        e.preventDefault();
        if (e.shiftKey) redo();
        else undo();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div className="app">
      <Toolbar />
      {!root ? (
        <Welcome />
      ) : (
        <div className="workbench">
          {leftOpen && (
            <aside className="sidebar-left">
              <FileTree />
            </aside>
          )}
          <main className="main-area">
            <div className={`views view-${noFlow ? "code" : view}`}>
              <div className="design-pane">
                <StatesBar />
                <div className="canvas-host">
                  <WorkflowCanvas />
                </div>
              </div>
              <div className="code-pane">
                <CodeEditor />
              </div>
            </div>
            {!noFlow && <RunPanel />}
          </main>
          {rightOpen && view !== "code" && isMh(activePath) && (
            <aside className="sidebar-right">
              <div className="panel-head">Inspector</div>
              <Inspector />
            </aside>
          )}
        </div>
      )}
      <Toasts />
    </div>
  );
}
