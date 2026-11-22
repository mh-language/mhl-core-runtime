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

/** A fallthrough edge: the cycle breaker reverses another edge of its loop first. */
const NEXT = { "elk.layered.priority.direction": "10" };

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
 * The workflow's spine: the path a run takes when no condition holds —
 * from Start, each node's fallthrough, else its unconditional goto, else
 * its only edge to End. Empty when that path does not reach End (a choice
 * with no default, a loop back). With stages it is the phases in order, the
 * conditional exits (a rejection, a revision) hanging off it. `from` and
 * `leaves` walk it inside one stage instead: from its first step out.
 */
export function spine(edges: GraphEdge[], from: string = START, leaves?: (id: string) => boolean): string[] {
  const out = new Map<string, GraphEdge[]>();
  for (const e of edges) if (e.from !== e.to && e.kind !== "fail") out.set(e.from, [...(out.get(e.from) ?? []), e]);
  const path = [from];
  const seen = new Set(path);
  // From inside a stage, the spine ends where it leaves it (`leaves`).
  for (let at = from; at !== END && !(at !== from && leaves?.(at)); ) {
    const es = out.get(at) ?? [];
    const toEnd = es.filter((e) => e.to === END);
    const next = es.find((e) => e.kind === "next") ?? es.find((e) => !e.label && e.kind !== "match" && e.kind !== "route") ?? (toEnd.length === 1 ? toEnd[0] : undefined);
    if (!next || seen.has(next.to)) return [];
    at = next.to;
    seen.add(at);
    path.push(at);
  }
  return path;
}

/**
 * An open stage's spine: the step that leads into it (its fallthrough or
 * unconditional goto — the first a run reaches, by `reached`: not a
 * revision looping back), the stage, then its own spine from its first step
 * to where it leaves.
 */
