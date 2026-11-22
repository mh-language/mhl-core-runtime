import { describe, expect, it } from "vitest";
import { canPause, resumeParams, toolMethod } from "./store";
import type { McpTool, RunStatus } from "./model";

const asyncTool: McpTool = { name: "W", _meta: { "mhl.run": { version: 1 } } };
const stdioTool: McpTool = { name: "W" };

describe("toolMethod", () => {
  it("sends run/start only where the tool advertises it", () => {
    expect(toolMethod({ tools: [asyncTool], methods: { W: "run/start" } }, "W")).toBe("run/start");
    expect(toolMethod({ tools: [asyncTool], methods: {} }, "W")).toBe("tools/call");
  });

  it("runs a workflow that may pause with run/start only — a tools/call can't resume", () => {
    const staged: McpTool = {
      name: "W",
      _meta: { "mhl.run": { version: 1 } },
      inputSchema: { type: "object", properties: { v: { type: "string", "x-mhl-stage": "Impl" } } },
    };
    const gated: McpTool = { name: "W", _meta: { "mhl.run": { version: 1, pauses: { steps: ["Approve"], stages: [] } } } };
    for (const t of [staged, gated]) {
      expect(canPause(t)).toBe(true);
      expect(toolMethod({ tools: [t], methods: {} }, "W")).toBe("run/start");
      // An earlier choice of tools/call does not stick.
      expect(toolMethod({ tools: [t], methods: { W: "tools/call" } }, "W")).toBe("run/start");
    }
    // Over stdio there is only tools/call.
    expect(toolMethod({ tools: [{ ...staged, _meta: undefined }], methods: {} }, "W")).toBe("tools/call");
    // A workflow that never pauses keeps the choice.
    expect(canPause(asyncTool)).toBe(false);
    expect(toolMethod({ tools: [asyncTool], methods: { W: "run/start" } }, "W")).toBe("run/start");
  });

  it("falls back to tools/call over stdio, even when run/start was chosen on HTTP", () => {
    expect(toolMethod({ tools: [stdioTool], methods: { W: "run/start" } }, "W")).toBe("tools/call");
    expect(toolMethod({ tools: [], methods: { W: "run/start" } }, "W")).toBe("tools/call");
  });
});

describe("resumeParams", () => {
  const tool: McpTool = {
    name: "W",
    inputSchema: { type: "object", required: ["who"], properties: { who: { type: "string" }, plan: { type: "string", "x-mhl-stage": "Plan" } } },
  };
  const paused = (extra: Partial<RunStatus>): RunStatus => ({ runId: "r1", tool: "W", state: "paused", ...extra });

  it("answers a typed gate with the decision alone", () => {
    const st = paused({ awaiting: { step: "Gate", index: 0, type: "Review" } });
    expect(resumeParams(st, tool, { who: "x" }, { action: "Approve" })).toEqual({ runId: "r1", decision: { action: "Approve" } });
  });

  it("sends only a stage's inputs, and waits until they are filled in", () => {
    const st = paused({ awaiting_inputs: { stage: "Plan", step: "Choose", inputs: ["plan"] } });
    expect(resumeParams(st, tool, { who: "x" })).toBeNull();
    expect(resumeParams(st, tool, { who: "x", plan: "pro" })).toEqual({ runId: "r1", arguments: { plan: "pro" } });
  });

  it("otherwise merges whatever arguments were filled in", () => {
    expect(resumeParams(paused({}), tool, { who: "y" })).toEqual({ runId: "r1", arguments: { who: "y" } });
  });
});
