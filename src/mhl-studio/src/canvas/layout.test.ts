import { describe, expect, it } from "vitest";
import { drawnEdges, layoutWorkflow, runOrder, takenEdges, type StepNodeData } from "./layout";
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

describe("stages", () => {
  /** Intake → [Build: Compile, Fan(L, R)] → [Ship: Gate → goto Build | Publish] → End */
  function staged(): Workflow {
    const nodes = [
      node("Intake"),
      node("Compile", { stage: "Build" }),
      node("Fan", { kind: "parallel", children: ["L", "R"], stage: "Build" }),
      node("L", { parent: "Fan" }),
      node("R", { parent: "Fan" }),
      node("Gate", { stage: "Ship" }),
      node("Publish", { stage: "Ship" }),
    ];
    const edges = [
      edge(START, "Intake"),
      edge("Intake", "Compile"),
      edge("Compile", "Fan"),
      edge("Fan", "Gate"),
      edge("Gate", "Build", "goto"),
      edge("Gate", "Publish"),
      edge("Publish", END),
    ];
    const stage = (name: string, members: string[]) => ({ name, members, span: [0, 0], headerSpan: [0, 0], nameSpan: [0, 0], bodyOpen: 0, bodyClose: 0 }) as const;
    return {
      ...branching(),
      nodes,
      edges,
      stages: [stage("Build", ["Compile", "Fan"]), stage("Ship", ["Gate", "Publish"])] as unknown as Workflow["stages"],
    };
  }

  it("boxes each stage around its members, a group's branches inside the group", async () => {
    const { nodes, edges } = await layoutWorkflow(staged());
    const byId = new Map(nodes.map((n) => [n.id, n]));
    expect(byId.get("Build")?.type).toBe("stage");
    expect(byId.get("Compile")?.parentId).toBe("Build");
    expect(byId.get("Fan")?.parentId).toBe("Build");
    expect(byId.get("L")?.parentId).toBe("Fan");
    expect(byId.get("Gate")?.parentId).toBe("Ship");
    expect(byId.get("Intake")?.parentId).toBeUndefined();
    // A parent comes before its children, as React Flow requires.
    const at = (id: string) => nodes.findIndex((n) => n.id === id);
    expect(at("Build")).toBeLessThan(at("Compile"));
    expect(at("Fan")).toBeLessThan(at("L"));
    // Every edge is routed, the goto into a stage's box included.
    for (const e of edges) expect(e.data!.points.length).toBeGreaterThan(1);
    expect(edges.some((e) => e.target === "Build")).toBe(true);
    // Edges are in canvas coordinates: one between two boxed nodes leaves the
    // source's bottom edge where React Flow draws it (box + relative position).
    const abs = (id: string): { x: number; y: number } => {
      const n = byId.get(id)!;
      const p = n.parentId ? abs(n.parentId) : { x: 0, y: 0 };
      return { x: p.x + n.position.x, y: p.y + n.position.y };
    };
    const e = edges.find((x) => x.source === "Compile" && x.target === "Fan")!;
    const start = e.data!.points[0];
    const c = abs("Compile");
    const h = Number(byId.get("Compile")!.style?.height);
    expect(Math.abs(start.y - (c.y + h))).toBeLessThan(1);
    expect(start.x).toBeGreaterThanOrEqual(c.x);
    expect(start.x).toBeLessThanOrEqual(c.x + 236);
  });

  it("lights the goto into a stage when the run took it", () => {
    const wf = staged();
    const taken = takenEdges(wf, ["Intake", "Compile", "L", "R", "Gate", "Compile"], null);
    expect(taken.has("Gate>Build")).toBe(true);
  });
});

