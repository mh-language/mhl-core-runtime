import { describe, expect, it } from "vitest";
import type { Declaration, Graph } from "../lib/types";
import { enumVariants, inputFields, runInputPairs, stageInputPairs, type State } from "./store";

const decl = (d: Partial<Declaration>) => d as Declaration;
const decls = [decl({ kind: "enum", name: "AgentModel", variants: ["Claude", "Codex", "All"] }), decl({ kind: "type", name: "Req" })];

describe("enumVariants", () => {
  it("lists an enum input's variants in declaration order", () => {
    expect(enumVariants("AgentModel", decls)).toEqual(["Claude", "Codex", "All"]);
  });
  it("treats an optional T? as T", () => {
    expect(enumVariants("AgentModel?", decls)).toEqual(["Claude", "Codex", "All"]);
  });
  it("is null for a non-enum or unknown type", () => {
    expect(enumVariants("string", decls)).toBeNull();
    expect(enumVariants("Req", decls)).toBeNull();
    expect(enumVariants("Missing", decls)).toBeNull();
  });
});

// A workflow whose stages declare their own inputs (`mhl graph` WfInput.stage).
const staged = (runInputs: Record<string, string>, inspection: State["inspection"] = null) =>
  ({
    workflow: "Onboarding",
    inspection,
    runInputs,
    graph: {
      workflows: [
        {
          name: "Onboarding",
          inputs: [
            { name: "customer", type: "string", span: [0, 0] },
            { name: "plan", type: "string", span: [0, 0], stage: "Plan", doc: "The plan to pick." },
            { name: "card", type: "string", span: [0, 0], stage: "Billing" },
            { name: "coupon", type: "string", default: '"none"', span: [0, 0], stage: "Billing" },
          ],
        },
      ],
    } as unknown as Graph,
  }) as unknown as State;

describe("stage inputs", () => {
  it("carry their stage, from the graph or the dry run", () => {
    expect(inputFields(staged({})).map((f) => [f.name, f.stage])).toEqual([
      ["customer", undefined],
      ["plan", "Plan"],
      ["card", "Billing"],
      ["coupon", "Billing"],
    ]);
    const dry = staged({}, { ok: true, dry_run: true, inputs: [{ name: "plan", type: "string", supplied: false, stage: "Plan" }] });
    // The `///` doc comes from the dry run, else from the graph.
    expect(inputFields(dry)).toEqual([{ name: "plan", type: "string", def: undefined, stage: "Plan", doc: "The plan to pick." }]);
    expect(inputFields(staged({})).find((f) => f.name === "plan")?.doc).toBe("The plan to pick.");
  });
  it("a stage's resume passes only that stage's filled-in inputs", () => {
    const s = staged({ customer: "ana", plan: "pro", card: "4242", coupon: "" });
    expect(runInputPairs(s)).toEqual([
      ["customer", "ana"],
      ["plan", "pro"],
      ["card", "4242"],
    ]);
    expect(stageInputPairs("Plan", undefined, s)).toEqual([["plan", "pro"]]);
    expect(stageInputPairs("Billing", undefined, s)).toEqual([["card", "4242"]]);
    expect(stageInputPairs("Billing", ["coupon"], s)).toEqual([]);
  });
});
