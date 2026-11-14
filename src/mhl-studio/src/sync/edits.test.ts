import { describe, expect, it } from "vitest";
import {
  addInput,
  addWorkflow,
  applyEdits,
  connect,
  deleteNode,
  Doc,
  EditError,
  getBody,
  insertState,
  moveNode,
  removeEdge,
  removeInput,
  renameNode,
  setBody,
  setDescription,
  setTimeoutClause,
  type EditResult,
} from "./edits";
import { OffsetMap } from "./offsets";
import { newWorkflowSource } from "./templates";
import { graphOf, MHL } from "./testutil";

const SRC = `// Order handling — ünïcödé comment stays put
agent Writer { engine: "cli/claude" }

workflow Orders {
    description: "Handles orders"
    input kind: string = "small"

    // the first step
    step Intake {
        log("intake")
    }

    step Classify {
        goto match kind {
            "small" -> Ship
            _ -> fail("unknown")
        }
    }

    step Ship timeout 5m {
        log("ship")
    }
}
`;

function doc(src = SRC) {
  return new Doc(src, graphOf(src));
}

/** Applies an edit and re-graphs the result, which must still parse. */
function apply(d: Doc, r: EditResult) {
  const src = applyEdits(d.src, r.edits);
  const g = graphOf(src);
  expect(g.ok, `${src}\n${JSON.stringify(g.diagnostics)}`).toBe(true);
  return { src, g, wf: g.workflows[0], doc: new Doc(src, g) };
}

function trace(d: { wf: { edges: { from: string; to: string; kind: string }[] } }) {
  return d.wf.edges.map((e) => `${e.from}>${e.to}:${e.kind}`);
}

