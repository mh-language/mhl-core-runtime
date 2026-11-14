// Workflow graph → positioned React Flow nodes/edges, laid out by ELK
// (layered, top-down, orthogonal edges — the Step Functions look). Layout is
// fully automatic: node positions are derived from the source on every
// change, never stored, so code and diagram cannot disagree.
import ELK, { type ElkExtendedEdge, type ElkNode } from "elkjs/lib/elk.bundled.js";
import type { Edge, Node } from "@xyflow/react";
import { END, FAIL, START, type GraphEdge, type GraphNode, type Workflow } from "../lib/types";

const elk = new ELK();

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
          "elk.padding": "[top=44,left=18,bottom=18,right=18]",
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
  const edges: ElkExtendedEdge[] = wf.edges.map((e, i) => ({
    id: edgeId(e, i),
    sources: [ref(e.from)],
    targets: [ref(e.to)],
    labels: e.label ? [{ text: e.label, width: labelWidth(e.label), height: 18 }] : [],
  }));

  const root = await elk.layout({
    id: "root",
    layoutOptions: {
      "elk.algorithm": "layered",
      "elk.direction": "DOWN",
      "elk.edgeRouting": "ORTHOGONAL",
      "elk.layered.spacing.nodeNodeBetweenLayers": "52",
      "elk.spacing.nodeNode": "44",
      "elk.spacing.edgeNode": "22",
      "elk.spacing.edgeEdge": "14",
      "elk.layered.considerModelOrder.strategy": "NODES_AND_EDGES",
      "elk.layered.nodePlacement.strategy": "BRANDES_KOEPF",
      "elk.edgeLabels.inline": "true",
      "elk.layered.edgeLabels.sideSelection": "ALWAYS_UP",
    },
    children,
    edges,
  });

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
