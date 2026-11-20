import { describe, expect, it } from "vitest";
import { END, FAIL } from "../lib/types";
import { applyEdits, connect, Doc, EditError, insertState } from "./edits";
import { allowsGoto, stateAllowed, transitionAllowed } from "./kinds";
import { PALETTE } from "./templates";
import { graphOf, MHL } from "./testutil";

const pipeline = { kind: "pipeline" as const, name: "P" };
const workflow = { kind: "workflow" as const, name: "W" };

describe("pipeline vs workflow rules", () => {
  it("only a workflow takes goto", () => {
    expect(allowsGoto(workflow)).toBe(true);
    expect(allowsGoto(pipeline)).toBe(false);
  });
  it("keeps Choice (a goto match) out of a pipeline, and nothing else", () => {
    expect(PALETTE.filter((p) => !stateAllowed(pipeline, p.kind)).map((p) => p.kind)).toEqual(["choice"]);
    expect(PALETTE.every((p) => stateAllowed(workflow, p.kind))).toBe(true);
  });
  it("lets a pipeline draw a transition only to End or Fail", () => {
    expect(transitionAllowed(pipeline, END)).toBe(true);
    expect(transitionAllowed(pipeline, FAIL)).toBe(true);
    expect(transitionAllowed(pipeline, "Ship")).toBe(false);
    expect(transitionAllowed(workflow, "Ship")).toBe(true);
  });
});

const SRC = `pipeline Build {
    step Fetch {
        log("fetch")
    }

    step Ship {
        log("ship")
    }
}
`;

describe.skipIf(!MHL)("a pipeline's edits never need goto", () => {
  const doc = () => new Doc(SRC, graphOf(SRC));

  it.each(PALETTE.filter((p) => p.kind !== "choice").map((p) => p.kind))("inserts %s with code lint accepts", (kind) => {
    const src = applyEdits(SRC, insertState(doc(), "Build", kind, { after: "Fetch" }).edits);
    const g = graphOf(src);
    expect(g.ok, src).toBe(true);
    expect(g.diagnostics, src).toEqual([]);
  });

  it("refuses a Choice, saying why", () => {
    expect(() => insertState(doc(), "Build", "choice", { after: "Fetch" })).toThrow(EditError);
    expect(() => insertState(doc(), "Build", "choice", { after: "Fetch" })).toThrow(/needs a workflow/);
  });

  it("refuses a goto between steps, but connects to End", () => {
    expect(() => connect(doc(), "Build", "Fetch", "Ship")).toThrow(/only allowed in a workflow/);
    const src = applyEdits(SRC, connect(doc(), "Build", "Fetch", END).edits);
    expect(src).toContain("complete()");
    expect(graphOf(src).diagnostics).toEqual([]);
  });
});