describe.skipIf(!MHL)("edits round-trip through mhl graph", () => {
  it("inserts a state between two steps (Step Functions-style)", () => {
    const d = doc();
    const r = insertState(d, "Orders", "agent", { after: "Intake" });
    expect(r.select).toBe("Ask");
    const out = apply(d, r);
    expect(out.wf.nodes.map((n) => n.id)).toEqual(["Intake", "Ask", "Classify", "Ship"]);
    expect(trace(out).slice(0, 3)).toEqual(["$start>Intake:next", "Intake>Ask:next", "Ask>Classify:next"]);
    expect(out.wf.nodes[1].calls).toEqual(["Writer.run"]);
    expect(out.src).toContain("// Order handling — ünïcödé comment stays put");
    expect(out.src).toContain("    // the first step\n    step Intake {");
  });

  it("inserts before the first step, at the end, and into a parallel group", () => {
    let d = doc();
    let out = apply(d, insertState(d, "Orders", "pass", { before: "Intake" }));
    expect(out.wf.nodes[0].id).toBe("Step");
    out = apply(out.doc, insertState(out.doc, "Orders", "parallel", { end: true }));
    expect(out.wf.nodes.map((n) => n.id).slice(-3)).toEqual(["Fan", "FanLeft", "FanRight"]);
    out = apply(out.doc, insertState(out.doc, "Orders", "approval", { into: "Fan" }));
    expect(out.wf.nodes.find((n) => n.id === "Fan")!.children).toEqual(["FanLeft", "FanRight", "Approve"]);
    d = out.doc;
    expect(() => insertState(d, "Orders", "choice", { into: "Fan" })).toThrow(EditError);
  });

  it("fills an empty workflow body", () => {
    const d = doc("workflow Empty {\n}\n");
    const out = apply(d, insertState(d, "Empty", "succeed", { end: true }));
    expect(out.src).toBe("workflow Empty {\n    step Done {\n        complete()\n    }\n}\n");
    const d2 = doc("workflow Inline { }\n");
    expect(apply(d2, insertState(d2, "Inline", "pass", { end: true })).wf.nodes).toHaveLength(1);
  });

  it("renames a step and every reference to it", () => {
    const d = doc();
    const out = apply(d, renameNode(d, "Orders", "Ship", "Deliver"));
    expect(out.wf.nodes.map((n) => n.id)).toContain("Deliver");
    expect(trace(out)).toContain("Classify>Deliver:match");
    expect(() => renameNode(d, "Orders", "Ship", "Intake")).toThrow(/already exists/);
    expect(() => renameNode(d, "Orders", "Ship", "9x")).toThrow(/valid name/);
  });

  it("deletes a step with its lines", () => {
    const d = doc();
    const out = apply(d, deleteNode(d, "Orders", "Intake"));
    expect(out.wf.nodes.map((n) => n.id)).toEqual(["Classify", "Ship"]);
    expect(out.src).not.toMatch(/\n\n\n/);
  });

  it("sets, changes and clears a timeout", () => {
    let d = doc();
    let out = apply(d, setTimeoutClause(d, "Orders", "Intake", "30s"));
    expect(out.wf.nodes[0].timeout).toBe("30s");
    out = apply(out.doc, setTimeoutClause(out.doc, "Orders", "Ship", "1h"));
    expect(out.wf.nodes.find((n) => n.id === "Ship")!.timeout).toBe("1h");
    d = out.doc;
    out = apply(d, setTimeoutClause(d, "Orders", "Ship", ""));
    expect(out.src).toContain("    step Ship {");
    expect(() => setTimeoutClause(d, "Orders", "Ship", "5 minutes")).toThrow(EditError);
  });

  it("connects steps with goto, complete() and fail()", () => {
    const d = doc();
    let out = apply(d, connect(d, "Orders", "Ship", "Intake"));
    expect(trace(out)).toContain("Ship>Intake:goto");
    expect(out.wf.nodes.find((n) => n.id === "Ship")!.fallsThrough).toBe(false);
    // retargets the now-unconditional trailing goto instead of adding another
    out = apply(out.doc, connect(out.doc, "Orders", "Ship", "Classify"));
    expect(trace(out).filter((t) => t.startsWith("Ship>"))).toEqual(["Ship>Classify:goto"]);
    out = apply(out.doc, connect(out.doc, "Orders", "Intake", "$end"));
    expect(trace(out)).toContain("Intake>$end:end");
    // connecting to the step it already falls into is a no-op
    expect(connect(d, "Orders", "Intake", "Classify").edits).toEqual([]);
  });

  it("removes a goto line and a match arm", () => {
    const d = doc();
    let out = apply(d, connect(d, "Orders", "Ship", "Intake"));
    const g = out.wf.edges.find((e) => e.kind === "goto")!;
    out = apply(out.doc, removeEdge(out.doc, "Orders", g));
    expect(trace(out)).not.toContain("Ship>Intake:goto");
    const arm = out.wf.edges.find((e) => e.kind === "match" && e.to === "Ship")!;
    out = apply(out.doc, removeEdge(out.doc, "Orders", arm));
    expect(trace(out).filter((t) => t.startsWith("Classify>"))).toEqual(["Classify>$fail:match"]);
  });

  it("reads and replaces a step body", () => {
    const d = doc();
    expect(getBody(d, "Orders", "Classify")).toBe('goto match kind {\n    "small" -> Ship\n    _ -> fail("unknown")\n}');
    const out = apply(d, setBody(d, "Orders", "Intake", 'log("a")\nif (kind == "x") goto Ship'));
    expect(trace(out)).toContain("Intake>Ship:goto");
    expect(out.src).toContain('    step Intake {\n        log("a")\n        if (kind == "x") goto Ship\n    }');
  });

  it("moves a step", () => {
    const d = doc();
    const out = apply(d, moveNode(d, "Orders", "Ship", -1));
    expect(out.wf.nodes.map((n) => n.id)).toEqual(["Intake", "Ship", "Classify"]);
  });

  it("edits inputs and the description", () => {
    const d = doc();
    let out = apply(d, addInput(d, "Orders", "approved", "bool", "false"));
    expect(out.wf.inputs.map((i) => `${i.name}:${i.type}=${i.default}`)).toEqual(["kind:string=\"small\"", "approved:bool=false"]);
    out = apply(out.doc, removeInput(out.doc, "Orders", "kind"));
    expect(out.wf.inputs.map((i) => i.name)).toEqual(["approved"]);
    out = apply(out.doc, setDescription(out.doc, "Orders", 'Says "hi"'));
    expect(out.wf.description).toBe('Says "hi"');
    out = apply(out.doc, setDescription(out.doc, "Orders", ""));
    expect(out.wf.description).toBeUndefined();
  });

  it("adds a new workflow to the file", () => {
    const d = doc();
    const r = addWorkflow(d, newWorkflowSource);
    const out = apply(d, r);
    expect(out.g.workflows.map((w) => w.name)).toEqual(["Orders", "Workflow"]);
  });
});

describe("OffsetMap", () => {
  it("maps UTF-8 byte offsets onto UTF-16 indices", () => {
    const s = "aé😀b";
    const m = new OffsetMap(s);
    // bytes: a=0, é=1..2, 😀=3..6, b=7
    expect(m.toUtf16(0)).toBe(0);
    expect(m.toUtf16(1)).toBe(1);
    expect(m.toUtf16(3)).toBe(2);
    expect(m.toUtf16(7)).toBe(4);
    expect(m.toUtf16(8)).toBe(5);
    expect(m.toByte(4)).toBe(7);
  });

  it("is the identity for ASCII", () => {
    const m = new OffsetMap("abc");
    expect(m.toUtf16(2)).toBe(2);
    expect(m.toByte(2)).toBe(2);
  });
});
