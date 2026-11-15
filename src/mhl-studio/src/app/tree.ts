// The workspace's files and folders (relative, "/"-separated paths from
// list_files) as a folder tree for the side bar.

export interface DirNode {
  kind: "dir";
  name: string;
  /** Relative path of the folder, "/"-separated, no trailing slash. */
  path: string;
  children: TreeNode[];
}

export interface FileNode {
  kind: "file";
  name: string;
  /** Relative path of the file. */
  path: string;
}

export type TreeNode = DirNode | FileNode;

const byName = (a: TreeNode, b: TreeNode) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: "base" });

/**
 * Builds the tree: folders before files, each group sorted by name. `dirs`
 * adds folders with no file in them (a folder just created).
 */
export function buildTree(files: string[], dirs: string[] = []): TreeNode[] {
  const root: DirNode = { kind: "dir", name: "", path: "", children: [] };
  for (const rel of [...dirs.map((d) => `${d}/`), ...files]) {
    const parts = rel.split("/").filter(Boolean);
    const isDir = rel.endsWith("/");
    let dir = root;
    (isDir ? parts : parts.slice(0, -1)).forEach((name, i) => {
      const path = parts.slice(0, i + 1).join("/");
      let next = dir.children.find((c): c is DirNode => c.kind === "dir" && c.name === name);
      if (!next) {
        next = { kind: "dir", name, path, children: [] };
        dir.children.push(next);
      }
      dir = next;
    });
    if (parts.length && !isDir) dir.children.push({ kind: "file", name: parts[parts.length - 1], path: parts.join("/") });
  }
  const sort = (nodes: TreeNode[]): TreeNode[] => {
    for (const n of nodes) if (n.kind === "dir") sort(n.children);
    return nodes.sort((a, b) => (a.kind === b.kind ? byName(a, b) : a.kind === "dir" ? -1 : 1));
  };
  return sort(root.children);
}

/** The folders that contain `rel`, outermost first: "a/b/x.mh" → ["a", "a/b"]. */
export function ancestors(rel: string): string[] {
  const parts = rel.split("/").filter(Boolean);
  return parts.slice(0, -1).map((_, i) => parts.slice(0, i + 1).join("/"));
}
