import { describe, expect, it } from "vitest";
import { execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { applyEdits, Doc, insertState, type EditResult } from "./edits";
import { addMerge, removeMerge, sharedWrites } from "./merge";
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
});