describe("readability", () => {
  /** Retry is declared first but only reached from Review; Review asks again twice; Check guards with fail(). */
  function review(): Workflow {
    const long = "implementation.status != \"ready\" && attempts < max_attempts";
    return {
      ...branching(),
      nodes: [node("Retry"), node("Check", { exits: ["fail"] }), node("Review"), node("Done")],
      edges: [
        edge(START, "Check"),
        edge("Check", FAIL, "fail", "dir.missing()"),
        edge("Check", FAIL, "fail", "cmd.is_empty()"),
        edge("Check", "Review"),
        edge("Review", "Review", "goto", "note.is_empty()"),
        edge("Review", "Review", "goto", "!passed"),
        edge("Review", "Retry", "goto", long),
        edge("Review", "Done"),
        edge("Retry", "Review", "goto"),
        edge("Done", END, "end", "complete"),
      ],
    };
  }

  it("orders nodes as a run reaches them, an early-declared step after its caller", () => {
    const wf = review();
    expect(runOrder(wf, wf.nodes).map((n) => n.id)).toEqual(["Check", "Review", "Retry", "Done"]);
  });

  it("puts a guard's fail() conditions on the step, with no edge to Fail", async () => {
    const { nodes, edges } = await layoutWorkflow(review());
    expect(nodes.some((n) => n.id === FAIL)).toBe(false);
    expect(edges.some((e) => e.target === FAIL)).toBe(false);
    const check = nodes.find((n) => n.id === "Check")!.data as StepNodeData;
    expect(check.failsWhen).toEqual(["dir.missing()", "cmd.is_empty()"]);
  });

  it("keeps a match arm to Fail as an edge to the terminal", async () => {
    const { nodes, edges } = await layoutWorkflow(branching());
    expect(nodes.some((n) => n.id === FAIL)).toBe(true);
    expect(edges.some((e) => e.target === FAIL)).toBe(true);
  });

  it("draws a step's ask-again gotos as one short loop on its right side", async () => {
    const { nodes, edges } = await layoutWorkflow(review());
    const loops = edges.filter((e) => e.source === "Review" && e.target === "Review");
    expect(loops).toHaveLength(1);
    const d = loops[0].data!;
    expect(d.text).toBe("↻ 2");
    expect(d.title).toBe("asks again when:\nnote.is_empty()\n!passed");
    expect(loops[0].deletable).toBe(false);
    const r = nodes.find((n) => n.id === "Review")!;
    for (const p of d.points) expect(p.x).toBeGreaterThanOrEqual(r.position.x + 236);
  });

  it("cuts a long label, keeping the full condition for the tooltip", async () => {
    const { edges } = await layoutWorkflow(review());
    const d = edges.find((e) => e.source === "Review" && e.target === "Retry")!.data!;
    expect(d.text!.length).toBeLessThanOrEqual(34);
    expect(d.text!.endsWith("…")).toBe(true);
    expect(d.title).toBe(d.edge.label);
  });
});

describe("runOrder", () => {
  /** Fix is declared first; S is Begin → (Draft →) Approve, Approve sending a revision to Fix. */
  function gated(): Workflow {
    return {
      ...branching(),
      nodes: [node("Fix"), node("Begin", { stage: "S" }), node("Draft", { stage: "S" }), node("Approve", { stage: "S" })],
      edges: [
        edge(START, "S", "goto"),
        edge("Begin", "Approve", "goto", "existing"),
        edge("Begin", "Draft"),
        edge("Draft", "Approve"),
        edge("Approve", "Fix", "goto", "revise"),
        edge("Fix", "S", "goto"),
      ],
      stages: [{ name: "S", members: ["Begin", "Draft", "Approve"] }] as unknown as Workflow["stages"],
    };
  }

  it("moves a stage as one unit, its members in written order", () => {
    const wf = gated();
    expect(runOrder(wf, wf.nodes).map((n) => n.id)).toEqual(["Begin", "Draft", "Approve", "Fix"]);
  });

  it("draws a gate below the step that falls into it, though it can send the run back there", async () => {
    const wf = gated();
    wf.edges.push(edge("Approve", "Draft", "goto", "!approved"));
    const { nodes } = await layoutWorkflow(wf);
    const y = (id: string) => nodes.find((n) => n.id === id)!.position.y;
    expect(y("Begin")).toBeLessThan(y("Draft"));
    expect(y("Draft")).toBeLessThan(y("Approve"));
  });
});
