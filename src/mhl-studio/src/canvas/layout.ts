// Workflow graph → positioned React Flow nodes/edges, laid out by ELK
// (layered, top-down, orthogonal edges — the Step Functions look). Layout is
// fully automatic: node positions are derived from the source on every
// change, never stored, so code and diagram cannot disagree.
import ELK, { type ElkExtendedEdge, type ElkNode } from "elkjs/lib/elk.bundled.js";
import type { Edge, Node } from "@xyflow/react";
import { END, FAIL, START, type GraphEdge, type GraphNode, type Route, type StageGroup, type Workflow } from "../lib/types";

/** An edge's label as drawn: a match/route arm's `_` reads "otherwise". */
export function edgeLabel(e: { kind: string; label?: string }): string | undefined {
  if (!e.label || (e.kind !== "match" && e.kind !== "route")) return e.label;
  return e.label.replace(/(^|\s)_$/, "$1otherwise");
}

/** The canvas id of a `route Name(...) { arms }`, drawn as a Choice state of its own. */
export const ROUTE_PREFIX = "route:";
export const routeId = (name: string) => `${ROUTE_PREFIX}${name}`;

/**
 * The guard part of a `goto Route(x)` edge's label: `mhl graph` joins the
 * guard and the arm's pattern as "guard → pattern" (flow.joinLabel).
 */
function routeGuard(label: string | undefined, r: Route): string {
  if (!label) return "";
  for (const a of r.arms) {
    if (label === a.pattern) return "";
    if (label.endsWith(` → ${a.pattern}`)) return label.slice(0, -(a.pattern.length + 3));
  }
  return label;
}

/** The routes some step runs `goto Route(x)` on — the only ones in the flow. */
export function usedRoutes(wf: Workflow): Set<string> {
  return new Set(wf.edges.flatMap((e) => (e.kind === "route" && e.via ? [e.via] : [])));
}

/**
 * The edges as drawn. `mhl graph` flattens a `goto Route(x)` into one
 * caller → target edge per arm; the canvas draws the route itself instead:
 * caller → route once (per guard), route → each arm target. A route no step
 * calls has no edges: nothing reaches it, so its arms are never taken (it
 * is drawn apart, its arms listed inside — see layoutWorkflow).
 */
export function drawnEdges(wf: Workflow): GraphEdge[] {
  if (wf.routes.length === 0) return wf.edges;
  const used = usedRoutes(wf);
  const routes = new Map(wf.routes.filter((r) => used.has(r.name)).map((r) => [r.name, r]));
  const out: GraphEdge[] = [];
  const seen = new Set<string>();
  for (const e of wf.edges) {
    const r = e.kind === "route" && e.via ? routes.get(e.via) : undefined;
    if (!r) {
      out.push(e);
      continue;
    }
    const guard = routeGuard(e.label, r);
    const key = `${e.from}>${r.name}>${guard}`;
    if (seen.has(key)) continue;
    seen.add(key);
    // From Start (an `entry route`), the edge is the start edge.
    out.push({ from: e.from, to: routeId(r.name), kind: e.from === START ? "next" : "goto", ...(guard ? { label: guard } : {}) });
  }
  for (const r of routes.values())
    for (const a of r.arms) {
      const to = a.fail ? FAIL : a.target;
      if (to) out.push({ from: routeId(r.name), to, kind: "route", label: a.pattern, via: r.name, span: a.targetSpan });
    }
  return out;
}

/**
 * Where each called route goes in ELK's model order (which also breaks
 * cycles): right after Start for an `entry route`, else right after the
 * first step that calls it — so its edges point down, not back up.
 */
function routesAfter(wf: Workflow, top: GraphNode[]): Map<string, Route[]> {
  const after = new Map<string, Route[]>();
  const calls = (from: string, r: Route) => wf.edges.some((e) => e.from === from && e.kind === "route" && e.via === r.name);
  for (const r of wf.routes) {
    const caller = calls(START, r) ? START : top.find((n) => calls(n.id, r))?.id;
    if (caller) after.set(caller, [...(after.get(caller) ?? []), r]);
  }
  return after;
}

