import { describe, expect, it } from "vitest";
import { inputJourney } from "./inputs";
import type { WfInput } from "./types";

const input = (name: string, stage?: string): WfInput => ({ name, type: "string", span: [0, 0], stage });

describe("inputJourney", () => {
  it("is Start, then every stage in run order with its inputs — none included", () => {
    const groups = inputJourney([input("a"), input("v", "Impl"), input("m", "Spec"), input("b")], [{ name: "Spec" }, { name: "Impl" }, { name: "Done" }]);
    expect(groups.map((g) => [g.stage, g.inputs.map((i) => i.name)])).toEqual([
      [undefined, ["a", "b"]],
      ["Spec", ["m"]],
      ["Impl", ["v"]],
      ["Done", []],
    ]);
  });
  it("keeps a stage input whose stage the graph does not list", () => {
    expect(inputJourney([input("v", "Impl")]).map((g) => g.stage)).toEqual([undefined, "Impl"]);
  });
});
