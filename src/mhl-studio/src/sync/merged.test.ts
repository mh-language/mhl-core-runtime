import { execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { applyEdits, connect, deleteNode, Doc, insertState, localGraph, renameNode } from "./edits";
import type { Graph } from "../lib/types";
import { MHL } from "./testutil";

const MAIN = `import "part.mh"
partial workflow W {
    input x: string = "1"
    entry step A { log(x) }
}
`;
const PART = `partial workflow W {
    var y = 0
    step B { goto A }
}
`;

function merged(src = MAIN): Graph {
  const dir = mkdtempSync(join(tmpdir(), "studio-merged-"));
  writeFileSync(join(dir, "main.mh"), src);
  writeFileSync(join(dir, "part.mh"), PART);
  const out = execFileSync(MHL!, ["graph", join(dir, "main.mh"), "--stdin", "--merged"], { input: src, encoding: "utf8" });
  return JSON.parse(out) as Graph;
}

describe.skipIf(!MHL)("a merged partial workflow", () => {
  it("shows every fragment, the other file's members marked", () => {
    const wf = merged().workflows[0];
    expect(wf.merged).toBe(true);
    expect(wf.nodes.map((n) => [n.id, n.file ? "other" : "here"])).toEqual([
      ["A", "here"],
      ["B", "other"],
    ]);
    expect(wf.vars[0].file).toMatch(/part\.mh$/);
  });

  it("is edited as this file alone: other fragments are externals", () => {
    const local = localGraph(merged()).workflows[0];
    expect(local.nodes.map((n) => n.id)).toEqual(["A"]);
    expect(local.vars).toEqual([]);
    expect(local.externals).toEqual(["B"]);
    expect(local.edges.every((e) => e.from !== "B")).toBe(true);
  });

  it("applies an edit to this file's text only", () => {
    const g = merged();
    const d = new Doc(MAIN, g);
    const r = insertState(d, "W", "pass", { after: "A" });
    const next = applyEdits(MAIN, r.edits);
    expect(next).toContain("step Step");
    expect(next).not.toContain("step B");
    const again = merged(next).workflows[0];
    expect(again.nodes.map((n) => n.id)).toEqual(["A", "Step", "B"]);
    // A name another fragment declares is taken, not reused.
    expect(() => renameNode(new Doc(MAIN, g), "W", "A", "B")).toThrow(/already exists/);
  });
});

describe.skipIf(!MHL)("edits a merged workflow can't follow into another fragment", () => {
  it("refuses to rename or delete a step another fragment goes to", () => {
    const d = new Doc(MAIN, merged());
    expect(() => renameNode(d, "W", "A", "Start")).toThrow(/goto target in part\.mh/);
    expect(() => deleteNode(d, "W", "A")).toThrow(/goto target in part\.mh/);
  });
});

describe.skipIf(!MHL)("a fragment's stage", () => {
  const SRC = `partial workflow W stage Build {
    entry step Compile {
        log("c")
    }

    step Check {
        log("k")
    }
}
`;
  const graph = (src: string) => JSON.parse(execFileSync(MHL!, ["graph", "w.mh", "--stdin"], { input: src, encoding: "utf8" })) as Graph;

  it("holds every step of the fragment", () => {
    const wf = graph(SRC).workflows[0];
    expect(wf.stages?.map((s) => [s.name, s.members])).toEqual([["Build", ["Compile", "Check"]]]);
    expect(wf.nodes.every((n) => n.stage === "Build")).toBe(true);
  });

  it("is a transition target: drawn onto the stage, a transition writes `goto <stage>`", () => {
    const out = applyEdits(SRC, connect(new Doc(SRC, graph(SRC)), "W", "Check", "Build").edits);
    expect(graph(out).workflows[0].edges.some((e) => e.from === "Check" && e.to === "Build" && e.kind === "goto")).toBe(true);
  });

  it("stays when its last step is deleted — only the step goes", () => {
    const one = applyEdits(SRC, deleteNode(new Doc(SRC, graph(SRC)), "W", "Check").edits);
    const out = applyEdits(one, deleteNode(new Doc(one, graph(one)), "W", "Compile").edits);
    expect(out).toContain("partial workflow W stage Build {");
    expect(out).not.toContain("step");
  });

  it("is a taken name", () => {
    expect(() => renameNode(new Doc(SRC, graph(SRC)), "W", "Check", "Build")).toThrow(/already exists/);
  });
});
