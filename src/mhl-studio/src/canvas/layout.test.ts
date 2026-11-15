import { describe, expect, it } from "vitest";
import { layoutWorkflow } from "./layout";
import { END, FAIL, START, type GraphEdge, type GraphNode, type Workflow } from "../lib/types";

const node = (id: string, extra: Partial<GraphNode> = {}): GraphNode =>
  ({ id, kind: "step", name: id, span: [0, 0], headerSpan: [0, 0], nameSpan: [0, 0], bodyOpen: 0, bodyClose: 0, calls: [], callSites: [], exits: [], fallsThrough: false, ...extra }) as GraphNode;
const edge = (from: string, to: string, kind: GraphEdge["kind"] = "next", label?: string): GraphEdge => ({ from, to, kind, label });

/** The shape of a workflow that branches three ways and merges: Choose → (A | B | Fan → Merge | fail) → Done. */
function branching(): Workflow {
  const nodes = [
    node("Choose"),
    node("A", { callSites: [{ receiver: "X", method: "run", text: "X.run()", span: [0, 0] }] }),
    node("B"),
    node("Fan", { kind: "parallel", children: ["L", "R"] }),
    node("L", { parent: "Fan" }),
    node("R", { parent: "Fan" }),
    node("Merge"),
    node("Done"),
  ];
  const edges = [
    edge(START, "Choose"),
    edge("Choose", "A", "match", "M.A"),
    edge("Choose", "B", "match", "M.B"),
    edge("Choose", "Fan", "match", "M.All"),
    edge("Choose", FAIL, "match", "_"),
    edge("Fan", "Merge"),
    edge("A", "Done", "goto"),
    edge("B", "Done", "goto"),
    edge("Merge", "Done"),
    edge("Done", END, "end", "complete"),
  ];
  return { name: "W", kind: "workflow", span: [0, 0], headerSpan: [0, 0], nameSpan: [0, 0], bodyOpen: 0, bodyClose: 0, inputs: [], vars: [], props: [], routes: [], nodes, edges, externals: [] };
}

describe("layoutWorkflow", () => {
  it("puts the main line — Start, the choice, the merge step, End — on one vertical axis", async () => {
    const l = await layoutWorkflow(branching());
    const cx = (id: string) => {
      const n = l.nodes.find((x) => x.id === id)!;
      const w = id === START || id === END || id === FAIL ? 104 : 236;
      return Math.round(n.position.x + w / 2);
    };
    const axis = cx(START);
    expect([cx("Choose"), cx("Done"), cx(END)]).toEqual([axis, axis, axis]);
    // A straight segment stays straight: Merge right under its group.
    const fan = l.nodes.find((n) => n.id === "Fan")!;
    expect(cx("Merge")).toBe(Math.round(fan.position.x + (fan.style!.width as number) / 2));
    // Every edge is still routed, from its source to its target.
    for (const e of l.edges) expect((e.data!.points ?? []).length, e.id).toBeGreaterThanOrEqual(2);
  });

  it("never stacks two nodes of a layer", async () => {
    const l = await layoutWorkflow(branching());
    const top = l.nodes.filter((n) => !n.parentId);
    const size = (n: (typeof top)[number]) =>
      [START, END, FAIL].includes(n.id) ? [104, 36] : n.type === "parallel" ? [n.style!.width as number, n.style!.height as number] : [236, Number(n.style?.height ?? 68)];
    for (const a of top)
      for (const b of top) {
        if (a === b) continue;
        const [aw, ah] = size(a);
        const [bw, bh] = size(b);
        const overlap = a.position.x < b.position.x + bw && b.position.x < a.position.x + aw && a.position.y < b.position.y + bh && b.position.y < a.position.y + ah;
        expect(overlap, `${a.id} / ${b.id}`).toBe(false);
      }
  });
});
