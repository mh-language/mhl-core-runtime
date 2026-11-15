import { describe, expect, it } from "vitest";
import { cleanAmount, formatDuration, parseDuration } from "./duration";

describe("duration", () => {
  it("parses a timeout clause value", () => {
    expect(parseDuration("30s")).toEqual({ amount: "30", unit: "s" });
    expect(parseDuration("250ms")).toEqual({ amount: "250", unit: "ms" });
    expect(parseDuration("2d")).toEqual({ amount: "2", unit: "d" });
    expect(parseDuration("")).toBeNull();
    expect(parseDuration(undefined)).toBeNull();
    expect(parseDuration("1.5h")).toBeNull();
  });

  it("accepts only a positive whole number", () => {
    expect(cleanAmount("1.5")).toBe("15");
    expect(cleanAmount("-3")).toBe("3");
    expect(cleanAmount("007")).toBe("7");
    expect(cleanAmount("0")).toBe("");
    expect(formatDuration("45", "m")).toBe("45m");
    expect(formatDuration("0", "s")).toBe("");
    expect(formatDuration("", "h")).toBe("");
  });
});
