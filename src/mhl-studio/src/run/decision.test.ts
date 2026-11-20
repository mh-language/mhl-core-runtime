import { describe, expect, it } from "vitest";
import { buildDecision, decisionFields, initialValue } from "./decision";

const REVIEW = {
  type: "object",
  title: "Review",
  properties: {
    action: { type: "string", description: "mhl enum Action", enum: ["Approve", "Revise", "Reject"] },
    feedback: { type: "string" },
    score: { type: "number" },
    urgent: { type: "boolean" },
  },
  required: ["action"],
};

describe("decision form", () => {
  it("has one field per property, enums as options", () => {
    const fields = decisionFields(REVIEW);
    expect(fields.map((f) => [f.name, f.kind, f.required])).toEqual([
      ["action", "enum", true],
      ["feedback", "string", false],
      ["score", "number", false],
      ["urgent", "bool", false],
    ]);
    expect(fields[0].options).toEqual(["Approve", "Revise", "Reject"]);
    expect(initialValue(fields[0])).toBe("Approve");
  });

  it("builds the decision, leaving out empty optional fields", () => {
    const fields = decisionFields(REVIEW);
    expect(buildDecision(fields, { action: "Revise", feedback: "", score: "3", urgent: "true" })).toEqual({ action: "Revise", score: 3, urgent: true });
    expect(() => buildDecision(fields, { action: "" })).toThrow(/action is required/);
    expect(() => buildDecision(fields, { action: "Approve", score: "x" })).toThrow(/number/);
  });

  it("takes a scalar decision as is", () => {
    const fields = decisionFields({ type: "boolean" });
    expect(fields).toEqual([{ name: "", kind: "bool", required: true }]);
    expect(buildDecision(fields, { "": "true" })).toBe(true);
  });
});
