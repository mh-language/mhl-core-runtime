import { describe, expect, it } from "vitest";
import { applyEdits, Doc, type EditResult } from "./edits";
import { setLoop, setMax } from "./header";
import { graphOf, MHL } from "./testutil";

const SRC = `enum Kind { A, B }
type In = { kind: Kind }

partial workflow Refine(req: In) {
    entry step S {
        log(req.kind)
    }
}
`;

const doc = (src: string) => new Doc(src, graphOf(src));

function apply(d: Doc, r: EditResult) {
  const src = applyEdits(d.src, r.edits);
  const g = graphOf(src);
  expect(g.ok, `${src}\n${JSON.stringify(g.diagnostics)}`).toBe(true);
  return { src, wf: g.workflows[0], doc: new Doc(src, g) };
}

describe.skipIf(!MHL)("workflow header", () => {
  it("turns loop on with a max, edits the max, and turns it off again", () => {
    const d = doc(SRC);
    const on = apply(d, setLoop(d, "Refine", true));
    expect(on.src).toContain("partial loop workflow Refine(req: In) max 3 {");
    expect(on.wf.loop).toBe(true);
    expect(on.wf.max).toBe("3");

    const five = apply(on.doc, setMax(on.doc, "Refine", "05"));
    expect(five.wf.max).toBe("5");
    const none = apply(five.doc, setMax(five.doc, "Refine", ""));
    expect(none.src).toContain("partial loop workflow Refine(req: In) {");
    const again = apply(none.doc, setMax(none.doc, "Refine", "2"));
    expect(again.src).toContain("loop workflow Refine(req: In) max 2 {");

    const off = apply(again.doc, setLoop(again.doc, "Refine", false));
    expect(off.src).toContain("partial workflow Refine(req: In) {");
    expect(off.wf.loop).toBeFalsy();
    expect(off.wf.max).toBeFalsy();
  });

  it("refuses max on a plain workflow", () => {
    const d = doc(SRC);
    expect(() => setMax(d, "Refine", "3")).toThrow(/turn Loop on/);
  });
});
