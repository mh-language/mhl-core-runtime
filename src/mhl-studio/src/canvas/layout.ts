// Workflow graph → positioned React Flow nodes/edges, laid out by ELK
// (layered, top-down, orthogonal edges — the Step Functions look). Layout is
// fully automatic: node positions are derived from the source on every
// change, never stored, so code and diagram cannot disagree.
import ELK, { type ElkExtendedEdge, type ElkNode } from "elkjs/lib/elk.bundled.js";
import type { Edge, Node } from "@xyflow/react";
import { END, FAIL, START, type GraphEdge, type GraphNode, type Workflow } from "../lib/types";

/** An edge's label as drawn: a match/route arm's `_` reads "otherwise". */
export function edgeLabel(e: { kind: string; label?: string }): string | undefined {
  if (!e.label || (e.kind !== "match" && e.kind !== "route")) return e.label;
  return e.label.replace(/(^|\s)_$/, "$1otherwise");
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
export type TerminalNodeData = { role: "start" | "end" | "fail" };
export type ExternalNodeData = { name: string };
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

export async function layoutWorkflow(wf: Workflow): Promise<Layout> {
  const top = wf.nodes.filter((n) => !n.parent);
  const childrenOf = (id: string) => wf.nodes.filter((n) => n.parent === id);
  const usesFail = wf.edges.some((e) => e.to === FAIL);

  const children: ElkNode[] = [{ id: START, width: TERM_W, height: TERM_H }];
  for (const n of top) {
    if (n.kind === "parallel") {
      children.push({
        id: n.id,
        layoutOptions: {
          // A merge is drawn as a bar along the group's bottom edge.
          "elk.padding": `[top=44,left=18,bottom=${n.merge ? 18 + MERGE_H + 8 : 18},right=18]`,
          "elk.spacing.nodeNode": "24",
        },
        children: childrenOf(n.id).map((c) => ({ id: c.id, width: STEP_W, height: stepHeight(c) })),
      });
    } else {
      children.push({ id: n.id, width: STEP_W, height: stepHeight(n) });
    }
  }
  for (const x of wf.externals) children.push({ id: `ext:${x}`, width: 170, height: 44 });
  children.push({ id: END, width: TERM_W, height: TERM_H });
  if (usesFail) children.push({ id: FAIL, width: TERM_W, height: TERM_H });

  const ext = new Set(wf.externals);
  const ref = (id: string) => (ext.has(id) ? `ext:${id}` : id);
  const edges: ElkExtendedEdge[] = wf.edges.map((e, i) => {
    const text = edgeLabel(e);
    return {
      id: edgeId(e, i),
      sources: [ref(e.from)],
      targets: [ref(e.to)],
      labels: text ? [{ text, width: labelWidth(text), height: 18 }] : [],
    };
  });

  const line = mainLine(wf.edges.map((e) => [ref(e.from), ref(e.to)]));
  const root = await elk.layout({ id: "root", layoutOptions: LAYERED, children, edges: linkMainLine(edges, line) });
  centerMainLine(root, line);

  const byId = new Map(wf.nodes.map((n) => [n.id, n]));
  const nodes: Node[] = [];
  for (const c of root.children ?? []) {
    const pos = { x: c.x ?? 0, y: c.y ?? 0 };
    if (c.id === START || c.id === END || c.id === FAIL) {
      const role = c.id === START ? "start" : c.id === END ? "end" : "fail";
      nodes.push({ id: c.id, type: "terminal", position: pos, data: { role } satisfies TerminalNodeData, draggable: false, connectable: c.id !== START, deletable: false });
      continue;
    }
    if (c.id.startsWith("ext:")) {
      nodes.push({ id: c.id, type: "external", position: pos, data: { name: c.id.slice(4) } satisfies ExternalNodeData, draggable: false, deletable: false });
      continue;
    }
    const n = byId.get(c.id)!;
    if (n.kind === "parallel") {
      nodes.push({
        id: n.id,
        type: "parallel",
        position: pos,
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
      nodes.push({ id: n.id, type: "step", position: pos, data: { node: n, workflow: wf.name } satisfies StepNodeData, style: { height: c.height }, draggable: false });
    }
  }

  const laid = new Map((root.edges ?? []).map((e) => [e.id, e]));
  const flowEdges: Edge<FlowEdgeData>[] = wf.edges.map((e, i) => {
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
 * start edge and the terminal edge for a run that ended.
 */
export function takenEdges(wf: Workflow, visits: string[], ended: "end" | "fail" | null): Set<string> {
  const parent = new Map(wf.nodes.filter((n) => n.parent).map((n) => [n.id, n.parent!]));
  // A group's merge runs as its own step ("Fan.merge"): it is part of the group.
  for (const n of wf.nodes) if (n.merge) parent.set(n.merge.stepName, n.id);
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
