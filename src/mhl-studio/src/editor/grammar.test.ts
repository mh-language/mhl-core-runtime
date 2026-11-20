// The mhl TextMate grammar (src/vscode-mhl/syntaxes/mhl.tmLanguage.json),
// tokenized the way the studio's editor does it: shiki with its JavaScript
// regex engine. The same grammar highlights `.mh` in VS Code.
import { describe, expect, it } from "vitest";
import { createHighlighterCore } from "shiki/core";
import { createJavaScriptRegexEngine } from "shiki/engine/javascript";
import grammar from "../../../vscode-mhl/syntaxes/mhl.tmLanguage.json";

async function scopesOf(code: string): Promise<Map<string, string[]>> {
  const h = await createHighlighterCore({
    themes: [],
    langs: [{ ...(grammar as object), name: "mhl" } as never],
    engine: createJavaScriptRegexEngine({ forgiving: true }),
  });
  const out = new Map<string, string[]>();
  for (const line of h.codeToTokensBase(code, { lang: "mhl", includeExplanation: true }))
    for (const t of line)
      for (const e of t.explanation ?? []) {
        const word = e.content.trim();
        if (word) out.set(`${out.has(word) ? word + "#2" : word}`, e.scopes.map((s) => s.scopeName));
      }
  return out;
}

describe("mhl grammar", () => {
  it("highlights a fragment's header stage, after a typed signature too", async () => {
    for (const header of ["partial workflow Story stage Build {", "partial workflow Story(req: In): Out max 3 stage Build {"]) {
      const s = await scopesOf(`${header}\n    step A { log(1) }\n}\n`);
      expect(s.get("stage"), header).toContain("storage.type.declaration.mhl");
      expect(s.get("Build"), header).toContain("entity.name.label.mhl");
    }
  });

  it("leaves `stage` alone where it is a plain name", async () => {
    const s = await scopesOf("partial workflow W {\n    input stage: string = \"x\"\n    step_start: (step) -> { log(step.stage) }\n}\n");
    const plain = [...s].filter(([word]) => word.replace(/^\./, "").startsWith("stage"));
    expect(plain.length).toBeGreaterThanOrEqual(2); // the input's name and the member access
    for (const [, scopes] of plain) expect(scopes).not.toContain("storage.type.declaration.mhl");
  });
});
