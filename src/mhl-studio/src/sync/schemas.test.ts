import { execFileSync } from "node:child_process";
import { chmodSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { applyEdits, Doc } from "./edits";
import { moverFor, relinkEdits } from "./relink";
import { agentRun, declareSchema, promptWithSchema, SCHEMA_NOTE, schemaArg, schemaNameFor, schemaOnStdin, setSchemaArg, setSchemaInPrompt } from "./schemas";
import { newFileSource } from "./templates";
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

describe.skipIf(!MHL)("teaching an agent to read a schema (schemaOnStdin)", () => {
  const agentOf = (d: Doc, name: string) => d.graph.declarations.find((x) => x.kind === "agent" && x.name === name)!;
  const taught = (src: string, name: string) => {
    const d = doc(src);
    const r = schemaOnStdin(d, name);
    expect(r).not.toBeNull();
    return applyEdits(src, r!.edits);
  };

  it("appends the schema to the generic template's stdin, after the prompt", () => {
    const src = newFileSource("agent", "Agents");
    expect(agentOf(doc(src), "Agents").schema).toBe("none");
    const out = taught(src, "Agents");
    expect(out).toContain(`stdin: "\${prompt}\\n\\n${SCHEMA_NOTE}\\n\${schema}"`);
    expect(agentOf(doc(out), "Agents").schema).toBe("stdin");
  });

  it("extends a triple-quoted stdin, and one with an interpolation holding quotes", () => {
    const tq = taught('agent A {\n    command: "x"\n    stdin: """Do it:\n${prompt}"""\n}\n', "A");
    expect(tq).toContain(`\${prompt}\n\n${SCHEMA_NOTE}\n\${schema}"""`);
    expect(agentOf(doc(tq), "A").schema).toBe("stdin");
    const q = taught('agent A {\n    command: "x"\n    stdin: "a \\"b\\" ${prompt}"\n}\n', "A");
    expect(agentOf(doc(q), "A").schema).toBe("stdin");
  });

  it("refuses an agent with no literal stdin", () => {
    expect(schemaOnStdin(doc('agent A {\n    command: "x"\n    args: ["${prompt}"]\n}\n'), "A")).toBeNull();
    expect(schemaOnStdin(doc('agent A {\n    command: "x"\n    // stdin: "${prompt}"\n}\n'), "A")).toBeNull();
  });

  it("runs: the CLI gets the prompt, the note and the schema's JSON on stdin", () => {
    const dir = mkdtempSync(join(tmpdir(), "mhl-schema-"));
    try {
      const fake = join(dir, "fakecli");
      writeFileSync(fake, '#!/bin/sh\ncat\n');
      chmodSync(fake, 0o755);
      writeFileSync(join(dir, "answer.json"), '{"type":"object"}');
      const agent = taught(newFileSource("agent", "Agents"), "Agents").replace('command: "claude"', `command: ${JSON.stringify(fake)}`);
      writeFileSync(
        join(dir, "main.mh"),
        `${agent}\nschema Answer from "./answer.json"\n\nworkflow W {\n    step A {\n        log(Agents.run(prompt: "PING", schema: Answer.content))\n    }\n}\n`,
      );
      const out = execFileSync(MHL!, ["run", "main.mh"], { cwd: dir, encoding: "utf8" });
      expect(out).toContain(`PING\n\n${SCHEMA_NOTE}\n{"type":"object"}`);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});

describe("promptWithSchema", () => {
  const fold = ` + "\\n\\n${SCHEMA_NOTE}\\n" + `;
  it("appends the schema after a literal, a call or a concatenation", () => {
    expect(promptWithSchema('"go"', "A.content")).toBe(`"go"${fold}A.content`);
    expect(promptWithSchema('Review(code: "a > b")', "A.content")).toBe(`Review(code: "a > b")${fold}A.content`);
    expect(promptWithSchema('"a" + b', "A.content")).toBe(`"a" + b${fold}A.content`);
  });
  it("keeps a lower-precedence expression whole", () => {
    expect(promptWithSchema("p ?? \"x\"", "A.content")).toBe(`(p ?? "x")${fold}A.content`);
  });
  it("replaces a schema folded in before", () => {
    expect(promptWithSchema(promptWithSchema('"go"', "A.content"), "B.content")).toBe(`"go"${fold}B.content`);
  });
});

describe.skipIf(!MHL)("folding a schema into an argv-prompt agent's call", () => {
  // The old generic template: the prompt goes in argv, nowhere to put a schema.
  const AGENT = 'agent Agents {\n    command: "claude"\n    args: ["-p", "${prompt}"]\n    timeout: 5m\n}\n';
  const SRC = `${AGENT}prompt Review(code: string) from "./review.md"\nschema Answer from "./answer.json"\n\nworkflow W {\n    step Ask {\n        var reply = Agents.run(prompt: Review(code: "x"))\n        log(reply)\n    }\n}\n`;

  it("has no stdin to teach, and the folded call parses and runs with the schema in the prompt", () => {
    const d = doc(SRC);
    expect(schemaOnStdin(d, "Agents")).toBeNull();
    const site = agentRun(d.graph.workflows[0].nodes[0].callSites, { Agents: "agent" })!;
    const out = applyEdits(SRC, setSchemaInPrompt(d, site, "Answer.content").edits);
    expect(graphOf(out).ok).toBe(true);
    const dir = mkdtempSync(join(tmpdir(), "mhl-fold-"));
    try {
      const fake = join(dir, "fakecli");
      writeFileSync(fake, '#!/bin/sh\nfor a in "$@"; do printf "[%s]" "$a"; done\n');
      chmodSync(fake, 0o755);
      writeFileSync(join(dir, "answer.json"), '{"type":"object"}');
      writeFileSync(join(dir, "review.md"), "Review ${code}");
      writeFileSync(join(dir, "main.mh"), out.replace('command: "claude"', `command: ${JSON.stringify(fake)}`));
      const res = execFileSync(MHL!, ["run", "main.mh"], { cwd: dir, encoding: "utf8" });
      expect(res).toContain(`[-p][Review x\n\n${SCHEMA_NOTE}\n{"type":"object"}]`);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});
