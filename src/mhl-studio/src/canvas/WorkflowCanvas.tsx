import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Background,
  Controls,
  MarkerType,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Connection,
  type Edge,
  type Node,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { currentWorkflow, get, set, useStore } from "../app/store";
import { addAgentCallToStep, addAgentStep, addToolCallStep, addToolCallToStep, hintSchema } from "../app/library";
import { edit } from "../app/controller";
import { connect, deleteNode, insertState, removeEdge, type Placement } from "../sync/edits";
import { transitionAllowed } from "../sync/kinds";
import { END, FAIL, START } from "../lib/types";
import type { StateKind } from "../sync/templates";
import { layoutWorkflow, ROUTE_PREFIX, takenEdges, type FlowEdgeData, type Layout } from "./layout";
import { nodeTypes } from "./nodes";
import { edgeTypes } from "./ElkEdge";
import { ImportsCard } from "./ImportsCard";
import { centerPoint, FIT, loadKeepCentered, saveKeepCentered, ViewportButtons } from "./viewport";
import { dropPrompt, type PromptDrag, type PromptTarget } from "../app/prompts";
import { dropSchema, type SchemaDrag } from "../app/schemas";

export const DRAG_MIME = "application/x-mhl-state";
/**
 * With a "tool" state: `{tool, method, file?}` — the method the new step
 * calls; `file` when the tool lives in another workspace file (imported then).
 */
export const CALL_MIME = "application/x-mhl-call";
/** With an "agent" state: `{agent, file}` — an agent of another workspace file. */
export const AGENT_MIME = "application/x-mhl-agent";
/**
 * With an "agent" state: a prompt — `{prompt, file?}` (a declaration) or
 * `{md}` (a Markdown file of the explorer, declared on drop). See app/prompts.ts.
 */
export const PROMPT_MIME = "application/x-mhl-prompt";
/**
 * With an "agent" state: a JSON Schema — `{schema, file?}` (a declaration)
 * or `{json}` (a .json file of the explorer, declared on drop). See app/schemas.ts.
 */
export const SCHEMA_MIME = "application/x-mhl-schema";

function payload<T>(ev: React.DragEvent, mime: string): T | undefined {
  try {
    return JSON.parse(ev.dataTransfer.getData(mime) || "null") ?? undefined;
  } catch {
    return undefined;
  }
}

