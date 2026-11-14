// Importing a declaration of another workspace file into the open one:
// `import { Name } from "./rel/path.mh"` (merged into an existing import of
// the same file when there is one), and `export` on the declaration in its
// own file when it is not exported yet — a named import requires it.

import type { Declaration, Graph } from "../lib/types";
import { Doc, EditError, type EditResult, type TextEdit } from "./edits";

/** Declaration kinds another file can import and use. */
export const IMPORTABLE = new Set<Declaration["kind"]>(["tool", "agent", "prompt", "extension", "memory", "router", "type", "enum", "alias", "schema", "skill"]);

function norm(p: string): string {
  return p.replace(/\\/g, "/");
}

function dirname(p: string): string {
  const n = norm(p);
  return n.slice(0, n.lastIndexOf("/")) || "/";
}

/** Resolves an import spec written in `fromFile` to an absolute, normalized path. */
export function resolveImport(fromFile: string, spec: string): string {
  const out = dirname(fromFile).split("/");
  for (const seg of norm(spec).split("/")) {
    if (seg === "" || seg === ".") continue;
    if (seg === "..") out.pop();
    else out.push(seg);
  }
  return out.join("/");
}

/** The import spec that names `toFile` from `fromFile`: "./x.mh", "../lib/x.mh". */
export function importSpec(fromFile: string, toFile: string): string {
  const a = dirname(fromFile).split("/").filter(Boolean);
  const b = norm(toFile).split("/").filter(Boolean);
  let i = 0;
  while (i < a.length && i < b.length - 1 && a[i] === b[i]) i++;
  const up = a.length - i;
  const rest = b.slice(i).join("/");
  return up === 0 ? `./${rest}` : `${"../".repeat(up)}${rest}`;
}

/** The import of `targetFile` in graph, if any. */
function importOf(graph: Graph, activePath: string, targetFile: string) {
  const target = norm(targetFile);
  return graph.imports.find((i) => resolveImport(activePath, i.from) === target);
}

/** Whether `name` from `targetFile` is already visible in the open file. */
export function isImported(graph: Graph, activePath: string, name: string, targetFile: string): boolean {
  const imp = importOf(graph, activePath, targetFile);
  return !!imp && (imp.names.length === 0 || imp.names.includes(name));
}

/** Adds `name` from `targetFile` to the open file's imports. */
export function addImport(doc: Doc, activePath: string, name: string, targetFile: string): EditResult {
  if (norm(activePath) === norm(targetFile)) return { edits: [] };
  const existing = importOf(doc.graph, activePath, targetFile);
  if (existing && (existing.names.length === 0 || existing.names.includes(name))) return { edits: [] };
  const clash =
    doc.graph.declarations.some((d) => d.name === name) ||
    doc.graph.workflows.some((w) => w.name === name) ||
    doc.graph.imports.some((i) => i.names.includes(name));
  if (clash) throw new EditError(`${name} is already declared or imported in this file`);

  if (existing) {
    // import { A, B } from "x" → import { A, B, name } from "x"
    const [s, e] = doc.span(existing.span);
    let at = s + doc.src.slice(s, e).lastIndexOf("}");
    while (at > s && /[ \t\n]/.test(doc.src[at - 1])) at--;
    return { edits: [{ start: at, end: at, text: `, ${name}` }] };
  }

  const line = `import { ${name} } from ${JSON.stringify(importSpec(activePath, targetFile))}`;
  const last = [...doc.graph.imports].sort((a, b) => a.span[1] - b.span[1]).pop();
  if (last) {
    const end = doc.at(last.span[1]);
    return { edits: [{ start: end, end, text: `\n${line}` }] };
  }
  return { edits: [topInsert(doc, line)] };
}

/** Inserts a first import after the file's leading comment block, if any. */
function topInsert(doc: Doc, line: string): TextEdit {
  if (doc.src.trim() === "") return { start: 0, end: doc.src.length, text: `${line}\n` };
  const lines = doc.src.split("\n");
  let i = 0;
  while (i < lines.length && lines[i].trim().startsWith("//")) i++;
  const at = lines.slice(0, i).reduce((n, l) => n + l.length + 1, 0);
  if (i === 0) return { start: 0, end: 0, text: `${line}\n\n` };
  const blank = (lines[i] ?? "").trim() === "";
  return { start: at, end: at, text: blank ? `\n${line}\n` : `\n${line}\n\n` };
}

/** Marks a declaration `export` in its own file. */
export function exportDeclaration(doc: Doc, name: string): EditResult {
  const d = doc.graph.declarations.find((x) => x.name === name);
  if (!d) throw new EditError(`${name} not found`);
  if (d.exported) return { edits: [] };
  const at = doc.at(d.span[0]);
  return { edits: [{ start: at, end: at, text: "export " }] };
}

/** Where a name the open file uses comes from. */
export interface ImportOrigin {
  /** The import spec as written: "./tools.mh". */
  from: string;
  /** The imported file's path (the library's spelling when it is known). */
  path: string;
  /** The declaration's name in its own file (differs from the local one under `as`). */
  name: string;
}

/**
 * Every imported name the open file can use, by the name it is used under:
 * the `as` alias when there is one. A whole-file import (`import "x.mh"`)
 * brings in every declaration the library knows for that file.
 */
export function importOrigins(
  graph: Pick<Graph, "imports"> | null,
  activePath: string | null,
  library: { path: string; declarations: Pick<Declaration, "name">[] }[],
): Map<string, ImportOrigin> {
  const out = new Map<string, ImportOrigin>();
  if (!graph || !activePath) return out;
  for (const imp of graph.imports) {
    const target = resolveImport(activePath, imp.from);
    const file = library.find((l) => norm(l.path) === target);
    const path = file?.path ?? target;
    if (imp.names.length === 0) {
      for (const d of file?.declarations ?? []) if (!out.has(d.name)) out.set(d.name, { from: imp.from, path, name: d.name });
      continue;
    }
    // A named import beats a whole-file one for the same local name.
    imp.names.forEach((name, i) => out.set(imp.locals?.[i] ?? name, { from: imp.from, path, name }));
  }
  return out;
}
