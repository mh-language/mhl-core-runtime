import { describe, expect, it } from "vitest";
import { addToStep, applyEdits, Doc, EditError, takenNames } from "./edits";
import { agentCallStatement, freshVar, toolCallStatement } from "./templates";
import { graphOf, MHL } from "./testutil";

const SRC = `agent Writer { command: "echo" }

tool Pricing {
    unit_price(kind: string, base: number = 10): number -> base
}

workflow W {
    input kind: string = "small"
    var price = 0

    step Empty { }

    step Plain {
        log("a")
    }

    step Leaves {
        var unit_price = 1
        log("b")
        goto match kind {
            "small" -> Plain
            _ -> fail("no")
        }
    }

    parallel Fan {
        step Left { }
        step Right { }
    }
}
`;

const doc = (src: string) => new Doc(src, graphOf(src));

function apply(src: string, stmtFor: (d: Doc) => string, id: string): string {
  const d = doc(src);
  const out = applyEdits(src, addToStep(d, "W", id, stmtFor(d)).edits);
  const g = graphOf(out);
  expect(g.ok, out).toBe(true);
  expect(g.diagnostics, out).toEqual([]);
  return out;
}

describe("freshVar", () => {
  it("avoids taken names", () => {
    expect(freshVar("reply", [])).toBe("reply");
    expect(freshVar("reply", ["reply", "reply2"])).toBe("reply3");
  });
});

describe.skipIf(!MHL)("dropping a call onto a step", () => {
  const method = () => doc(SRC).graph.declarations.find((d) => d.name === "Pricing")!.methods![0];

  it("fills an empty step", () => {
    const out = apply(SRC, (d) => toolCallStatement("Pricing", method(), takenNames(d, "W", "Empty")), "Empty");
    expect(out).toContain('    step Empty {\n        var unit_price = Pricing.unit_price("")\n    }');
  });

  it("appends to a step that falls through", () => {
    const out = apply(SRC, (d) => agentCallStatement("Writer", takenNames(d, "W", "Plain")), "Plain");
    expect(out).toContain('        log("a")\n        var reply = Writer.run(prompt: "TODO: describe the task")\n    }');
  });

  it("goes before the statement a step always leaves through, with a fresh name", () => {
    const out = apply(SRC, (d) => toolCallStatement("Pricing", method(), takenNames(d, "W", "Leaves")), "Leaves");
    expect(out).toContain('        log("b")\n        var unit_price2 = Pricing.unit_price("")\n        goto match kind {');
  });

  it("does not reuse workflow vars and inputs", () => {
    expect(takenNames(doc(SRC), "W", "Plain")).toEqual(expect.arrayContaining(["kind", "price"]));
  });

  it("works inside a parallel branch, not on the group", () => {
    const out = apply(SRC, () => "log(1)", "Left");
    expect(out).toContain("        step Left {\n            log(1)\n        }");
    expect(() => addToStep(doc(SRC), "W", "Fan", "log(1)")).toThrow(EditError);
  });

  it("reports each call as a call site", () => {
    const out = apply(SRC, (d) => agentCallStatement("Writer", takenNames(d, "W", "Plain")), "Plain");
    const node = graphOf(out).workflows[0].nodes.find((n) => n.id === "Plain")!;
    expect(node.callSites.map((c) => c.text)).toEqual(['Writer.run(prompt: "TODO: describe the task")']);
  });
});
