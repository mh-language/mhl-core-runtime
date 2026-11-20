import { describe, expect, it } from "vitest";
import { toolMethod } from "./store";
import type { McpTool } from "./model";

const asyncTool: McpTool = { name: "W", _meta: { "mhl.run": { version: 1 } } };
const stdioTool: McpTool = { name: "W" };

describe("toolMethod", () => {
  it("sends run/start only where the tool advertises it", () => {
    expect(toolMethod({ tools: [asyncTool], methods: { W: "run/start" } }, "W")).toBe("run/start");
    expect(toolMethod({ tools: [asyncTool], methods: {} }, "W")).toBe("tools/call");
  });

  it("falls back to tools/call over stdio, even when run/start was chosen on HTTP", () => {
    expect(toolMethod({ tools: [stdioTool], methods: { W: "run/start" } }, "W")).toBe("tools/call");
    expect(toolMethod({ tools: [], methods: { W: "run/start" } }, "W")).toBe("tools/call");
  });
});
