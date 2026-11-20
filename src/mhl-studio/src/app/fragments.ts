// A `partial` workflow is split across files; `mhl graph --merged` shows it
// whole. These helpers find the file to merge from: the one declaring the
// fragment with the `entry` step, which imports (transitively) the open one.
import { resolveImport } from "../sync/imports";
import type { Graph } from "../lib/types";

const slash = (p: string) => p.replace(/\\/g, "/");

/** What a file contributes to finding a merge root: its imports (absolute, "/"-separated) and the partial workflows it starts. */
export function fragmentInfo(path: string, g: Pick<Graph, "imports" | "workflows">): { imports: string[]; entries: string[] } {
  return {
    imports: g.imports.map((i) => resolveImport(slash(path), i.from)),
    entries: g.workflows.filter((w) => w.partial && (w.nodes.some((n) => n.entry) || w.routes.some((r) => r.entry))).map((w) => w.name),
  };
}

export interface FragmentFile {
  path: string;
  imports?: string[];
  entries?: string[];
}

/**
 * The file whose import closure holds every fragment of partial workflow
 * `wf` as seen from `active`: `active` itself when it declares the entry
 * fragment, else a workspace file that does and (transitively) imports
 * `active`; null when none does (a fragment no file imports yet).
 */
export function mergeRoot(active: string, wf: string, files: FragmentFile[]): string | null {
  const target = slash(active);
  const byPath = new Map(files.map((f) => [slash(f.path), f]));
  if (byPath.get(target)?.entries?.includes(wf)) return active;
  for (const f of files) {
    if (!f.entries?.includes(wf)) continue;
    const seen = new Set<string>();
    const todo = [slash(f.path)];
    while (todo.length) {
      const p = todo.pop()!;
      if (seen.has(p)) continue;
      seen.add(p);
      if (p === target) return f.path;
      todo.push(...(byPath.get(p)?.imports ?? []));
    }
  }
  return null;
}