const elk = new ELK();

const LAYERED: Record<string, string> = {
  "elk.algorithm": "layered",
  "elk.direction": "DOWN",
  "elk.edgeRouting": "ORTHOGONAL",
  "elk.layered.spacing.nodeNodeBetweenLayers": "52",
  "elk.spacing.nodeNode": "44",
  "elk.spacing.edgeNode": "22",
  "elk.spacing.edgeEdge": "14",
  "elk.layered.considerModelOrder.strategy": "NODES_AND_EDGES",
  // A loop is drawn upward on the goto to an earlier-declared step (the
  // `goto` back), not wherever the greedy cycle breaker cuts it.
  "elk.layered.cycleBreaking.strategy": "MODEL_ORDER",
  // Straight edges first (and the main line's weighted ones above all).
  "elk.layered.nodePlacement.strategy": "NETWORK_SIMPLEX",
  "elk.edgeLabels.inline": "true",
  "elk.layered.edgeLabels.sideSelection": "ALWAYS_UP",
};

/** Weight on an edge of the main line: ELK draws it straight before others. */
const STRAIGHT = { "elk.layered.priority.straightness": "10" };

/**
 * The workflow's main line: the nodes every Start → End path goes through
 * (Start, a choice, the step its branches merge into, End), in path order.
 * Empty when no path reaches End.
 */
export function mainLine(edges: [string, string][]): string[] {
  const succ = new Map<string, string[]>();
  for (const [a, b] of edges) if (a !== b) succ.set(a, [...(succ.get(a) ?? []), b]);
  // Breadth-first from Start, never through `without`: the distance of each
  // node reached (path order, for the main line), or whether End is.
  const walk = (without?: string) => {
    const dist = new Map([[START, 0]]);
    const todo = [START];
    for (let i = 0; i < todo.length; i++)
      for (const n of succ.get(todo[i]) ?? [])
        if (n !== without && !dist.has(n)) dist.set(n, dist.get(todo[i])! + 1), todo.push(n);
    return dist;
  };
  const dist = walk();
  if (!dist.has(END)) return [];
  return [...dist.keys()].filter((id) => id === START || id === END || !walk(id).has(END)).sort((a, b) => dist.get(a)! - dist.get(b)!);
}

/**
 * Links consecutive main-line nodes that no edge joins directly (a choice
 * and the step its branches merge into) with an invisible edge ELK keeps
 * straight, placed in the middle of the choice's edges: it holds the column
 * between the branches, which then spread on both sides. Main-line edges
 * that do exist are weighted straight instead.
 */
function linkMainLine(edges: ElkExtendedEdge[], line: string[]): ElkExtendedEdge[] {
  const out = [...edges];
  for (let i = 0; i + 1 < line.length; i++) {
    const [a, b] = [line[i], line[i + 1]];
    const direct = out.filter((e) => e.sources[0] === a && e.targets[0] === b);
    if (direct.length) {
      for (const e of direct) e.layoutOptions = { ...e.layoutOptions, ...STRAIGHT };
      continue;
    }
    const outs = out.flatMap((e, k) => (e.sources[0] === a ? [k] : []));
    const at = outs.length ? outs[Math.floor(outs.length / 2)] : out.length;
    out.splice(at, 0, { id: `${HIDDEN}${a}>${b}`, sources: [a], targets: [b], layoutOptions: STRAIGHT });
  }
  return out;
}

const HIDDEN = "hidden:";

/**
 * Puts every main-line node on one axis. ELK lines them up through the
 * invisible edges, but an edge leaves and enters a node at a port spread
 * along its side, so a node can sit a few pixels off. Shift it — unless
 * it would touch another node — and move the vertical end of each edge on
 * it along.
 */
