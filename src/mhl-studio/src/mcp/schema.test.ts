import { describe, expect, it } from "vitest";
import { buildArguments, placeholder, schemaFields, valuesFor } from "./schema";
import { hasRunMethods, isActive, isControlTool, pausedCall, runProgress, supportsAsync } from "./model";

// The shape `mhl serve mcp` derives from a workflow's inputs.
const schema = {
  type: "object",
  additionalProperties: false,
  required: ["repo"],
  properties: {
    repo: { type: "string" },
    name: { type: "string", default: "world" },
    times: { type: "number", default: 1 },
    count: { type: "integer" },
    approved: { type: "boolean", default: false },
    level: { type: "string", enum: ["Low", "High"] },
    tags: { type: "array", items: { type: "string" } },
    req: { type: "object", properties: {} },
    note: { type: ["string", "null"] },
  },
};

describe("schemaFields", () => {
  it("maps each property to a field kind", () => {
    const f = schemaFields(schema);
    expect(f.map((x) => [x.name, x.kind, x.type, x.required])).toEqual([
      ["repo", "string", "string", true],
      ["name", "string", "string", false],
      ["times", "number", "number", false],
      ["count", "integer", "integer", false],
      ["approved", "boolean", "boolean", false],
      ["level", "enum", "enum", false],
      ["tags", "json", "string[]", false],
      ["req", "json", "object", false],
      ["note", "string", "string", false],
    ]);
    expect(f.find((x) => x.name === "level")?.variants).toEqual(["Low", "High"]);
  });

  it("tolerates a missing or empty schema", () => {
    expect(schemaFields(undefined)).toEqual([]);
    expect(schemaFields({ type: "object" })).toEqual([]);
  });

  it("says what a blank field means", () => {
    const [repo, name, , count, , , tags] = schemaFields(schema);
    expect(placeholder(repo)).toBe("required");
    expect(placeholder(name)).toBe('default: "world"');
    expect(placeholder(count)).toBe("");
    expect(placeholder(tags)).toBe("JSON");
  });
});

describe("buildArguments", () => {
  const fields = schemaFields(schema);

  it("leaves blank fields out, so the server applies the defaults", () => {
    expect(buildArguments(fields, { repo: "mhl", name: "" })).toEqual({ args: { repo: "mhl" }, errors: {} });
  });

  it("types each value", () => {
    const { args, errors } = buildArguments(fields, {
      repo: "mhl",
      times: " 2.5 ",
      count: "3",
      approved: "true",
      level: "High",
      tags: '["a","b"]',
      req: '{"x":1}',
    });
    expect(errors).toEqual({});
    expect(args).toEqual({ repo: "mhl", times: 2.5, count: 3, approved: true, level: "High", tags: ["a", "b"], req: { x: 1 } });
  });

  it("reports what can't be sent", () => {
    const { errors } = buildArguments(fields, { times: "x", count: "1.5", approved: "yes", tags: "[" });
    expect(errors).toEqual({ repo: "required", times: "not a number", count: "not an integer", approved: "true or false", tags: "not valid JSON" });
  });

  it("round-trips through valuesFor", () => {
    const args = { repo: "mhl", times: 2, approved: false, tags: ["a"] };
    expect(buildArguments(fields, valuesFor(fields, args)).args).toEqual(args);
  });
});

describe("model", () => {
  it("knows which run states still change on their own", () => {
    expect(["pending", "claimed", "queued", "working"].every((s) => isActive(s as never))).toBe(true);
    expect(["paused", "completed", "failed", "canceled", undefined].some((s) => isActive(s as never))).toBe(false);
  });

  it("reads the HTTP transport's run/* advertisement", () => {
    expect(hasRunMethods({ capabilities: { experimental: { "mhl.run": { version: "1" } } } })).toBe(true);
    expect(hasRunMethods({ capabilities: {} })).toBe(false);
    expect(hasRunMethods(null)).toBe(false);
    expect(supportsAsync({ name: "Greet", _meta: { "mhl.run": { async: true } } })).toBe(true);
    expect(supportsAsync({ name: "Greet" })).toBe(false);
    expect(isControlTool("mhl_run_start")).toBe(true);
    expect(isControlTool("Greet")).toBe(false);
  });

  it("reads a paused tools/call reply's _meta", () => {
    const paused = { isError: false, structuredContent: { ok: false }, _meta: { "mhl.run": { state: "paused", reason: "awaiting approval", step: "Ask", resumable: false, hint: "use run/start" } } };
    expect(pausedCall(paused)).toEqual({ reason: "awaiting approval", step: "Ask", hint: "use run/start" });
    expect(pausedCall({ _meta: { "mhl.run": { state: "paused", reason: { gate: 1 } } } })?.reason).toBe('{"gate":1}');
    expect(pausedCall({ isError: false, structuredContent: {} })).toBeNull();
    expect(pausedCall(null)).toBeNull();
  });
});

describe("runProgress", () => {
  it("fills the bar once the run has stopped, whatever step it ended on", () => {
    expect(runProgress({ state: "completed", reached: ["Choose", "Done"], stepTotal: 9 })).toBe(100);
    expect(runProgress({ state: "failed", reached: ["A"], stepTotal: 9 })).toBe(100);
    expect(runProgress({ state: "canceled", stepTotal: 9 })).toBe(100);
  });

  it("counts distinct steps reached while running, never a full bar", () => {
    expect(runProgress({ state: "working", reached: ["A", "B", "A"], stepTotal: 4 })).toBe(50);
    expect(runProgress({ state: "working", reached: ["A", "B", "C", "D"], stepTotal: 4 })).toBe(95);
    expect(runProgress({ state: "working", stepTotal: 4 })).toBe(5);
    expect(runProgress({ state: "paused", reached: ["A"], stepTotal: 2 })).toBe(50);
  });
});
