import { describe, expect, it } from "vitest";
import { applyEdits, Doc } from "./edits";
import { moverFor, relinkEdits } from "./relink";
import { graphOf, MHL } from "./testutil";

const SRC = `import { Tools } from "./lib/tools.mh"
import "shared.mh"
prompt Review(code: string) from "prompts/review.prompt.md"

workflow W {
    step A { log(Tools.greet(Review(code: "x"))) }
}
`;

const relink = (src: string, path: string, from: string, to: string) => applyEdits(src, relinkEdits(new Doc(src, graphOf(src)), path, moverFor(from, to)));

describe("moverFor", () => {
  it("moves a file, a folder's contents, and nothing else", () => {
    const m = moverFor("/w/lib", "/w/src/lib");
    expect(m("/w/lib/tools.mh")).toBe("/w/src/lib/tools.mh");
    expect(m("/w/lib")).toBe("/w/src/lib");
    expect(m("/w/library.mh")).toBe("/w/library.mh");
    expect(moverFor("C:\\w\\a.mh", "C:\\w\\b.mh")("C:\\w\\a.mh")).toBe("C:/w/b.mh");
  });
});

describe.skipIf(!MHL)("relinkEdits", () => {
  it("follows an imported file that was renamed", () => {
    const out = relink(SRC, "/w/main.mh", "/w/lib/tools.mh", "/w/lib/helpers.mh");
    expect(out).toContain('import { Tools } from "./lib/helpers.mh"');
    expect(out).toContain('import "shared.mh"');
  });

  it("follows a moved folder of prompts", () => {
    const out = relink(SRC, "/w/main.mh", "/w/prompts", "/w/assets/prompts");
    expect(out).toContain('prompt Review(code: string) from "./assets/prompts/review.prompt.md"');
  });

  it("rewrites a moved file's own relative paths", () => {
    const out = relink(SRC, "/w/main.mh", "/w/main.mh", "/w/flows/main.mh");
    expect(out).toContain('from "../lib/tools.mh"');
    expect(out).toContain('import "../shared.mh"');
    expect(out).toContain('from "../prompts/review.prompt.md"');
  });

  it("leaves paths alone when the file and its targets move together", () => {
    expect(relink(SRC, "/w/main.mh", "/w", "/x")).toBe(SRC);
    expect(relink(SRC, "/w/main.mh", "/w/other.mh", "/w/renamed.mh")).toBe(SRC);
  });
});