function centerMainLine(root: ElkNode, line: string[]) {
  const kids = new Map((root.children ?? []).map((c) => [c.id, c]));
  const cx = (id: string) => (kids.get(id)!.x ?? 0) + (kids.get(id)!.width ?? 0) / 2;
  const xs = line.filter((id) => kids.has(id)).map(cx).sort((a, b) => a - b);
  if (xs.length < 2) return;
  const axis = xs[Math.floor(xs.length / 2)];
  const delta = new Map<string, number>();
  for (const id of line) {
    const n = kids.get(id);
    if (!n) continue;
    const d = axis - cx(id);
    if (Math.abs(d) < 0.5 || Math.abs(d) > 80) continue;
    const [x, y, w, h] = [(n.x ?? 0) + d, n.y ?? 0, n.width ?? 0, n.height ?? 0];
    const clash = [...kids.values()].some(
      (o) => o !== n && (o.y ?? 0) < y + h && y < (o.y ?? 0) + (o.height ?? 0) && (o.x ?? 0) < x + w + 12 && x < (o.x ?? 0) + (o.width ?? 0) + 12,
    );
    if (clash) continue;
    n.x = x;
    delta.set(id, d);
  }
  for (const e of root.edges ?? []) {
    const sec = e.sections?.[0];
    const ds = delta.get(e.sources[0]) ?? 0;
    const dt = delta.get(e.targets[0]) ?? 0;
    if (!sec || (!ds && !dt) || e.id.startsWith(HIDDEN)) continue;
    const bends = sec.bendPoints ?? [];
    const [s0, t0] = [sec.startPoint.x, sec.endPoint.x];
    sec.startPoint = { ...sec.startPoint, x: s0 + ds };
    sec.endPoint = { ...sec.endPoint, x: t0 + dt };
    if (bends.length) {
      if (ds && Math.abs(bends[0].x - s0) < 0.5) bends[0] = { ...bends[0], x: bends[0].x + ds };
      const k = bends.length - 1;
      if (dt && Math.abs(bends[k].x - t0) < 0.5) bends[k] = { ...bends[k], x: bends[k].x + dt };
    } else if (Math.abs(sec.startPoint.x - sec.endPoint.x) >= 0.5) {
      // A straight edge whose ends moved apart: a step halfway down.
      const mid = (sec.startPoint.y + sec.endPoint.y) / 2;
      bends.push({ x: sec.startPoint.x, y: mid }, { x: sec.endPoint.x, y: mid });
    }
    sec.bendPoints = bends;
  }
}

export const STEP_W = 236;
export const STEP_H = 68;
/** One call box inside a step; at most MAX_BOXES are drawn (then "+N more"). */
export const BOX_H = 26;
export const MAX_BOXES = 4;

/** A step's height: the header plus one box per call it makes. */
export function stepHeight(n: GraphNode): number {
  const calls = n.callSites?.length ?? 0;
  if (calls === 0) return STEP_H;
  const boxes = Math.min(calls, MAX_BOXES) + (calls > MAX_BOXES ? 1 : 0);
  return 40 + boxes * BOX_H + 6;
}
const TERM_W = 104;
const TERM_H = 36;

export interface Point {
  x: number;
  y: number;
}

export type StepNodeData = { node: GraphNode; workflow: string };
/** A stage (a fragment's header `stage Name`): a labelled box around its members. */
export type StageNodeData = { stage: StageGroup; workflow: string; steps: string[] };

/** The canvas id of a stage — its own name: `goto Name` edges target it. */
export const stageId = (name: string) => name;
/** Room above a stage's members for its title. */
const STAGE_PAD = "[top=40,left=20,bottom=20,right=20]";
export type TerminalNodeData = { role: "start" | "end" | "fail" };
export type ExternalNodeData = { name: string };
/** `used`: some step runs `goto Route(x)` — it is in the flow, with edges. */
export type RouteNodeData = { route: Route; workflow: string; used: boolean };
export type FlowEdgeData = { edge: GraphEdge; points: Point[]; labelAt?: Point; index: number };

