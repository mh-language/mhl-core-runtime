import { describe, expect, it } from "vitest";
import { ancestors, buildTree, closedByDefault, pathToCopy, type TreeNode } from "./tree";

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

describe("closedByDefault", () => {
  it("folds hidden folders, __pycache__ and folders listed without contents", () => {
    expect(closedByDefault(".mhl", [])).toBe(true);
    expect(closedByDefault("sub/.mhl-cache", [])).toBe(true);
    expect(closedByDefault("__pycache__", [])).toBe(true);
    expect(closedByDefault("node_modules", ["node_modules"])).toBe(true);
    expect(closedByDefault("story", [])).toBe(false);
    expect(closedByDefault("assets", [])).toBe(false);
  });
});

describe("pathToCopy", () => {
  it("is the absolute path, or the workspace-relative one", () => {
    expect(pathToCopy("/w/proj", "story/impl.mh", false)).toBe("/w/proj/story/impl.mh");
    expect(pathToCopy("/w/proj", "story/impl.mh", true)).toBe("story/impl.mh");
  });

  it("is the root itself for the tree's empty space", () => {
    expect(pathToCopy("/w/proj", "", false)).toBe("/w/proj");
    expect(pathToCopy("/w/proj", "", true)).toBe(".");
  });

  it("uses the root's separator on Windows", () => {
    expect(pathToCopy("C:\\w\\proj", "story/impl.mh", false)).toBe("C:\\w\\proj\\story\\impl.mh");
  });
});
