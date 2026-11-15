import { describe, expect, it } from "vitest";
import { applyEdits, Doc, EditError, insertState, type EditResult } from "./edits";
import {
  addMethod,
  addTool,
  convertedBody,
  deleteMethod,
  deleteTool,
  findMethod,
  findTool,
  getMethodBody,
  renameTool,
  setMethodBody,
  setSignature,
  signatureOf,
  testCall,
} from "./tools";
import { callText } from "./templates";
import { graphOf, MHL } from "./testutil";

const SRC = `// Paths — ünïcödé comment stays put
tool Paths {
    // builds a project root
    root(id: string, n: number = 2): string -> "projects/" + self.check(id)

    internal check(id: string): string {
        if (!id.matches("^[a-z0-9]+$")) fail("bad id")
        return id
    }
}

workflow W {
    step A {
        log(Paths.root("a"))
    }
}
`;

const doc = (src: string) => new Doc(src, graphOf(src));

/** Applies an edit and checks the result still parses. */
function apply(src: string, r: EditResult): string {
  const out = applyEdits(src, r.edits);
  const g = graphOf(out);
  expect(g.ok, `${out}\n${JSON.stringify(g.diagnostics)}`).toBe(true);
  return out;
}

describe.skipIf(!MHL)("tool edits", () => {
  it("adds a tool after the last tool", () => {
    const r = addTool(doc(SRC));
    const out = apply(SRC, r);
    expect(r.name).toBe("NewTool");
    expect(out).toContain('    greet(name: string): string -> "hello " + name\n}\n\nworkflow W');
    expect(out.indexOf("tool NewTool")).toBeGreaterThan(out.indexOf("tool Paths"));
  });

  it("adds a tool before the first workflow, or to an empty file", () => {
    const src = "workflow W {\n    step A { }\n}\n";
    expect(apply(src, addTool(doc(src)))).toMatch(/^tool NewTool \{[\s\S]*\}\n\nworkflow W/);
    expect(apply("", addTool(doc("")))).toMatch(/^tool NewTool \{[\s\S]*\}\n$/);
  });

  it("renames a tool and its call sites", () => {
    const out = apply(SRC, renameTool(doc(SRC), "Paths", "Dirs"));
    expect(out).toContain("tool Dirs {");
    expect(out).toContain('log(Dirs.root("a"))');
    expect(out).toContain("ünïcödé comment stays put");
    expect(() => renameTool(doc(SRC), "Paths", "W")).toThrow(EditError);
  });

  it("deletes a tool", () => {
    const out = applyEdits(SRC, deleteTool(doc(SRC), "Paths").edits);
    expect(out).not.toContain("tool Paths");
    expect(out.startsWith("// Paths")).toBe(true);
  });

  it("adds and deletes a method", () => {
    const r = addMethod(doc(SRC), "Paths");
    const out = apply(SRC, r);
    expect(out).toContain("        return id\n    }\n    method(input: string): string -> input\n}");
    const back = applyEdits(out, deleteMethod(doc(out), "Paths", r.name).edits);
    expect(back).toBe(SRC);
  });

  it("adds a method to an empty tool", () => {
    const src = "tool T { }\n";
    expect(apply(src, addMethod(doc(src), "T"))).toBe("tool T {\n    method(input: string): string -> input\n}\n");
  });

  it("rewrites a signature and renames call sites", () => {
    const d = doc(SRC);
    const sig = signatureOf(findMethod(findTool(d, "Paths"), "check"));
    expect(sig).toEqual({ name: "check", internal: true, args: [{ name: "id", type: "string", default: "" }], returns: "string" });
    const out = apply(SRC, setSignature(d, "Paths", "check", { ...sig, name: "validate", internal: false, args: [...sig.args, { name: "strict", type: "bool", default: "true" }] }));
    expect(out).toContain("    validate(id: string, strict: bool = true): string {\n");
    expect(out).toContain('"projects/" + self.validate(id)');
    const out2 = apply(SRC, setSignature(d, "Paths", "root", { ...signatureOf(findMethod(findTool(d, "Paths"), "root")), name: "home" }));
    expect(out2).toContain('log(Paths.home("a"))');
    expect(out2).toContain("    // builds a project root\n    home(id: string, n: number = 2): string ->");
  });

  it("refuses a bad signature", () => {
    const d = doc(SRC);
    const sig = signatureOf(findMethod(findTool(d, "Paths"), "root"));
    expect(() => setSignature(d, "Paths", "root", { ...sig, name: "check" })).toThrow(/already has/);
    expect(() => setSignature(d, "Paths", "root", { ...sig, args: [sig.args[1], sig.args[0]] })).toThrow(/needs a default/);
    expect(() => setSignature(d, "Paths", "root", { ...sig, args: [sig.args[0], sig.args[0]] })).toThrow(/twice/);
  });

  it("reads and writes bodies, converting between expression and block", () => {
    const d = doc(SRC);
    expect(getMethodBody(d, "Paths", "root")).toEqual({ kind: "expr", text: '"projects/" + self.check(id)' });
    expect(getMethodBody(d, "Paths", "check")).toEqual({
      kind: "block",
      text: 'if (!id.matches("^[a-z0-9]+$")) fail("bad id")\nreturn id',
    });
    const block = convertedBody("expr", '"projects/" + id');
    const out = apply(SRC, setMethodBody(d, "Paths", "root", block.kind, block.text));
    expect(out).toContain('    root(id: string, n: number = 2): string {\n        return "projects/" + id\n    }\n');
    const back = convertedBody("block", getMethodBody(doc(out), "Paths", "root").text);
    const out2 = apply(out, setMethodBody(doc(out), "Paths", "root", back.kind, back.text));
    expect(out2).toContain('    root(id: string, n: number = 2): string -> "projects/" + id\n');
    expect(() => setMethodBody(d, "Paths", "root", "expr", " ")).toThrow(EditError);
    expect(apply(SRC, setMethodBody(d, "Paths", "check", "block", ""))).toContain("internal check(id: string): string {\n    }");
  });

  it("drops a call to a chosen method as a new step", () => {
    const d = doc(SRC);
    const m = findMethod(findTool(d, "Paths"), "root");
    expect(callText("Paths", m)).toBe('Paths.root("")');
    const out = apply(SRC, insertState(d, "W", "tool", { end: true }, { tool: "Paths", method: m }));
    expect(out).toContain('    step Call {\n        var result = Paths.root("")\n        log(result)\n    }');
  });

  it("builds a test call", () => {
    const m = findMethod(findTool(doc(SRC), "Paths"), "root");
    expect(testCall("Paths", m, ['"a"', ""])).toBe('Paths.root("a")');
    expect(testCall("Paths", m, ['"a"', "3"])).toBe('Paths.root("a", 3)');
    expect(() => testCall("Paths", m, ["", ""])).toThrow(/id is required/);
  });
});