export interface Layout {
  nodes: Node[];
  edges: Edge<FlowEdgeData>[];
}

function labelWidth(text: string) {
  return Math.min(220, 12 + text.length * 6.6);
}

export function edgeId(e: GraphEdge, i: number) {
  return `${e.from}>${e.to}#${i}`;
}

/** Height of a parallel group's merge bar (nodes.tsx ParallelNode). */
export const MERGE_H = 30;

/** A route node's height (nodes.tsx RouteNode): a header and its signature. */
export const ROUTE_H = 54;
/** One arm row inside an unused route's card. */
export const ARM_ROW_H = 20;

/** A route node's height: an unused one lists its arms inside, since it has no edges. */
export function routeHeight(r: Route, used: boolean): number {
  return used ? ROUTE_H : ROUTE_H + r.arms.length * ARM_ROW_H + 6;
}

/** Gap between the diagram and the column of unused routes on its right. */
const ASIDE_GAP = 72;

export async function layoutWorkflow(wf: Workflow): Promise<Layout> {
  const top = wf.nodes.filter((n) => !n.parent);
  const childrenOf = (id: string) => wf.nodes.filter((n) => n.parent === id);
  const drawn = drawnEdges(wf);
  const usesFail = drawn.some((e) => e.to === FAIL);
  const used = usedRoutes(wf);
  const after = routesAfter(wf, top);
  const routeNode = (r: Route): ElkNode => ({ id: routeId(r.name), width: STEP_W, height: ROUTE_H });

  const children: ElkNode[] = [{ id: START, width: TERM_W, height: TERM_H }];
  for (const r of after.get(START) ?? []) children.push(routeNode(r));
  // A stage is a box holding its members: each top-level node goes into its
  // stage's box (created where its first member comes, in model order).
  const stages = new Map((wf.stages ?? []).map((s) => [s.name, s]));
  const boxes = new Map<string, ElkNode>();
  const holder = (n: GraphNode): ElkNode[] => {
    const s = n.stage ? stages.get(n.stage) : undefined;
    if (!s) return children;
    let box = boxes.get(s.name);
    if (!box) {
      box = { id: stageId(s.name), layoutOptions: { "elk.padding": STAGE_PAD, "elk.spacing.nodeNode": "36" }, children: [] };
      boxes.set(s.name, box);
      children.push(box);
    }
    return box.children!;
  };
  for (const n of top) {
    const into = holder(n);
    if (n.kind === "parallel") {
      into.push({
        id: n.id,
        layoutOptions: {
          // A merge is drawn as a bar along the group's bottom edge.
          "elk.padding": `[top=44,left=18,bottom=${n.merge ? 18 + MERGE_H + 8 : 18},right=18]`,
          "elk.spacing.nodeNode": "24",
        },
        children: childrenOf(n.id).map((c) => ({ id: c.id, width: STEP_W, height: stepHeight(c) })),
      });
    } else {
      into.push({ id: n.id, width: STEP_W, height: stepHeight(n) });
    }
    for (const r of after.get(n.id) ?? []) into.push(routeNode(r));
  }
  for (const x of wf.externals) children.push({ id: `ext:${x}`, width: 170, height: 44 });
  children.push({ id: END, width: TERM_W, height: TERM_H });
  if (usesFail) children.push({ id: FAIL, width: TERM_W, height: TERM_H });

  const ext = new Set(wf.externals);
  const ref = (id: string) => (ext.has(id) ? `ext:${id}` : id);
  const edges: ElkExtendedEdge[] = drawn.map((e, i) => {
    const text = edgeLabel(e);
    return {
      id: edgeId(e, i),
      sources: [ref(e.from)],
      targets: [ref(e.to)],
      labels: text ? [{ text, width: labelWidth(text), height: 18 }] : [],
    };
  });

  // With stages, edges cross the boxes: ELK lays the hierarchy out as one
  // (INCLUDE_CHILDREN) and returns every edge in root coordinates, while a
  // node's position stays relative to its box — what React Flow expects of
  // a child. The main-line straightening only knows the root level.
  const nested = boxes.size > 0;
  const line = nested ? [] : mainLine(drawn.map((e) => [ref(e.from), ref(e.to)]));
  const root = await elk.layout({
    id: "root",
    layoutOptions: nested ? { ...LAYERED, "elk.hierarchyHandling": "INCLUDE_CHILDREN", "elk.json.edgeCoords": "ROOT" } : LAYERED,
    children,
    edges: nested ? edges : linkMainLine(edges, line),
  });
  if (!nested) centerMainLine(root, line);

  const byId = new Map(wf.nodes.map((n) => [n.id, n]));
  const routes = new Map(wf.routes.map((r) => [routeId(r.name), r]));
  const nodes: Node[] = [];
  // A route no step calls is outside the flow: a card in a column on the
  // diagram's right, its arms listed inside, no edges.
  const laidOut = root.children ?? [];
  let asideY = Math.min(...laidOut.map((c) => c.y ?? 0));
  const asideX = Math.max(...laidOut.map((c) => (c.x ?? 0) + (c.width ?? 0))) + ASIDE_GAP;
  for (const r of wf.routes.filter((r) => !used.has(r.name))) {
    const height = routeHeight(r, false);
    nodes.push({
      id: routeId(r.name),
      type: "route",
      position: { x: asideX, y: asideY },
      data: { route: r, workflow: wf.name, used: false } satisfies RouteNodeData,
      style: { height },
      draggable: false,
      connectable: false,
      deletable: false,
    });
    asideY += height + 24;
  }
  const emit = (c: ElkNode, parentId?: string) => {
    const pos = { x: c.x ?? 0, y: c.y ?? 0 };
    const inBox = parentId ? { parentId, extent: "parent" as const } : {};
    const stage = boxes.has(c.id) ? stages.get(c.id) : undefined;
    if (stage) {
      // Every step that runs in it — a group's branches and merge too — for its run status.
      const steps = stage.members.flatMap((m) => {
        const g = byId.get(m);
        return [m, ...childrenOf(m).map((k) => k.id), ...(g?.merge ? [g.merge.stepName] : [])];
      });
      nodes.push({
        id: c.id,
        type: "stage",
        position: pos,
        data: { stage, workflow: wf.name, steps } satisfies StageNodeData,
        style: { width: c.width, height: c.height },
        draggable: false,
        selectable: false,
        deletable: false,
        zIndex: -1,
      });
      for (const k of c.children ?? []) emit(k, c.id);
      return;
    }
    const route = routes.get(c.id);
    if (route) {
      nodes.push({ id: c.id, type: "route", position: pos, ...inBox, data: { route, workflow: wf.name, used: true } satisfies RouteNodeData, draggable: false, connectable: false, deletable: false });
      return;
    }
    if (c.id === START || c.id === END || c.id === FAIL) {
      const role = c.id === START ? "start" : c.id === END ? "end" : "fail";
      nodes.push({ id: c.id, type: "terminal", position: pos, data: { role } satisfies TerminalNodeData, draggable: false, connectable: c.id !== START, deletable: false });
      return;
    }
    if (c.id.startsWith("ext:")) {
      nodes.push({ id: c.id, type: "external", position: pos, data: { name: c.id.slice(4) } satisfies ExternalNodeData, draggable: false, deletable: false });
      return;
    }
    const n = byId.get(c.id)!;
    if (n.kind === "parallel") {
      nodes.push({
        id: n.id,
        type: "parallel",
        position: pos,
        ...inBox,
        data: { node: n, workflow: wf.name } satisfies StepNodeData,
        style: { width: c.width, height: c.height },
        draggable: false,
      });
      for (const k of c.children ?? []) {
        nodes.push({
          id: k.id,
          type: "step",
          parentId: n.id,
          extent: "parent",
          position: { x: k.x ?? 0, y: k.y ?? 0 },
          data: { node: byId.get(k.id)!, workflow: wf.name } satisfies StepNodeData,
          style: { height: k.height },
          draggable: false,
        });
      }
    } else {
      nodes.push({ id: n.id, type: "step", position: pos, ...inBox, data: { node: n, workflow: wf.name } satisfies StepNodeData, style: { height: c.height }, draggable: false });
    }
  };
  for (const c of laidOut) emit(c);

  const laid = new Map((root.edges ?? []).map((e) => [e.id, e]));
  const flowEdges: Edge<FlowEdgeData>[] = drawn.map((e, i) => {
    const id = edgeId(e, i);
    const le = laid.get(id);
    const sec = le?.sections?.[0];
    const points: Point[] = sec ? [sec.startPoint, ...(sec.bendPoints ?? []), sec.endPoint] : [];
    const lab = le?.labels?.[0];
    return {
      id,
      source: ref(e.from),
      target: ref(e.to),
      type: "elk",
      data: { edge: e, points, labelAt: lab && lab.x != null ? { x: lab.x + (lab.width ?? 0) / 2, y: lab.y! + (lab.height ?? 0) / 2 } : undefined, index: i },
      deletable: true,
    };
  });
  return { nodes, edges: flowEdges };
}

