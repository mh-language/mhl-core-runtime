import { describe, expect, it } from "vitest";
import { applyEdits, Doc, insertState } from "./edits";
import { agentRunWithPrompt, declarePrompt, promptCall, promptNameFor, promptParams, setPromptArg } from "./prompts";
import { graphOf, MHL } from "./testutil";

describe("promptParams", () => {
  it("lists unescaped placeholders once, in order, skipping frontmatter", () => {
    const md = "---\ndescription: uses ${ignored}\n---\nReview ${lang} code:\n\n${code}\n\nIn ${lang}. Literal \\${TARGET_DIR}.";
    expect(promptParams(md)).toEqual(["lang", "code"]);
    expect(promptParams("no placeholders")).toEqual([]);
  });

  it("names a prompt after its file", () => {
    expect(promptNameFor("prompts/code_review.prompt.md")).toBe("CodeReview");
    expect(promptNameFor("/w/Greeting.md")).toBe("Greeting");
  });

  it("passes in-scope names through, placeholders otherwise", () => {
    expect(promptCall("Review", ["code", "lang"], ["code"])).toBe('Review(code: code, lang: "")');
    expect(promptCall("Hello", [])).toBe("Hello()");
  });
});

const SRC = `agent Ask { command: "echo" }

workflow W {
    input code: string = "x"
    step A {
        var reply = Ask.run(prompt: "TODO: describe the task")
        log(reply)
    }
}
`;

const doc = (src: string) => new Doc(src, graphOf(src));

describe.skipIf(!MHL)("dropping a prompt", () => {
  it("declares a Markdown prompt before the first workflow", () => {
    const d = doc(SRC);
    const r = declarePrompt(d, "Review", ["code", "lang"], "./prompts/review.prompt.md");
    const out = applyEdits(SRC, r.edits);
    expect(out).toContain('prompt Review(code: string, lang: string) from "./prompts/review.prompt.md"\n\nworkflow W');
    const g = graphOf(out);
    expect(g.declarations.find((x) => x.name === "Review")).toMatchObject({ kind: "prompt", params: ["code", "lang"], detail: "./prompts/review.prompt.md" });
  });

  it("picks a free name and goes after the last prompt", () => {
    const src = `prompt Review(x: string) { "a \${x}" }\n${SRC}`;
    const r = declarePrompt(doc(src), "Review", [], "./r.md");
    expect(r.name).toBe("Review2");
    expect(applyEdits(src, r.edits)).toContain('prompt Review(x: string) { "a ${x}" }\nprompt Review2() from "./r.md"');
  });

  it("swaps the prompt: of a step's agent call", () => {
    const d = doc(SRC);
    const site = agentRunWithPrompt(d.graph.workflows[0].nodes[0].callSites, { Ask: "agent" })!;
    expect(site).toBeDefined();
    const out = applyEdits(SRC, setPromptArg(d, site, promptCall("Review", ["code"], ["code"])).edits);
    expect(out).toContain("var reply = Ask.run(prompt: Review(code: code))");
  });

  it("makes a new agent step running the prompt", () => {
    const out = applyEdits(SRC, insertState(doc(SRC), "W", "agent", { end: true }, undefined, "Ask", 'Review(code: "")').edits);
    expect(out).toContain('Ask.run(prompt: Review(code: ""))');
  });
});
