import { describe, expect, it } from "vitest";
import { pausedCall } from "./model";

describe("pausedCall", () => {
  it("carries a gate's decision name and doc", () => {
    const result = {
      _meta: {
        "mhl.run": {
          state: "paused",
          reason: "review it",
          awaiting: { step: "Review", index: 0, type: "bool", name: "approved", description: "true publishes it" },
        },
      },
    };
    expect(pausedCall(result)?.awaiting).toEqual({ step: "Review", index: 0, type: "bool", schema: undefined, name: "approved", description: "true publishes it" });
  });

  it("leaves them out when the gate binds no name", () => {
    const result = { _meta: { "mhl.run": { state: "paused", awaiting: { step: "S", index: 1, type: "Review" } } } };
    expect(pausedCall(result)?.awaiting).toEqual({ step: "S", index: 1, type: "Review", schema: undefined });
  });
});
