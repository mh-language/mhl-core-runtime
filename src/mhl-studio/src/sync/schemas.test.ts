import { describe, expect, it } from "vitest";
import { applyEdits, Doc } from "./edits";
import { moverFor, relinkEdits } from "./relink";
import { agentRun, declareSchema, schemaArg, schemaNameFor, setSchemaArg } from "./schemas";
import { graphOf, MHL } from "./testutil";

const doc = (src: string) => new Doc(src, graphOf(src));
const KINDS = { Ask: "agent" } as const;

const SRC = `agent Ask { command: "echo" }
prompt Review(code: string) from "./review.prompt.md"

workflow W {
    step A {
        var reply = Ask.run(prompt: "go")
        log(reply)
    }
    step B {
        var r = Ask.run(
            prompt: "multi"
        )
    }
    step C {
        var r = Ask.run(prompt: "x", schema: "{}")
    }
}
`;

const site = (d: Doc, step: string) => agentRun(d.graph.workflows[0].nodes.find((n) => n.id === step)!.callSites, KINDS)!;

describe("schemaNameFor", () => {
  it("names a schema after its file", () => {
    expect(schemaNameFor("schemas/code_review.schema.json")).toBe("CodeReview");
    expect(schemaNameFor("/w/answer.json")).toBe("Answer");
  });
});

describe.skipIf(!MHL)("dropping a schema", () => {
  it("declares it after the last prompt-or-before-workflow and the result parses", () => {
    const d = doc(SRC);
    const r = declareSchema(d, "Answer", "./schemas/answer.schema.json");
    const out = applyEdits(SRC, r.edits);
    expect(out).toContain('schema Answer from "./schemas/answer.schema.json"\n\nworkflow W');
    expect(graphOf(out).declarations.find((x) => x.name === "Answer")).toMatchObject({ kind: "schema", detail: "./schemas/answer.schema.json" });
  });

  it("appends schema: to a one-line call, a multi-line one, and replaces an existing one", () => {
    const d = doc(SRC);
    const edits = [site(d, "A"), site(d, "B"), site(d, "C")].flatMap((s) => setSchemaArg(d, s, schemaArg("Answer")).edits);
    const out = applyEdits(SRC, edits);
    expect(out).toContain('Ask.run(prompt: "go", schema: Answer.content)');
    expect(out).toContain('prompt: "multi", schema: Answer.content\n        )');
    expect(out).toContain('Ask.run(prompt: "x", schema: Answer.content)');
    expect(graphOf(out).ok).toBe(true);
  });

  it("keeps a schema's from path valid when its file moves", () => {
    const src = `schema Answer from "schemas/answer.schema.json"\n`;
    const out = applyEdits(src, relinkEdits(doc(src), "/w/main.mh", moverFor("/w/schemas", "/w/shapes")));
    expect(out).toBe(`schema Answer from "./shapes/answer.schema.json"\n`);
  });
});
