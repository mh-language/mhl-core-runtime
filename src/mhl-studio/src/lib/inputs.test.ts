import { describe, expect, it } from "vitest";
import { inputJourney, inputProblem } from "./inputs";
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

describe("inputProblem", () => {
  it("leaves blank, strings and named types to the runtime", () => {
    expect(inputProblem("string[]", "  ")).toBeNull();
    expect(inputProblem("string", "anything")).toBeNull();
    expect(inputProblem("Review", "whatever")).toBeNull();
  });

  it("offers a command line as its words for a string[]", () => {
    expect(inputProblem("string[]", "python3 -m pytest")).toEqual({
      message: 'a JSON list of strings, e.g. ["python3","-m","pytest"]',
      fix: '["python3","-m","pytest"]',
    });
    expect(inputProblem("string[]", '"ls"')?.fix).toBe('["ls"]');
    expect(inputProblem("string[]", '["ls", "-la"]')).toBeNull();
    expect(inputProblem("string[]", "[1]")?.message).toBe("a JSON list of strings");
  });

  it("checks numbers, bools, lists and objects", () => {
    expect(inputProblem("number", "3")).toBeNull();
    expect(inputProblem("number", "three")?.message).toBe("a number");
    expect(inputProblem("bool?", "yes")?.message).toBe("true or false");
    expect(inputProblem("number[]", "1, 2")?.message).toBe("a JSON list, e.g. [1, 2]");
    expect(inputProblem("{ a: string }", "[]")?.message).toBe("a JSON object");
  });
});
