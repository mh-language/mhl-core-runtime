import { describe, expect, it } from "vitest";
import { ancestors, buildTree, type TreeNode } from "./tree";

const shape = (nodes: TreeNode[]): unknown[] => nodes.map((n) => (n.kind === "dir" ? { [n.name + "/"]: shape(n.children) } : n.name));

describe("buildTree", () => {
  it("nests files under their folders, folders first, sorted naturally", () => {
    const t = buildTree(["main.mh", "lib/tools.mh", "agents.mh", "lib/b/x.mh", "flows/step10.mh", "flows/step2.mh", "lib/a.mh"]);
    expect(shape(t)).toEqual([{ "flows/": ["step2.mh", "step10.mh"] }, { "lib/": [{ "b/": ["x.mh"] }, "a.mh", "tools.mh"] }, "agents.mh", "main.mh"]);
  });

  it("shows folders with no files, merged with the ones that have some", () => {
    expect(shape(buildTree(["lib/a.mh", "x.md"], ["lib", "prompts", "prompts/empty"]))).toEqual([{ "lib/": ["a.mh"] }, { "prompts/": [{ "empty/": [] }] }, "x.md"]);
  });

  it("keeps each node's relative path", () => {
    const [lib] = buildTree(["lib/b/x.mh"]) as [Extract<TreeNode, { kind: "dir" }>];
    expect(lib.path).toBe("lib");
    const b = lib.children[0] as Extract<TreeNode, { kind: "dir" }>;
    expect(b.path).toBe("lib/b");
    expect(b.children[0].path).toBe("lib/b/x.mh");
  });

  it("lists a file's folders outermost first", () => {
    expect(ancestors("a/b/x.mh")).toEqual(["a", "a/b"]);
    expect(ancestors("x.mh")).toEqual([]);
  });
});
