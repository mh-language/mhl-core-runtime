// Keeping references valid when files move: a moved or renamed `.mh`/`.md`
// file — or the folder holding it — would break the `import ... from "x.mh"`
// and `prompt X(...) from "x.md"` / `schema X from "x.json"` paths that point
// at it, and a moved `.mh`
// file's own relative paths. relinkEdits rewrites exactly those string
// literals, leaving every path that still resolves untouched.

import { Doc, type TextEdit } from "./edits";
import { importSpec, resolveImport } from "./imports";

/** Where a path ("/"-separated, absolute) ends up after the move; unaffected paths map to themselves. */
export type Mover = (path: string) => string;

const norm = (p: string) => p.replace(/\\/g, "/").replace(/\/+$/, "");

/** The Mover for moving `from` (a file or a folder, absolute) to `to`. */
export function moverFor(from: string, to: string): Mover {
  const f = norm(from);
  const t = norm(to);
  return (p) => {
    const n = norm(p);
    if (n === f) return t;
    return n.startsWith(`${f}/`) ? t + n.slice(f.length) : n;
  };
}

const STRING = /"(?:[^"\\\n]|\\.)*"/g;

/** The edit replacing the string literal `which` picks in src[start, end), if the path changed. */
function relinkLiteral(doc: Doc, s: number, e: number, pick: (text: string) => RegExpExecArray | null, file: string, move: Mover): TextEdit | null {
  const text = doc.src.slice(s, e);
  const m = pick(text);
  if (!m) return null;
  let spec: string;
  try {
    spec = JSON.parse(m[m.length - 1]) as string;
  } catch {
    return null;
  }
  const target = resolveImport(file, spec);
  const next = move(target);
  const nextFile = move(file);
  // Still pointing at the same file from the new location: keep it as written.
  if (resolveImport(nextFile, spec) === next) return null;
  const start = s + m.index + m[0].length - m[m.length - 1].length;
  return { start, end: start + m[m.length - 1].length, text: JSON.stringify(importSpec(nextFile, next)) };
}

/**
 * The edits that keep file `path`'s imports and `prompt`/`schema ... from` paths
 * pointing at the same files once `move` is applied — to the files they
 * point at, to `path` itself, or both. `path` is the file's current location.
 */
export function relinkEdits(doc: Doc, path: string, move: Mover): TextEdit[] {
  const file = norm(path);
  const out: TextEdit[] = [];
  for (const imp of doc.graph.imports) {
    const [s, e] = doc.span(imp.span);
    // The import's path is its last string literal.
    const ed = relinkLiteral(doc, s, e, (t) => {
      let last: RegExpExecArray | null = null;
      for (const m of t.matchAll(STRING)) last = Object.assign([m[0]], { index: m.index!, input: t }) as RegExpExecArray;
      return last;
    }, file, move);
    if (ed) out.push(ed);
  }
  for (const d of doc.graph.declarations) {
    if ((d.kind !== "prompt" && d.kind !== "schema") || !d.detail) continue;
    const [s, e] = doc.span(d.span);
    const ed = relinkLiteral(doc, s, e, (t) => /\bfrom\s*("(?:[^"\\\n]|\\.)*")/.exec(t), file, move);
    if (ed) out.push(ed);
  }
  return out;
}
