import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { applyEdits, Doc, insertState } from "./edits";
import { addImport, exportDeclaration, importOrigins, importSpec, isImported, resolveImport } from "./imports";
import { newFileSource } from "./templates";
import { graphOf, MHL } from "./testutil";

describe("import paths", () => {
  it("computes a relative spec and resolves it back", () => {
    expect(importSpec("/w/main.mh", "/w/tools.mh")).toBe("./tools.mh");
    expect(importSpec("/w/main.mh", "/w/lib/tools.mh")).toBe("./lib/tools.mh");
    expect(importSpec("/w/flows/main.mh", "/w/lib/tools.mh")).toBe("../lib/tools.mh");
    expect(importSpec("C:\\w\\flows\\main.mh", "C:\\w\\tools.mh")).toBe("../tools.mh");
    expect(resolveImport("/w/flows/main.mh", "../lib/./tools.mh")).toBe("/w/lib/tools.mh");
    expect(resolveImport("/w/main.mh", "tools.mh")).toBe("/w/tools.mh");
  });
});

const doc = (src: string) => new Doc(src, graphOf(src));

describe.skipIf(!MHL)("addImport", () => {
  const MAIN = "/w/main.mh";
  const WF = `// Orders — the main flow

workflow W {
    step A { }
}
`;

  it("adds the first import after the leading comment", () => {
    const out = applyEdits(WF, addImport(doc(WF), MAIN, "Paths", "/w/lib/tools.mh").edits);
    expect(out).toBe(`// Orders — the main flow\n\nimport { Paths } from "./lib/tools.mh"\n\nworkflow W {\n    step A { }\n}\n`);
  });

  it("adds at the top of a file without a comment", () => {
    const src = "workflow W {\n    step A { }\n}\n";
    expect(applyEdits(src, addImport(doc(src), MAIN, "Paths", "/w/tools.mh").edits)).toBe(`import { Paths } from "./tools.mh"\n\n${src}`);
  });

  it("merges into an import of the same file, and skips what is already imported", () => {
    const src = `import { Paths } from "./tools.mh"\nimport { Other } from "./other.mh"\n\n${WF}`;
    const d = doc(src);
    expect(isImported(d.graph, MAIN, "Paths", "/w/tools.mh")).toBe(true);
    expect(isImported(d.graph, MAIN, "Clock", "/w/tools.mh")).toBe(false);
    expect(addImport(d, MAIN, "Paths", "/w/tools.mh").edits).toEqual([]);
    expect(applyEdits(src, addImport(d, MAIN, "Clock", "/w/tools.mh").edits)).toContain('import { Paths, Clock } from "./tools.mh"\n');
    expect(applyEdits(src, addImport(d, MAIN, "Writer", "/w/agents.mh").edits)).toContain(
      'import { Other } from "./other.mh"\nimport { Writer } from "./agents.mh"\n',
    );
  });

  it("treats a whole-file import as importing everything", () => {
    const src = `import "./tools.mh"\n\n${WF}`;
    expect(isImported(doc(src).graph, MAIN, "Anything", "/w/tools.mh")).toBe(true);
  });

  it("refuses a name already declared here", () => {
    const src = `tool Paths {\n    a() -> 1\n}\n\n${WF}`;
    expect(() => addImport(doc(src), MAIN, "Paths", "/w/tools.mh")).toThrow(/already declared/);
  });

  it("exports a declaration in its own file", () => {
    const src = "// lib\ntool Paths {\n    a() -> 1\n}\n";
    const out = applyEdits(src, exportDeclaration(doc(src), "Paths").edits);
    expect(out).toBe("// lib\nexport tool Paths {\n    a() -> 1\n}\n");
    expect(exportDeclaration(doc(out), "Paths").edits).toEqual([]);
  });

  it("imports a tool file and calls it from a step — and mhl accepts it", () => {
    const dir = mkdtempSync(join(tmpdir(), "mhl-studio-import-"));
    try {
      mkdirSync(join(dir, "lib"));
      const lib = join(dir, "lib", "paths.mh");
      writeFileSync(lib, newFileSource("tool", "Paths"));
      const libGraph = graphOf(newFileSource("tool", "Paths"));
      const tool = libGraph.declarations.find((d) => d.name === "Paths")!;
      expect(tool.exported).toBe(true);

      const main = join(dir, "main.mh");
      const d = new Doc(WF, graphOf(WF), [tool]);
      const imp = addImport(d, main, "Paths", lib);
      const step = insertState(d, "W", "tool", { end: true });
      const out = applyEdits(WF, [...imp.edits, ...step.edits]);
      expect(out).toContain('import { Paths } from "./lib/paths.mh"');
      expect(out).toContain('var result = Paths.greet("")');
      writeFileSync(main, out);
      expect(execFileSync(MHL!, ["lint", dir], { encoding: "utf8" })).toContain("No problems found");
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});

describe("importOrigins", () => {
  const span: [number, number] = [0, 0];
  const graph = {
    imports: [
      { from: "./all.mh", names: [], locals: [], span },
      { from: "../lib/tools.mh", names: ["Tools", "Reviewer"], locals: ["Tools", "R"], span },
    ],
  };
  const library = [
    { path: "/w/all.mh", declarations: [{ name: "Store" }, { name: "Tools" }] },
    { path: "/w/lib/tools.mh", declarations: [{ name: "Tools" }, { name: "Reviewer" }] },
  ];

  it("keys each origin by the local name, aliases included", () => {
    const o = importOrigins(graph, "/w/flows/main.mh", library);
    expect(o.get("R")).toEqual({ from: "../lib/tools.mh", path: "/w/lib/tools.mh", name: "Reviewer" });
    expect(o.has("Reviewer")).toBe(false);
  });

  it("expands a whole-file import from the library, but a named import wins", () => {
    const o = importOrigins(graph, "/w/main.mh", library);
    expect(o.get("Store")?.from).toBe("./all.mh");
    const named = importOrigins(graph, "/w/flows/main.mh", [{ path: "/w/flows/all.mh", declarations: [{ name: "Tools" }] }, library[1]]);
    expect(named.get("Tools")?.from).toBe("../lib/tools.mh");
  });

  it("falls back to the resolved path for a file outside the library", () => {
    expect(importOrigins(graph, "/w/flows/main.mh", []).get("Tools")?.path).toBe("/w/lib/tools.mh");
  });
});