function focusSpine(edges: GraphEdge[], stage: StageGroup, inside: (id: string) => boolean, reached: (id: string) => number): string[] {
  const first = stage.members[0];
  const into = edges.filter((e) => !inside(e.from) && (e.to === stage.name || e.to === first) && (e.kind === "next" || !e.label));
  const entry = into.sort((a, b) => reached(a.from) - reached(b.from))[0];
  const own = spine(edges, first, (id) => !inside(id));
  if (!entry) return own;
  return entry.to === stage.name ? [entry.from, stage.name, ...own] : [entry.from, ...own];
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

interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Every laid-out node's box in root coordinates: a child's position is relative to its parent's. */
function absoluteRects(root: ElkNode): Map<string, Rect> {
  const out = new Map<string, Rect>();
  const place = (c: ElkNode, ox: number, oy: number) => {
    const x = ox + (c.x ?? 0);
    const y = oy + (c.y ?? 0);
    out.set(c.id, { x, y, w: c.width ?? 0, h: c.height ?? 0 });
    for (const k of c.children ?? []) place(k, x, y);
  };
  for (const c of root.children ?? []) place(c, 0, 0);
  return out;
}

type Side = "NORTH" | "SOUTH" | "WEST" | "EAST";
/** Separates a port's node from the rest of its id: `Node::out:SOUTH`. */
const PORT = "::";
/** The node an edge end belongs to: its port's node, or the node itself. */
const endNode = (id: string) => {
  const i = id.indexOf(PORT);
  return i < 0 ? id : id.slice(0, i);
};

/**
 * Which side of its nodes each edge leaves and enters, as ELK ports (one
 * per node, direction and side, centred: edges on the same side meet there —
 * except edges leaving by a side, which get a port each: sharing one, two
 * gotos back up from the same step would climb together and then fork apart
 * with a jog; with their own, ELK spreads them along the side and each climbs
 * in its own lane).
 * - the spine (`line`) runs straight down, bottom centre to top centre;
 * - an exit off the spine (a rejection, a revision) leaves a spine node from
 *   the side its target is on — exits alternate left and right in the order
 *   they are declared (`declared`), so a target keeps its side whichever
 *   phase sends the run there, in every view —
 *   and comes back into the spine from that side too;
 * - a goto back up leaves and enters from the side (its source's, else the
 *   left), so it climbs beside the diagram instead of over the top;
 * - a jump ahead along the spine (a condition skipping a step) runs down
 *   its right side, joining the spine at the top of where it lands;
 * - anything else goes bottom to top.
 */
function assignPorts(children: ElkNode[], edges: ElkExtendedEdge[], line: string[], declared: (id: string) => number) {
  const nodes = new Map<string, ElkNode>();
  const order = new Map<string, number>();
  const walk = (c: ElkNode) => {
    nodes.set(c.id, c);
    order.set(c.id, order.size);
    for (const k of c.children ?? []) walk(k);
  };
  children.forEach(walk);
  const onLine = new Set(line);
  const linePairs = new Set(line.slice(1).map((id, i) => `${line[i]}>${id}`));
  // ELK reverses an edge to an earlier node (MODEL_ORDER cycle breaking): drawn going up.
  const forward = (s: string, t: string) => order.get(t)! > order.get(s)!;
  const sideOf = new Map<string, Side>();
  const offs = new Set<string>();
  for (const e of edges) {
    const [s, t] = [e.sources[0], e.targets[0]];
    if (nodes.has(s) && nodes.has(t) && onLine.has(s) && !onLine.has(t) && forward(s, t)) offs.add(t);
  }
  // In declaration order, so an exit keeps its side in every view.
  [...offs].sort((a, b) => declared(a) - declared(b) || order.get(a)! - order.get(b)!).forEach((t, i) => sideOf.set(t, i % 2 ? "EAST" : "WEST"));
  // ELK keeps the model order within a layer: a left exit placed after a
  // spine node it may share a layer with is drawn on its right. Move it
  // before the spine nodes just above it in the order that no edge joins it to.
  const topOf = new Map<string, string>();
  const mark = (c: ElkNode, top: string) => {
    topOf.set(c.id, top);
    for (const k of c.children ?? []) mark(k, top);
  };
  for (const c of children) mark(c, c.id);
  const joined = new Set(edges.flatMap((e) => [`${topOf.get(e.sources[0])}|${topOf.get(e.targets[0])}`, `${topOf.get(e.targets[0])}|${topOf.get(e.sources[0])}`]));
  for (const [t, side] of sideOf) {
    if (side !== "WEST") continue;
    let at = children.findIndex((c) => c.id === t);
    if (at < 0) continue;
    // Never into another band (a boxed stage's partition): only the order within one counts.
    const band = (c: ElkNode) => c.layoutOptions?.["elk.partitioning.partition"];
    while (at > 0 && onLine.has(children[at - 1].id) && !joined.has(`${children[at - 1].id}|${t}`) && band(children[at - 1]) === band(children[at])) {
      [children[at - 1], children[at]] = [children[at], children[at - 1]];
      at--;
    }
  }
  const port = (id: string, dir: "in" | "out", side: Side, own?: string) => {
    const n = nodes.get(id)!;
    const pid = `${id}${PORT}${dir}:${side}${own ? `:${own}` : ""}`;
    if (!n.ports?.some((p) => p.id === pid)) {
      n.layoutOptions = { ...n.layoutOptions, "elk.portConstraints": "FIXED_SIDE", "elk.portAlignment.default": "CENTER" };
      n.ports = [...(n.ports ?? []), { id: pid, width: 0, height: 0, layoutOptions: { "elk.port.side": side } }];
    }
    return pid;
  };
  for (const e of edges) {
    const [s, t] = [e.sources[0], e.targets[0]];
    if (!nodes.has(s) || !nodes.has(t)) continue;
    let out: Side = "SOUTH";
    let into: Side = "NORTH";
    if (linePairs.has(`${s}>${t}`)) e.layoutOptions = { ...e.layoutOptions, ...STRAIGHT };
    else if (!e.id.startsWith(HIDDEN)) {
      if (!forward(s, t)) out = into = sideOf.get(s) ?? sideOf.get(t) ?? "WEST";
      // A step the spine skips (a condition jumping ahead): down its right side, into the top.
      else if (onLine.has(s) && onLine.has(t)) out = "EAST";
      else {
        if (onLine.has(s) && sideOf.has(t)) out = sideOf.get(t)!;
        if (onLine.has(t) && sideOf.has(s)) into = sideOf.get(s)!;
      }
    }
    e.sources = [port(s, "out", out, out === "WEST" || out === "EAST" ? e.id : undefined)];
    // Into the top, the edge keeps to the node: ELK spreads several along it.
    if (into !== "NORTH") e.targets = [port(t, "in", into)];
  }
}

/**
 * Puts every main-line node on one axis. ELK lines them up through the
 * invisible edges, but a node can still sit a few pixels off. Shift it — unless
 * it would touch another node — and move the vertical end of each edge on
 * it along.
 */
function centerMainLine(root: ElkNode, line: string[], column?: number) {
  const kids = new Map((root.children ?? []).map((c) => [c.id, c]));
  const cx = (id: string) => (kids.get(id)!.x ?? 0) + (kids.get(id)!.width ?? 0) / 2;
  const xs = line.filter((id) => kids.has(id)).map(cx).sort((a, b) => a - b);
  if (column === undefined && xs.length < 2) return;
  const axis = column ?? xs[Math.floor(xs.length / 2)];
  const delta = new Map<string, number>();
  for (const id of line) {
    const n = kids.get(id);
    if (!n) continue;
    const d = axis - cx(id);
    // Start and End may travel further: only vertical edge ends meet them.
    // So may what sits above and below an open stage, onto the column of its steps.
    if (Math.abs(d) < 0.5 || Math.abs(d) > (column !== undefined || id === START || id === END ? 400 : 80)) continue;
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
    const ds = delta.get(endNode(e.sources[0])) ?? 0;
    const dt = delta.get(endNode(e.targets[0])) ?? 0;
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
/** How far a step's ask-again loop reaches out of its right side. */
const LOOP_W = 22;
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

/** `failsWhen`: the conditions of the step's own `fail(...)`, listed on its ✕ badge. */
export type StepNodeData = { node: GraphNode; workflow: string; failsWhen?: string[] };
/**
 * A stage (a fragment's header `stage Name`): a labelled box around its
 * members — or, `collapsed`, one card standing for them all, which opens
 * the stage's own diagram (StageView.focus).
 */
export type StageNodeData = { stage: StageGroup; workflow: string; steps: string[]; collapsed?: boolean };
/**
 * Outside the focused stage: a step or route its steps come from or go to,
 * drawn small — clicking it goes back to the whole workflow.
 */
export type BoundaryNodeData = { id: string; name: string; kind: "step" | "route" };
/** A boundary node's size. */
const BOUNDARY_W = 170;
const BOUNDARY_H = 44;

/** The canvas id of a stage — its own name: `goto Name` edges target it. */
export const stageId = (name: string) => name;
/** An open stage's box around its steps: room for its title above them, and around. */
const BOX_PAD = { top: 40, side: 44, bottom: 20 };
/** Between layers in an open stage's diagram: room for the box's padding and an edge label. */
const STAGE_GAP = 84;

/**
 * Wraps an open stage's steps (`members`, laid out flat) in its box: a
 * node around them, in their place among the root's children, its children
 * moved into it at positions relative to it — what React Flow expects of a
 * child. Edges stay in root coordinates.
 */
function boxAround(root: ElkNode, id: string, members: Set<string>, boxes: Map<string, ElkNode>) {
  const all = root.children ?? [];
  const inside = all.filter((c) => members.has(c.id));
  const x0 = Math.min(...inside.map((c) => c.x ?? 0)) - BOX_PAD.side;
  const y0 = Math.min(...inside.map((c) => c.y ?? 0)) - BOX_PAD.top;
  const x1 = Math.max(...inside.map((c) => (c.x ?? 0) + (c.width ?? 0))) + BOX_PAD.side;
  const y1 = Math.max(...inside.map((c) => (c.y ?? 0) + (c.height ?? 0))) + BOX_PAD.bottom;
  const box: ElkNode = {
    id,
    x: x0,
    y: y0,
    width: x1 - x0,
    height: y1 - y0,
    children: inside.map((c) => ({ ...c, x: (c.x ?? 0) - x0, y: (c.y ?? 0) - y0 })),
  };
  const at = all.findIndex((c) => members.has(c.id));
  root.children = [...all.slice(0, at).filter((c) => !members.has(c.id)), box, ...all.slice(at).filter((c) => !members.has(c.id))];
  boxes.set(id, box);
}
/** A collapsed stage card: its title, then one row per member (at most MAX_BOXES, then "+N more"). */
export const STAGE_HEAD = 52;
export const STAGE_ROW_H = 20;
export function stageCardHeight(s: StageGroup): number {
  const rows = Math.min(s.members.length, MAX_BOXES) + (s.members.length > MAX_BOXES ? 1 : 0);
  return STAGE_HEAD + rows * STAGE_ROW_H + 12;
}

/**
 * How stages are drawn. By default each is one card (the workflow's phases,
 * not their steps); `focus` draws one stage's steps alone, with what they
 * come from and go to around them; `expanded` draws every stage's steps,
 * each stage a box around them.
 */
export interface StageView {
  focus?: string | null;
  expanded?: boolean;
}

/**
 * The stage each drawn id belongs to: a stage's members, their branches,
 * the stage's own name (a `goto Stage` target), and a route only that
 * stage's steps call.
 */
export function stageGroups(wf: Workflow): Map<string, string> {
  const out = new Map<string, string>();
  if (!wf.stages?.length) return out;
  for (const s of wf.stages) out.set(s.name, s.name);
  for (const n of wf.nodes) if (n.stage) out.set(n.id, n.stage);
  for (const n of wf.nodes) if (n.parent && out.has(n.parent)) out.set(n.id, out.get(n.parent)!);
  for (const r of wf.routes) {
    const callers = new Set(wf.edges.filter((e) => e.kind === "route" && e.via === r.name).map((e) => out.get(e.from) ?? e.from));
    if (callers.size === 1) {
      const [g] = callers;
      if (out.get(g) === g) out.set(routeId(r.name), g);
    }
  }
  return out;
}

/**
 * What a drawn id stands for under `view`: the card of the collapsed stage
 * it belongs to, else itself. The identity when every stage is expanded.
 */
export function stageRep(wf: Workflow, view: StageView = {}): (id: string) => string {
  const focus = activeFocus(wf, view);
  if (view.expanded && !focus) return (id) => id;
  const group = stageGroups(wf);
  return (id) => {
    const g = group.get(id);
    return g && g !== focus ? stageId(g) : id;
  };
}

/** The focused stage, when the workflow has it. */
function activeFocus(wf: Workflow, view: StageView): string | null {
  return view.focus && wf.stages?.some((s) => s.name === view.focus) ? view.focus : null;
}

/**
 * The edges with each end replaced by what stands for it (`rep`): the edges
 * inside a collapsed stage dropped, those that became the same hop merged
 * into one, their conditions joined. `keep` filters the original edges first.
 */
function collapseEdges(edges: GraphEdge[], rep: (id: string) => string, keep: (e: GraphEdge) => boolean): { edges: GraphEdge[]; merged: Set<GraphEdge> } {
  const out: GraphEdge[] = [];
  const merged = new Set<GraphEdge>();
  const byKey = new Map<string, { edge: GraphEdge; labels: string[] }>();
  for (const e of edges) {
    if (!keep(e)) continue;
    const [from, to] = [rep(e.from), rep(e.to)];
    if (from === e.from && to === e.to) {
      out.push(e);
      continue;
    }
    if (from === to) continue; // inside one collapsed stage
    // A guard's fail() stays one: not drawn, like a step's own.
    const key = `${from}>${to}${e.kind === "fail" ? ">fail" : ""}`;
    const label = edgeLabel(e) ?? "";
    const seen = byKey.get(key);
    if (seen) {
      if (label && !seen.labels.includes(label)) seen.labels.push(label);
      if (e.kind === "next") seen.edge.kind = "next";
      continue;
    }
    const edge: GraphEdge = { from, to, kind: e.kind };
    byKey.set(key, { edge, labels: label ? [label] : [] });
    merged.add(edge);
    out.push(edge);
  }
  for (const { edge, labels } of byKey.values()) {
    if (labels.length === 1) edge.label = labels[0];
    else if (labels.length > 1) edge.label = labels.join(" | ");
  }
  return { edges: out, merged };
}
export type TerminalNodeData = { role: "start" | "end" | "fail" };
export type ExternalNodeData = { name: string };
/** `used`: some step runs `goto Route(x)` — it is in the flow, with edges. */
export type RouteNodeData = { route: Route; workflow: string; used: boolean };
export type FlowEdgeData = {
  edge: GraphEdge;
  points: Point[];
  labelAt?: Point;
  index: number;
  /** The label as drawn — shortened, or a summary (`3 conditions`, `↻`); edge.label is the full one. */
  text?: string;
  /** The full condition(s) the drawn label stands for, shown on hover. */
  title?: string;
};

/** Labels longer than this are cut, the full text kept for the tooltip. */
const LABEL_MAX = 34;
const shorten = (text: string) => (text.length > LABEL_MAX ? `${text.slice(0, LABEL_MAX - 1)}…` : text);

/**
 * The top-level nodes by how many hops a run needs to reach them from Start
 * (breadth-first, a goto to a stage entering its first member), declaration
 * order among equals, unreached ones last. A stage moves as one unit, at its
 * nearest member, its members kept in the order they are written. ELK takes this as its model order,
 * which also decides which edge of a cycle points back: a step declared early
 * but only ever reached late (a shared revision step in the entry fragment)
 * is then drawn after its callers, not above the whole diagram.
 */
export function runOrder(wf: Workflow, top: GraphNode[], first?: (n: GraphNode) => boolean): GraphNode[] {
  const parentOf = new Map(wf.nodes.filter((n) => n.parent).map((n) => [n.id, n.parent!]));
  const entry = new Map((wf.stages ?? []).map((s) => [s.name, s.members[0]]));
  const norm = (id: string) => parentOf.get(entry.get(id) ?? id) ?? entry.get(id) ?? id;
  const next = new Map<string, string[]>();
  for (const e of wf.edges) {
    const from = norm(e.from);
    next.set(from, [...(next.get(from) ?? []), norm(e.to)]);
  }
  const dist = new Map([[START, 0]]);
  const queue = [START];
  for (let i = 0; i < queue.length; i++)
    for (const to of next.get(queue[i]) ?? [])
      if (!dist.has(to)) dist.set(to, dist.get(queue[i])! + 1), queue.push(to);
  const stageHops = new Map<string, number>();
  for (const n of top) if (n.stage) stageHops.set(n.stage, Math.min(stageHops.get(n.stage) ?? Infinity, dist.get(n.id) ?? Infinity));
  const hops = (n: GraphNode) => (n.stage ? stageHops.get(n.stage)! : (dist.get(n.id) ?? Infinity));
  // Among equals, `first` (the spine) before the rest: a phase before the
  // exits reached as early, which would otherwise sit between two phases.
  const rank = (n: GraphNode) => (first?.(n) ? 0 : 1);
  return [...top].sort((a, b) => hops(a) - hops(b) || rank(a) - rank(b)); // stable: declaration order among equals
}

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

export async function layoutWorkflow(wf: Workflow, view: StageView = {}): Promise<Layout> {
  const tops = wf.nodes.filter((n) => !n.parent);
  const childrenOf = (id: string) => wf.nodes.filter((n) => n.parent === id);
  const isTop = new Set(tops.map((n) => n.id));
  const stages = new Map((wf.stages ?? []).map((s) => [s.name, s]));
  // Collapsed stages (the default) and a focused one redraw the edges: each
  // end becomes what stands for it — the card of a collapsed stage — and a
  // focused stage keeps only the edges that touch it.
  const focus = activeFocus(wf, view);
  const collapse = stages.size > 0 && (!view.expanded || !!focus);
  const group = collapse ? stageGroups(wf) : new Map<string, string>();
  const rep = stageRep(wf, view);
  const inFocus = (id: string) => !focus || group.get(id) === focus;
  const { edges: all, merged } = collapse
    ? collapseEdges(drawnEdges(wf), rep, (e) => inFocus(e.from) || inFocus(e.to))
    : { edges: drawnEdges(wf), merged: new Set<GraphEdge>() };
  // A step that asks again (`goto` itself) gets a short loop drawn by hand,
  // and a guard's `fail(...)` no edge at all: the step's ✕ badge already says
  // it can fail, and lists the conditions on hover. Neither is routed across
  // the diagram. A match/route arm to Fail is still an edge to the terminal.
  const loops = new Map<string, GraphEdge[]>();
  const failsWhen = new Map<string, string[]>();
  const drawn: GraphEdge[] = [];
  for (const e of all) {
    if (e.from === e.to && isTop.has(e.from)) loops.set(e.from, [...(loops.get(e.from) ?? []), e]);
    else if (e.kind === "fail" && e.to === FAIL) failsWhen.set(e.from, [...(failsWhen.get(e.from) ?? []), e.label || "always"]);
    else drawn.push(e);
  }
  const usesFail = drawn.some((e) => e.to === FAIL);
  // How soon a run reaches each drawn id (a stage card: its first member), to tell an open stage's way in from a loop back.
  const reachedAt = new Map<string, number>();
  runOrder(wf, tops).forEach((n, i) => {
    for (const id of [n.id, rep(n.id)]) if (!reachedAt.has(id)) reachedAt.set(id, i);
  });
  // The stages drawn as boxes (the open one, or every one): no ELK node — their
  // steps are laid out flat, in a band of their own, and the box is drawn
  // around them afterwards (boxAround). A `goto Stage` lands on its first step.
  const boxed = focus ? [focus] : view.expanded ? [...stages.keys()] : [];
  const memberOf = boxed.length ? stageGroups(wf) : new Map<string, string>();
  const boxOf = (id: string) => {
    const g = memberOf.get(id);
    return g && g !== id && boxed.includes(g) ? g : undefined;
  };
  const firstOf = (id: string) => (boxed.includes(id) ? stages.get(id)!.members[0] : id);
  const flowing = drawn.map((e) => (boxed.includes(e.to) ? { ...e, to: firstOf(e.to) } : e));
  const path = focus ? focusSpine(flowing, stages.get(focus)!, inFocus, (id) => reachedAt.get(id) ?? Infinity) : spine(flowing);
  const onSpine = new Set(path);
  const top = runOrder(wf, tops, (n) => onSpine.has(rep(n.id)));
  // Focused, what lies outside the stage is drawn only when an edge reaches it.
  const touched = new Set(drawn.flatMap((e) => [e.from, e.to]));
  const shown = (id: string) => !focus || touched.has(id);
  const stepData = (n: GraphNode): StepNodeData => ({ node: n, workflow: wf.name, ...(failsWhen.has(n.id) ? { failsWhen: failsWhen.get(n.id) } : {}) });
  const used = usedRoutes(wf);
  const after = routesAfter(wf, top);
  const routeNode = (r: Route): ElkNode => ({ id: routeId(r.name), width: STEP_W, height: ROUTE_H });

  const children: ElkNode[] = [];
  if (shown(START)) children.push({ id: START, width: TERM_W, height: TERM_H });
  // Collapsed stages, each one card (at its first member, in model order);
  // and, focused, the steps and routes outside the stage, small.
  const cards = new Set<string>();
  const boundary = new Map<string, BoundaryNodeData>();
  /** Places what stands for `id` when it is not drawn itself; false when it is. */
  const elsewhere = (id: string, kind: BoundaryNodeData["kind"], name: string): boolean => {
    const r = rep(id);
    if (r !== id) {
      if (!cards.has(r) && shown(r)) {
        cards.add(r);
        children.push({ id: r, width: STEP_W, height: stageCardHeight(stages.get(r)!) });
      }
      return true;
    }
    if (inFocus(id)) return false;
    if (touched.has(id) && !boundary.has(id)) {
      boundary.set(id, { id, name, kind });
      children.push({ id, width: BOUNDARY_W, height: BOUNDARY_H });
    }
    return true;
  };
  const placeRoute = (r: Route, into: ElkNode[]) => {
    if (!elsewhere(routeId(r.name), "route", r.name)) into.push(routeNode(r));
  };
  for (const r of after.get(START) ?? []) placeRoute(r, children);
  for (const n of top) {
    if (elsewhere(n.id, "step", n.id)) {
      for (const r of after.get(n.id) ?? []) placeRoute(r, children);
      continue;
    }
    const into = children;
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
    for (const r of after.get(n.id) ?? []) placeRoute(r, into);
  }
  for (const x of wf.externals) if (shown(x)) children.push({ id: `ext:${x}`, width: 170, height: 44 });
  if (shown(END)) children.push({ id: END, width: TERM_W, height: TERM_H });
  if (usesFail) children.push({ id: FAIL, width: TERM_W, height: TERM_H });

  const ext = new Set(wf.externals);
  const ref = (id: string) => (ext.has(id) ? `ext:${id}` : id);
  const textOf = (e: GraphEdge) => {
    const t = edgeLabel(e);
    return t ? shorten(t) : t;
  };
  const entryOf = (id: string) => ref(firstOf(id));
  const edges: ElkExtendedEdge[] = drawn.map((e, i) => {
    const text = textOf(e);
    return {
      id: edgeId(e, i),
      sources: [ref(e.from)],
      targets: [entryOf(e.to)],
      labels: text ? [{ text, width: labelWidth(text), height: 18 }] : [],
      // A fallthrough keeps pointing down: a loop through it is cut on the
      // goto back instead.
      ...(e.kind === "next" ? { layoutOptions: NEXT } : {}),
    };
  });

  const spineIds = path.map(ref);

  const line = spineIds.length ? spineIds : mainLine(flowing.map((e) => [ref(e.from), ref(e.to)]));
  const elkEdges = linkMainLine(edges, line);
  // Each boxed stage's steps keep a band of layers of their own (ELK
  // partitions), in model order — a new band wherever the stage changes, End
  // and Fail in the last — so the box drawn around them holds nothing else.
  if (boxed.length) {
    let band = 0;
    let at: string | undefined = boxOf(children[0]?.id ?? "") ?? "";
    for (const c of children) {
      const here = boxOf(c.id) ?? "";
      if (here !== at) band++;
      at = here;
      c.layoutOptions = { ...c.layoutOptions, "elk.partitioning.partition": String(c.id === END || c.id === FAIL ? children.length : band) };
    }
  }
  const declaredAt = new Map<string, number>();
  wf.nodes.forEach((n, i) => {
    if (!declaredAt.has(n.id)) declaredAt.set(n.id, i);
    if (n.stage && !declaredAt.has(stageId(n.stage))) declaredAt.set(stageId(n.stage), i);
  });
  assignPorts(children, elkEdges, spineIds, (id) => declaredAt.get(id) ?? Infinity);
  const root = await elk.layout({
    id: "root",
    layoutOptions: boxed.length ? { ...LAYERED, "elk.partitioning.activate": "true", "elk.layered.spacing.nodeNodeBetweenLayers": String(STAGE_GAP) } : LAYERED,
    children,
    edges: elkEdges,
  });
  centerMainLine(root, line);
  const boxes = new Map<string, ElkNode>();
  for (const name of boxed) {
    const members = new Set((root.children ?? []).filter((c) => boxOf(c.id) === name).map((c) => c.id));
    if (members.size) boxAround(root, stageId(name), members, boxes);
  }

  const byId = new Map(wf.nodes.map((n) => [n.id, n]));
  const routes = new Map(wf.routes.map((r) => [routeId(r.name), r]));
  const nodes: Node[] = [];
  // A route no step calls is outside the flow: a card in a column on the
  // diagram's right, its arms listed inside, no edges.
  const laidOut = root.children ?? [];
  let asideY = Math.min(...laidOut.map((c) => c.y ?? 0));
  const asideX = Math.max(...laidOut.map((c) => (c.x ?? 0) + (c.width ?? 0))) + ASIDE_GAP;
  for (const r of focus ? [] : wf.routes.filter((r) => !used.has(r.name))) {
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
    const stage = boxes.has(c.id) || cards.has(c.id) ? stages.get(c.id) : undefined;
    if (stage) {
      // Every step that runs in it — a group's branches and merge too — for its run status.
      const steps = stage.members.flatMap((m) => {
        const g = byId.get(m);
        return [m, ...childrenOf(m).map((k) => k.id), ...(g?.merge ? [g.merge.stepName] : [])];
      });
      if (cards.has(c.id)) {
        nodes.push({
          id: c.id,
          type: "stage",
          position: pos,
          data: { stage, workflow: wf.name, steps, collapsed: true } satisfies StageNodeData,
          style: { width: c.width, height: c.height },
          draggable: false,
          deletable: false,
        });
        return;
      }
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
    const outside = boundary.get(c.id);
    if (outside) {
      nodes.push({ id: c.id, type: "boundary", position: pos, data: outside, draggable: false, deletable: false });
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
          data: stepData(byId.get(k.id)!),
          style: { height: k.height },
          draggable: false,
        });
      }
    } else {
      nodes.push({ id: n.id, type: "step", position: pos, ...inBox, data: stepData(n), style: { height: c.height }, draggable: false });
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
    const full = edgeLabel(e);
    const text = textOf(e);
    return {
      id,
      source: ref(e.from),
      target: ref(e.to),
      type: "elk",
      data: {
        edge: e,
        points,
        labelAt: lab && lab.x != null ? { x: lab.x + (lab.width ?? 0) / 2, y: lab.y! + (lab.height ?? 0) / 2 } : undefined,
        index: i,
        text,
        title: full && full !== text ? full : undefined,
      },
      // Several gotos drawn as one hop between stages: not one edge of the code.
      deletable: !merged.has(e),
    };
  });

  // Positions are absolute: a box's own plus its parents'.
  const absolute = absoluteRects(root);
  // Each step that asks again: a short loop on its right side, its
  // conditions on hover.
  for (const [id, list] of loops) {
    const r = absolute.get(id);
    if (!r) continue;
    // High on the side, by the step's header: the middle is where an exit leaves (assignPorts).
    const [y1, y2] = [r.y + Math.min(14, r.h * 0.2), r.y + Math.min(38, r.h * 0.45)];
    const out = r.x + r.w + LOOP_W;
    const index = flowEdges.length;
    flowEdges.push({
      id: `loop:${id}`,
      source: id,
      target: id,
      type: "elk",
      data: {
        edge: list[0],
        points: [{ x: r.x + r.w, y: y1 }, { x: out, y: y1 }, { x: out, y: y2 }, { x: r.x + r.w, y: y2 }],
        labelAt: { x: out + 10, y: (y1 + y2) / 2 },
        index,
        text: list.length === 1 ? "↻" : `↻ ${list.length}`,
        title: `asks again when:\n${list.map((e) => e.label ?? "always").join("\n")}`,
      },
      // Several gotos drawn as one: deleting it would remove only the first.
      deletable: list.length === 1,
    });
  }
  return { nodes, edges: flowEdges };
}

/**
 * Edges a run actually took, as "from>to" keys over top-level ids: the
 * visit sequence with parallel branches folded into their group, plus the
 * start edge and the terminal edge for a run that ended. A hop a route
 * made (`goto Route(x)`) is drawn through the route's node: both its legs.
 */
export function takenEdges(wf: Workflow, visits: string[], ended: "end" | "fail" | null, view?: StageView): Set<string> {
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
  if (!view) return out;
  // Collapsed stages: the hop between what stands for each end.
  const rep = stageRep(wf, view);
  for (const key of [...out]) {
    const [from, to] = key.split(">").map(rep);
    if (from !== to) out.add(`${from}>${to}`);
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
