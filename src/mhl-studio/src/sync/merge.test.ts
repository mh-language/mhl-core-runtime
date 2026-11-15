import { describe, expect, it } from "vitest";
import { execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { applyEdits, connect, Doc, insertState, type EditResult } from "./edits";
import { addGroupVar, addMerge, removeGroupVar, removeMerge, sharedWrites } from "./merge";
import { graphOf, MHL } from "./testutil";

const FAN = `workflow W {
    var reply = []
    var summary = ""

    parallel Fan {
        step Left {
            self.reply = [...self.reply, "claude"]
            summary = "left"
        }
        step Right {
            self.reply = [...self.reply, "codex"]
            self.summary = "right"
        }
    }

    step Done {
        log("\${reply}")
    }
}
`;

const doc = (src: string) => new Doc(src, graphOf(src));

function apply(d: Doc, r: EditResult) {
  const src = applyEdits(d.src, r.edits);
  const g = graphOf(src);
  expect(g.ok, `${src}\n${JSON.stringify(g.diagnostics)}`).toBe(true);
  return { src, wf: g.workflows[0], doc: new Doc(src, g) };
}

/** Runs src with the real mhl and returns its output. */
function run(src: string): string {
  const dir = mkdtempSync(join(tmpdir(), "mhl-merge-"));
  writeFileSync(join(dir, "w.mh"), src);
  return execFileSync(MHL!, ["run", "w.mh"], { cwd: dir, encoding: "utf8" });
}

describe.skipIf(!MHL)("merge", () => {
  it("finds the vars more than one branch assigns", () => {
    const d = doc(FAN);
    const wf = d.graph.workflows[0];
    expect(sharedWrites(d, wf, d.node(wf, "Fan"))).toEqual([
      { name: "reply", by: ["Left", "Right"] },
      { name: "summary", by: ["Left", "Right"] },
    ]);
  });

  it("adds a merge that concatenates an array var, and the run then succeeds", () => {
    const d = doc(FAN.replace(/\n\s*(self\.)?summary = "\w+"/g, ""));
    const out = apply(d, addMerge(d, "W", "Fan"));
    expect(out.src).toContain("merge (branches) {\n            self.reply = [...branches.Left.reply, ...branches.Right.reply]\n        }\n    }");
    const fan = out.wf.nodes.find((n) => n.id === "Fan")!;
    expect(fan.merge?.stepName).toBe("Fan.merge");
    expect(run(out.src)).toContain('["claude","codex"]');
  });

  it("leaves a TODO for a non-array var", () => {
    const d = doc(FAN);
    const out = apply(d, addMerge(d, "W", "Fan"));
    expect(out.src).toContain("self.summary = branches.Left.summary  // TODO: combine with branches.Right.summary");
  });

  it("puts a new branch before the merge, and removes the merge cleanly", () => {
    const d = doc(FAN);
    const merged = apply(d, addMerge(d, "W", "Fan"));
    const more = apply(merged.doc, insertState(merged.doc, "W", "pass", { into: "Fan" }));
    const fan = more.wf.nodes.find((n) => n.id === "Fan")!;
    expect(fan.children).toHaveLength(3);
    expect(fan.merge).toBeDefined();
    const back = apply(more.doc, removeMerge(more.doc, "W", "Fan"));
    expect(back.wf.nodes.find((n) => n.id === "Fan")!.merge).toBeUndefined();
    expect(back.src).not.toContain("merge");
  });

  it("counts `x.append(...)` statements as writes", () => {
    const src = FAN.replace(/self\.reply = \[\.\.\.self\.reply, ("\w+")\]/g, "self.reply.append($1)");
    const d = doc(src);
    const wf = d.graph.workflows[0];
    expect(sharedWrites(d, wf, d.node(wf, "Fan")).map((w) => w.name)).toEqual(["reply", "summary"]);
  });

  it("connecting a group writes the goto into its merge, adding one when needed", () => {
    const src = FAN.replace(/\n\s*(self\.)?summary = "\w+"/g, "").replace("    step Done {", "    step Skip {\n        log(\"skip\")\n    }\n\n    step Done {");
    const d = doc(src);
    const added = apply(d, connect(d, "W", "Fan", "Done"));
    const fan = added.wf.nodes.find((n) => n.id === "Fan")!;
    expect(fan.merge).toBeDefined();
    expect(added.src).toContain("self.reply = [...branches.Left.reply, ...branches.Right.reply]\n            goto Done\n        }");
    expect(added.wf.edges.filter((e) => e.from === "Fan").map((e) => `${e.kind}:${e.to}`)).toEqual(["goto:Done"]);
    // Retargeting rewrites that goto in place.
    const moved = apply(added.doc, connect(added.doc, "W", "Fan", "Skip"));
    expect(moved.wf.edges.filter((e) => e.from === "Fan").map((e) => `${e.kind}:${e.to}`)).toEqual(["goto:Skip"]);
  });

  it("adds and removes group vars, and a merge without branches reads them through self", () => {
    const src = `workflow W {
    var reply = []

    parallel Fan {
        step Left {
            log("l")
        }
        step Right {
            log("r")
        }
    }
}
`;
    const d = doc(src);
    const one = apply(d, addGroupVar(d, "W", "Fan", "left", '""'));
    expect(one.src).toContain('    parallel Fan {\n        var left = ""\n\n        step Left {');
    const two = apply(one.doc, addGroupVar(one.doc, "W", "Fan", "right", ""));
    expect(two.wf.nodes.find((n) => n.id === "Fan")!.vars!.map((v) => v.name)).toEqual(["left", "right"]);
    expect(() => addGroupVar(two.doc, "W", "Fan", "reply", "1")).toThrow(/workflow input or var/);
    expect(() => addGroupVar(two.doc, "W", "Fan", "left", "1")).toThrow(/already has a var/);

    const merged = apply(two.doc, addMerge(two.doc, "W", "Fan"));
    expect(merged.src).toContain("merge {\n            // runs after every branch; the group's vars: self.left, self.right\n        }");
    expect(merged.wf.nodes.find((n) => n.id === "Fan")!.merge!.param).toBe("");

    const back = apply(merged.doc, removeGroupVar(merged.doc, "W", "Fan", "left"));
    expect(back.wf.nodes.find((n) => n.id === "Fan")!.vars!.map((v) => v.name)).toEqual(["right"]);
  });

  it("two branches writing one group var is a shared write too", () => {
    const src = `workflow W {
    parallel Fan {
        var tally = []
        step Left {
            self.tally.append("l")
        }
        step Right {
            self.tally.append("r")
        }
    }
}
`;
    const d = doc(src);
    const wf = d.graph.workflows[0];
    expect(sharedWrites(d, wf, d.node(wf, "Fan"))).toEqual([{ name: "tally", by: ["Left", "Right"] }]);
    const out = apply(d, addMerge(d, "W", "Fan"));
    expect(out.src).toContain("merge (branches) {\n            self.tally = [...branches.Left.tally, ...branches.Right.tally]");
  });
});
