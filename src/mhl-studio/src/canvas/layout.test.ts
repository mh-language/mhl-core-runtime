import { describe, expect, it } from "vitest";
import { drawnEdges, layoutWorkflow, takenEdges } from "./layout";
import { END, FAIL, START, type GraphEdge, type GraphNode, type Route, type Workflow } from "../lib/types";

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

const arm = (pattern: string, target?: string): Route["arms"][number] =>
  ({ pattern, target, fail: !target, wildcard: pattern === "_", span: [0, 0], patternSpan: [0, 0], thenSpan: [0, 0] }) as Route["arms"][number];

/**
 * `route Choose(m) { M.A -> A  M.B -> B  _ -> fail(...) }`, called by Gate and
 * (guarded by `ok`) by Retry — as `mhl graph` reports it: one edge per arm per caller.
 */
function routed(): Workflow {
  const nodes = [node("Gate"), node("Retry"), node("A"), node("B"), node("Done")];
  const viaChoose = (from: string, guard?: string): GraphEdge[] =>
    (
      [
        ["A", "M.A"],
        ["B", "M.B"],
        [FAIL, "_"],
      ] as const
    ).map(([to, p]) => ({ from, to, kind: "route", via: "Choose", label: guard ? `${guard} → ${p}` : p }));
  const edges: GraphEdge[] = [
    edge(START, "Gate"),
    ...viaChoose("Gate"),
    ...viaChoose("Retry", "ok"),
    edge("A", "Done", "goto"),
    edge("B", "Done", "goto"),
    edge("Done", END, "end", "complete"),
  ];
  const routes: Route[] = [{ name: "Choose", params: ["m"], span: [0, 0], arms: [arm("M.A", "A"), arm("M.B", "B"), arm("_")] }];
  return { name: "W", kind: "workflow", span: [0, 0], headerSpan: [0, 0], nameSpan: [0, 0], bodyOpen: 0, bodyClose: 0, inputs: [], vars: [], props: [], routes, nodes, edges, externals: [] };
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

  it("draws a route as its own node, between its callers and its arms", async () => {
    const wf = routed();
    const drawn = drawnEdges(wf);
    expect(drawn.filter((e) => e.to === "route:Choose").map((e) => [e.from, e.label])).toEqual([
      ["Gate", undefined],
      ["Retry", "ok"],
    ]);
    expect(drawn.filter((e) => e.from === "route:Choose").map((e) => [e.to, e.label, e.kind])).toEqual([
      ["A", "M.A", "route"],
      ["B", "M.B", "route"],
      [FAIL, "_", "route"],
    ]);
    expect(drawn.some((e) => e.kind === "route" && !e.from.startsWith("route:"))).toBe(false);
    const l = await layoutWorkflow(wf);
    const r = l.nodes.find((n) => n.id === "route:Choose")!;
    expect(r.type).toBe("route");
    const y = (id: string) => l.nodes.find((n) => n.id === id)!.position.y;
    expect(y("Gate")).toBeLessThan(r.position.y);
    expect(r.position.y).toBeLessThan(y("A"));
  });

  it("draws a route no step calls apart from the flow: a card on the right, no edges", async () => {
    const wf = routed();
    wf.edges = wf.edges.filter((e) => e.kind !== "route");
    const base = await layoutWorkflow({ ...wf, routes: [] });
    const l = await layoutWorkflow(wf);
    const r = l.nodes.find((n) => n.id === "route:Choose")!;
    expect(r.data).toMatchObject({ used: false });
    expect(l.edges.some((e) => e.source === "route:Choose" || e.target === "route:Choose")).toBe(false);
    // The flow is laid out exactly as if the route were not there…
    const pos = (ls: typeof l) => ls.nodes.filter((n) => n.type !== "route").map((n) => [n.id, n.position]);
    expect(pos(l)).toEqual(pos(base));
    // …and the card sits to the right of all of it.
    const right = Math.max(...l.nodes.filter((n) => n.type !== "route").map((n) => n.position.x + 236));
    expect(r.position.x).toBeGreaterThan(right);
  });

  it("marks both legs of a hop a route made as taken", () => {
    expect([...takenEdges(routed(), ["Gate", "B", "Done"], "end")]).toEqual(
      expect.arrayContaining(["Gate>route:Choose", "route:Choose>B", "B>Done", `Done>${END}`]),
    );
  });

  it("starts at an entry route: Start → route → its arms", async () => {
    // As `mhl graph` reports `entry route Choose`: the start edges are its arms.
    const wf = routed();
    wf.routes[0].entry = true;
    wf.edges = [
      ...wf.edges.filter((e) => e.from !== START && e.from !== "Gate"),
      ...wf.edges.filter((e) => e.from === "Gate" && e.kind === "route").map((e) => ({ ...e, from: START })),
    ];
    const drawn = drawnEdges(wf);
    expect(drawn.filter((e) => e.from === START)).toEqual([{ from: START, to: "route:Choose", kind: "next" }]);
    const l = await layoutWorkflow(wf);
    const y = (id: string) => l.nodes.find((n) => n.id === id)!.position.y;
    expect(y(START)).toBeLessThan(y("route:Choose"));
    expect(y("route:Choose")).toBeLessThan(y("A"));
    expect(l.nodes.find((n) => n.id === "route:Choose")!.data).toMatchObject({ used: true });
    // A run visits the route as a step of its own name.
    expect([...takenEdges(wf, ["Choose", "B", "Done"], "end")]).toEqual(
      expect.arrayContaining([`${START}>route:Choose`, "route:Choose>B", "B>Done"]),
    );
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