function Canvas() {
  const graph = useStore((s) => s.graph);
  const graphSource = useStore((s) => s.graphSource);
  const wf = useStore(currentWorkflow);
  const stale = useStore((s) => s.stale);
  const scheme = useStore((s) => s.scheme);
  const selection = useStore((s) => s.selection);
  const visits = useStore((s) => s.run.visits);
  const result = useStore((s) => s.run.result);
  const runPath = useStore((s) => s.run.path);
  const activePath = useStore((s) => s.activePath);
  const [layout, setLayout] = useState<Layout>({ nodes: [], edges: [] });
  const [selectedEdge, setSelectedEdge] = useState<string | null>(null);
  const flow = useReactFlow();
  const wrapper = useRef<HTMLDivElement>(null);
  const lastFit = useRef("");
  const [keepCentered, setKeepCentered] = useState(loadKeepCentered);
  const keepRef = useRef(keepCentered);
  keepRef.current = keepCentered;

  const fit = useCallback((duration = 200) => void flow.fitView({ ...FIT, duration }), [flow]);
  /** Pans the diagram to the middle of the canvas at the current zoom. */
  const center = useCallback(
    (duration = 200) => {
      const el = wrapper.current;
      const ns = flow.getNodes();
      if (!el || ns.length === 0) return;
      const zoom = flow.getZoom();
      const p = centerPoint(flow.getNodesBounds(ns), { width: el.clientWidth, height: el.clientHeight }, zoom);
      void flow.setCenter(p.x, p.y, { zoom, duration });
    },
    [flow],
  );
  const toggleKeepCentered = useCallback(() => {
    const on = !keepRef.current;
    setKeepCentered(on);
    saveKeepCentered(on);
    if (on) center();
  }, [center]);

  // Re-center when the canvas changes size (a panel toggled, the split moved).
  useEffect(() => {
    const el = wrapper.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    let frame = 0;
    const ro = new ResizeObserver(() => {
      if (!keepRef.current) return;
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => center(0));
    });
    ro.observe(el);
    return () => {
      cancelAnimationFrame(frame);
      ro.disconnect();
    };
  }, [center]);

  // Relayout whenever the graph (i.e. the text) changes.
  useEffect(() => {
    if (!wf) {
      setLayout({ nodes: [], edges: [] });
      return;
    }
    let cancelled = false;
    layoutWorkflow(wf)
      .then((l) => {
        if (cancelled) return;
        setLayout(l);
        const key = `${activePath}:${wf.name}`;
        if (lastFit.current !== key) {
          lastFit.current = key;
          requestAnimationFrame(() => fit(0));
        } else if (keepRef.current) {
          // After an edit: same zoom, back in the middle.
          requestAnimationFrame(() => requestAnimationFrame(() => center()));
        }
      })
      .catch((e) => console.error("layout", e));
    return () => {
      cancelled = true;
    };
    // graphSource identifies the graph's text; wf is derived from it
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graphSource, wf?.name, activePath]);

  const taken = useMemo(() => {
    if (!wf || runPath !== activePath) return new Set<string>();
    const ended = result ? (result.ok ? (result.paused ? null : "end") : "fail") : null;
    return takenEdges(wf, visits, ended);
  }, [wf, visits, result, runPath, activePath]);

  const nodes: Node[] = useMemo(
    () => layout.nodes.map((n) => ({ ...n, selected: n.id === selection })),
    [layout.nodes, selection],
  );
  const edges: Edge[] = useMemo(
    () =>
      layout.edges.map((e) => {
        const d = e.data as FlowEdgeData;
        const isTaken = taken.has(`${d.edge.from}>${d.edge.to}`);
        return {
          ...e,
          selected: e.id === selectedEdge,
          data: { ...d, taken: isTaken },
          markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 },
          zIndex: isTaken ? 2 : 1,
        };
      }),
    [layout.edges, taken, selectedEdge],
  );

  const onConnect = useCallback(
    (c: Connection) => {
      if (!wf || !c.source || !c.target) return;
      const to = c.target.startsWith("ext:") ? c.target.slice(4) : c.target;
      void edit((doc) => connect(doc, wf.name, c.source, to));
    },
    [wf],
  );

  const onKeyDown = useCallback(
    (ev: React.KeyboardEvent) => {
      if ((ev.target as HTMLElement).closest("input, textarea, .monaco-editor")) return;
      if (ev.shiftKey && !ev.metaKey && !ev.ctrlKey && !ev.altKey && (ev.code === "Digit1" || ev.code === "Digit2")) {
        if (ev.code === "Digit1") fit();
        else center();
        ev.preventDefault();
        return;
      }
      if (!wf || (ev.key !== "Delete" && ev.key !== "Backspace")) return;
      if (selectedEdge) {
        const e = layout.edges.find((x) => x.id === selectedEdge);
        if (e) void edit((doc) => removeEdge(doc, wf.name, (e.data as FlowEdgeData).edge));
        setSelectedEdge(null);
        ev.preventDefault();
      } else if (selection && wf.nodes.some((n) => n.id === selection)) {
        void edit((doc) => deleteNode(doc, wf.name, selection)).then((ok) => ok && set({ selection: null }));
        ev.preventDefault();
      }
    },
    [wf, selectedEdge, selection, layout.edges, fit, center],
  );

  /** Where a palette state dropped at a screen point goes. */
  const placementAt = useCallback(
    (x: number, y: number, kind: StateKind): Placement => {
      const p = flow.screenToFlowPosition({ x, y });
      const hits = flow.getIntersectingNodes({ x: p.x, y: p.y, width: 1, height: 1 });
      const group = hits.find((h) => h.type === "parallel");
      const step = hits.find((h) => h.type === "step");
      if (group && !["parallel", "choice"].includes(kind)) {
        return step ? { after: step.id } : { into: group.id };
      }
      if (step) return { after: step.parentId ?? step.id };
      if (group) return { after: group.id };
      if (hits.some((h) => h.id === START) && wf?.nodes.some((n) => !n.parent)) {
        return { before: wf!.nodes.find((n) => !n.parent)!.id };
      }
      return { end: true };
    },
    [flow, wf],
  );

  /** The step under the pointer, when what is dragged is a call (tool method or agent). */
  const callTargetAt = useCallback(
    (ev: React.DragEvent): string | null => {
      const types = ev.dataTransfer.types;
      if (![CALL_MIME, AGENT_MIME, PROMPT_MIME, SCHEMA_MIME].some((m) => types.includes(m))) return null;
      const p = flow.screenToFlowPosition({ x: ev.clientX, y: ev.clientY });
      return flow.getIntersectingNodes({ x: p.x, y: p.y, width: 1, height: 1 }).find((h) => h.type === "step")?.id ?? null;
    },
    [flow],
  );

  const onDrop = useCallback(
    (ev: React.DragEvent) => {
      ev.preventDefault();
      set({ dropTarget: null });
      const kind = ev.dataTransfer.getData(DRAG_MIME) as StateKind;
      if (!kind || !wf) return;
      // A prompt runs through an agent: onto a step it sets (or adds) that
      // step's agent call; elsewhere it makes a new agent step.
      // A JSON Schema likewise becomes the `schema:` of that agent call.
      const prompt = payload<PromptDrag>(ev, PROMPT_MIME);
      const schema = payload<SchemaDrag>(ev, SCHEMA_MIME);
      if (prompt || schema) {
        const step = callTargetAt(ev);
        let to: PromptTarget;
        if (step) to = { step };
        else {
          const where = placementAt(ev.clientX, ev.clientY, "agent");
          const target = "after" in where ? wf.nodes.find((n) => n.id === where.after) : undefined;
          to = { placement: target?.parent ? { into: target.parent } : where };
        }
        if (prompt) void dropPrompt(prompt, wf.name, to);
        else void dropSchema(schema!, wf.name, to);
        return;
      }
      // A tool method or agent dropped onto a step becomes a call inside it.
      const into = callTargetAt(ev);
      if (into) {
        const c = payload<{ tool: string; method: string; file?: string }>(ev, CALL_MIME);
        const a = payload<{ agent: string; file?: string }>(ev, AGENT_MIME);
        const f = (file?: string) => (file ? get().library.find((l) => l.path === file) : undefined);
        if (c) void addToolCallToStep(f(c.file), c.tool, c.method, wf.name, into);
        else if (a) void addAgentCallToStep(f(a.file), a.agent, wf.name, into);
        return;
      }
      const where = placementAt(ev.clientX, ev.clientY, kind);
      // A step dropped onto a branch of a parallel group joins that group.
      const target = "after" in where ? wf.nodes.find((n) => n.id === where.after) : undefined;
      const placement: Placement =
        target?.parent && !["parallel", "choice"].includes(kind) ? { into: target.parent } : where;
      const call = payload<{ tool: string; method: string; file?: string }>(ev, CALL_MIME);
      const agent = payload<{ agent: string; file?: string }>(ev, AGENT_MIME);
      const lib = (file?: string) => (file ? get().library.find((l) => l.path === file) : undefined);
      if (call?.file) {
        const f = lib(call.file);
        if (f) void addToolCallStep(f, call.tool, call.method, wf.name, placement);
        return;
      }
      if (agent?.file) {
        const f = lib(agent.file);
        if (f) void addAgentStep(f, agent.agent, wf.name, placement);
        return;
      }
      void edit((doc) => {
        const method = call && doc.graph.declarations.find((d) => d.name === call!.tool)?.methods?.find((m) => m.name === call!.method);
        return insertState(doc, wf.name, kind, placement, call && method ? { tool: call.tool, method } : undefined, agent?.agent);
      }).then((ok) => agent && hintSchema(ok, agent.agent));
    },
    [wf, placementAt, callTargetAt],
  );

  if (!graph) {
    return <div className="canvas-empty">{activePath ? "Building the graph…" : "Open a .mh file to see its workflow."}</div>;
  }
  if (!wf) {
    return (
      <div className="canvas-empty">
        <p>
          This file declares no workflow or pipeline yet.
          <br />
          Use <b>+ Workflow</b> in the toolbar to add one.
        </p>
      </div>
    );
  }

  return (
    <div className={`canvas ${stale ? "stale" : ""}`} ref={wrapper} tabIndex={0} onKeyDown={onKeyDown}>
      <ImportsCard />
      {stale && <div className="stale-banner">The code has errors — the diagram shows the last version that parsed.</div>}
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodeClick={(_, n) => {
          setSelectedEdge(null);
          // A route is not a step: clicking it shows its declaration (RouteNode).
          const inert = n.id === START || n.id === END || n.id === FAIL || n.id.startsWith("ext:") || n.id.startsWith(ROUTE_PREFIX);
          set({ selection: inert ? null : n.id });
        }}
        onEdgeClick={(_, e) => {
          setSelectedEdge(e.id);
          set({ selection: null });
        }}
        onPaneClick={() => {
          setSelectedEdge(null);
          set({ selection: null });
        }}
        onConnect={stale ? undefined : onConnect}
        // A pipeline has no goto: a handle only snaps to End or Fail.
        isValidConnection={(c) => !wf || !c.target || transitionAllowed(wf, c.target.startsWith("ext:") ? c.target.slice(4) : c.target)}
        onDragOver={(ev) => {
          ev.preventDefault();
          ev.dataTransfer.dropEffect = "copy";
          const t = callTargetAt(ev);
          if (t !== get().dropTarget) {
            const types = ev.dataTransfer.types;
            set({ dropTarget: t, dropKind: types.includes(PROMPT_MIME) ? "prompt" : types.includes(SCHEMA_MIME) ? "schema" : "call" });
          }
        }}
        onDragLeave={(ev) => {
          if (!ev.currentTarget.contains(ev.relatedTarget as globalThis.Node | null)) set({ dropTarget: null });
        }}
        onDrop={stale ? undefined : onDrop}
        nodesDraggable={false}
        nodesConnectable={!stale}
        deleteKeyCode={null}
        selectionKeyCode={null}
        multiSelectionKeyCode={null}
        colorMode={scheme}
        minZoom={0.2}
        maxZoom={2}
      >
        <Background gap={20} size={1} />
        <Controls showInteractive={false} showFitView={false} position="top-right">
          <ViewportButtons onFit={() => fit()} onCenter={() => center()} keepCentered={keepCentered} onToggle={toggleKeepCentered} />
        </Controls>
      </ReactFlow>
    </div>
  );
}

export function WorkflowCanvas() {
  return (
    <ReactFlowProvider>
      <Canvas />
    </ReactFlowProvider>
  );
}
