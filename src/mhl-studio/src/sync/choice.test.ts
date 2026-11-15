import { describe, expect, it } from "vitest";
import { applyEdits, connect, Doc, EditError, insertState, type EditResult } from "./edits";
import { addArm, choiceTemplate, nextPattern, removeArm, setArmPattern, setArmTarget, setSubject } from "./choice";
import { graphOf, MHL } from "./testutil";
import { FAIL } from "../lib/types";

const ENUM = `enum Kind { Bug, Feature, Chore }

workflow Triage {
    input kind: Kind = Kind.Bug
    input urgent: bool = false

    step Intake {
        log(kind)
    }

    step Fix {
        complete()
    }

    step Plan {
        complete()
    }
}
`;

const BARE = `workflow Bare {
    step Start {
        log("start")
    }

    step Review {
        complete()
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

const choice = (o: { wf: { nodes: { id: string; choice?: unknown }[] } }, id: string) =>
  (o.wf.nodes.find((n) => n.id === id)!.choice as { subject: string; arms: { pattern: string; target?: string; fail?: boolean }[] })!;

describe.skipIf(!MHL)("choice", () => {
  it("a new choice branches on an enum input, one rule per variant, the following steps first", () => {
    const d = doc(ENUM);
    const out = apply(d, insertState(d, "Triage", "choice", { after: "Intake" }));
    const c = choice(out, "Choose");
    expect(c.subject).toBe("kind");
    expect(c.arms.map((a) => `${a.pattern}>${a.target ?? "fail"}`)).toEqual(["Kind.Bug>Fix", "Kind.Feature>Plan", "_>fail"]);
    expect(out.src).toContain('_ -> fail("no branch matched: ${kind}")');
  });

  it("with nothing to branch on, declares a decision var and names the rules after the steps", () => {
    const d = doc(BARE);
    const r = insertState(d, "Bare", "choice", { after: "Start" });
    expect(r.note?.text).toContain("self.decision");
    const out = apply(d, r);
    expect(out.wf.vars.map((v) => v.name)).toEqual(["decision"]);
    const c = choice(out, "Choose");
    expect(c.subject).toBe("decision");
    expect(c.arms.map((a) => `${a.pattern}>${a.target ?? "fail"}`)).toEqual(['"review">Review', "_>fail"]);
  });

  it("template: bool before plain, enum before bool", () => {
    const wf = graphOf(ENUM).workflows[0];
    expect(choiceTemplate([], wf, ["A", "B"]).subject).toBe("urgent");
    expect(choiceTemplate([], wf, ["A", "B"]).arms).toEqual([["true", "A"], ["false", "B"]]);
  });

  it("edits the subject, a pattern, a target, and keeps the default's message in step", () => {
    const d0 = doc(ENUM);
    let out = apply(d0, insertState(d0, "Triage", "choice", { after: "Intake" }));
    out = apply(out.doc, setSubject(out.doc, "Triage", "Choose", "urgent"));
    expect(choice(out, "Choose").subject).toBe("urgent");
    expect(out.src).toContain('fail("no branch matched: ${urgent}")');
    out = apply(out.doc, setArmPattern(out.doc, "Triage", "Choose", 0, "true"));
    out = apply(out.doc, setArmTarget(out.doc, "Triage", "Choose", 1, FAIL));
    out = apply(out.doc, setArmTarget(out.doc, "Triage", "Choose", 2, "Plan"));
    const c = choice(out, "Choose");
    expect(c.arms.map((a) => `${a.pattern}>${a.target ?? "fail"}`)).toEqual(["true>Fix", "Kind.Feature>fail", "_>Plan"]);
    expect(() => setArmPattern(out.doc, "Triage", "Choose", 0, "_")).toThrow(EditError);
  });

  it("adds rules before the default, removes them, and refuses the last one", () => {
    const d0 = doc(ENUM);
    let out = apply(d0, insertState(d0, "Triage", "choice", { after: "Intake" }));
    out = apply(out.doc, removeArm(out.doc, "Triage", "Choose", 2)); // the default
    let c = choice(out, "Choose");
    expect(c.arms.map((a) => a.pattern)).toEqual(["Kind.Bug", "Kind.Feature"]);
    out = apply(out.doc, addArm(out.doc, "Triage", "Choose", "_", FAIL));
    const wf = out.wf;
    const n = wf.nodes.find((x) => x.id === "Choose")!;
    expect(nextPattern([...graphOf(out.src).declarations], wf, n.choice!, "Plan")).toBe("Kind.Chore");
    out = apply(out.doc, addArm(out.doc, "Triage", "Choose", "Kind.Chore", "Plan"));
    c = choice(out, "Choose");
    expect(c.arms.map((a) => `${a.pattern}>${a.target ?? "fail"}`)).toEqual(["Kind.Bug>Fix", "Kind.Feature>Plan", "Kind.Chore>Plan", "_>fail"]);
    expect(() => addArm(out.doc, "Triage", "Choose", "Kind.Bug", "Plan")).toThrow(EditError);
    out = apply(out.doc, removeArm(out.doc, "Triage", "Choose", 3));
    out = apply(out.doc, removeArm(out.doc, "Triage", "Choose", 2));
    out = apply(out.doc, removeArm(out.doc, "Triage", "Choose", 1));
    expect(() => removeArm(out.doc, "Triage", "Choose", 0)).toThrow(EditError);
  });

  it("connecting a choice to a step adds a rule instead of a dead goto", () => {
    const src = `workflow W {
    input mode: string = "x"
    step Pick {
        goto match mode {
            "a" -> Done
        }
    }
    step Other {
        complete()
    }
    step Done {
        complete()
    }
}
`;
    const d = doc(src);
    const out = apply(d, connect(d, "W", "Pick", "Other"));
    expect(choice(out, "Pick").arms.map((a) => `${a.pattern}>${a.target}`)).toEqual(['"a">Done', '"other">Other']);
    expect(out.src).not.toContain("goto Other");
    expect(connect(out.doc, "W", "Pick", "Other").edits).toEqual([]);
    expect(() => connect(out.doc, "W", "Pick", "$end")).toThrow(EditError);
  });
});