/**
 * Edges a run actually took, as "from>to" keys over top-level ids: the
 * visit sequence with parallel branches folded into their group, plus the
 * start edge and the terminal edge for a run that ended. A hop a route
 * made (`goto Route(x)`) is drawn through the route's node: both its legs.
 */
export function takenEdges(wf: Workflow, visits: string[], ended: "end" | "fail" | null): Set<string> {
  const out = new Set<string>();
  // `goto Stage` is drawn into the stage's box: the hop into its first member took it.
  const entryOf = new Map((wf.stages ?? []).map((s) => [s.members[0], s.name]));
  for (const key of hops(wf, visits, ended)) {
    out.add(key);
    const [from, to] = key.split(">");
    const stage = entryOf.get(to);
    if (stage && wf.edges.some((e) => e.from === from && e.to === stage)) out.add(`${from}>${stage}`);
    for (const e of wf.edges)
      if (e.kind === "route" && e.via && e.from === from && e.to === to) {
        out.add(`${from}>${routeId(e.via)}`);
        out.add(`${routeId(e.via)}>${to}`);
      }
  }
  return out;
}

function hops(wf: Workflow, visits: string[], ended: "end" | "fail" | null): Set<string> {
  const parent = new Map(wf.nodes.filter((n) => n.parent).map((n) => [n.id, n.parent!]));
  // A group's merge runs as its own step ("Fan.merge"): it is part of the group.
  for (const n of wf.nodes) if (n.merge) parent.set(n.merge.stepName, n.id);
  // An `entry route` runs as a step of its own name: its node is the route's.
  for (const r of wf.routes) if (r.entry) parent.set(r.name, routeId(r.name));
  const seq: string[] = [];
  for (const v of visits) {
    const id = parent.get(v) ?? v;
    // a group's branches all map to the group: count it once per entry
    if (parent.has(v) && seq[seq.length - 1] === id) continue;
    seq.push(id);
  }
  const out = new Set<string>();
  if (seq.length) out.add(`${START}>${seq[0]}`);
  for (let i = 1; i < seq.length; i++) out.add(`${seq[i - 1]}>${seq[i]}`);
  if (ended && seq.length) out.add(`${seq[seq.length - 1]}>${ended === "end" ? END : FAIL}`);
  return out;
}
