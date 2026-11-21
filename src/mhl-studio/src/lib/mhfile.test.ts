import { describe, expect, it } from "vitest";
import { isMhPath, stripMhExt } from "./mhfile";

describe("mhfile", () => {
  it("accepts .mh and its .mhl alias", () => {
    for (const yes of ["a.mh", "flows/a.mhl", "x.partial.mh"]) expect(isMhPath(yes), yes).toBe(true);
    for (const no of ["a.md", "a.mhx", "a.mh.bak", "a.prompt.md", "mh"]) expect(isMhPath(no), no).toBe(false);
  });
  it("strips the extension", () => {
    expect(stripMhExt("flows/a.mh")).toBe("flows/a");
    expect(stripMhExt("flows/a.mhl")).toBe("flows/a");
    expect(stripMhExt("notes.md")).toBe("notes.md");
  });
});
