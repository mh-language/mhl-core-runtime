import { describe, expect, it } from "vitest";
import type { Declaration } from "../lib/types";
import { enumVariants } from "./store";

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
