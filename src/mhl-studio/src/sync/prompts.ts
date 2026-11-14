// Using a prompt from the diagram: a prompt is text an agent runs, so a
// dropped prompt becomes `Agent.run(prompt: P(...))` — or, dropped on a step
// that already runs an agent, that call's `prompt:` argument. A Markdown file
// dropped from the explorer is declared first, `prompt P(a: string) from
// "./x.md"`, with one parameter per `${name}` placeholder in it.

import type { CallSite, Declaration } from "../lib/types";
import { Doc, EditError, type EditResult } from "./edits";
import { declNameFor, uniqueName } from "./templates";

// The same placeholder rule as internal/features/prompt/render.go:
// `${name}` binds a parameter, `\${name}` is literal text.
const PLACEHOLDER = /(\\)?\$\{([a-zA-Z_][a-zA-Z0-9_]*)\}/g;

/** The `---`-fenced frontmatter a file-sourced prompt may start with (stripped from its body). */
const FRONTMATTER = /^---\r?\n[\s\S]*?\r?\n---[ \t]*(\r?\n|$)/;

/** The parameters a Markdown prompt body binds: its unescaped `${name}`s, in first-use order. */
export function promptParams(md: string): string[] {
  const body = md.replace(FRONTMATTER, "");
  const out: string[] = [];
  for (const m of body.matchAll(PLACEHOLDER)) if (!m[1] && !out.includes(m[2])) out.push(m[2]);
  return out;
}

/** A prompt name from its file: `prompts/code_review.prompt.md` → `CodeReview`. */
export function promptNameFor(path: string): string {
  const base = path.split(/[\\/]/).pop()!.replace(/(\.prompt)?\.md$/i, "");
  return declNameFor(`${base}.mh`, "Prompt");
}

/** Every name already declared or imported in the open file. */
export function declaredNames(doc: Doc): string[] {
  return [
    ...doc.graph.declarations.map((d) => d.name),
    ...doc.graph.workflows.map((w) => w.name),
    ...doc.graph.imports.flatMap((i) => i.locals ?? i.names),
  ];
}

/** Declares `prompt Name(p: string, ...) from "spec"` (see insertDeclaration). */
export function declarePrompt(doc: Doc, base: string, params: string[], spec: string): EditResult & { name: string } {
  const name = uniqueName(base, declaredNames(doc));
  const line = `prompt ${name}(${params.map((p) => `${p}: string`).join(", ")}) from ${JSON.stringify(spec)}`;
  return { ...insertDeclaration(doc, "prompt", line), name };
}

/**
 * Inserts a one-line top-level declaration of `kind`: after the file's last
 * one of that kind, else before its first workflow, else at the end.
 */
export function insertDeclaration(doc: Doc, kind: Declaration["kind"], line: string): EditResult {
  const same = doc.graph.declarations.filter((d) => d.kind === kind);
  const last = same[same.length - 1];
  if (last) {
    const end = doc.at(last.span[1]);
    return { edits: [{ start: end, end, text: `\n${line}` }] };
  }
  const firstWf = [...doc.graph.workflows].sort((a, b) => a.span[0] - b.span[0])[0];
  if (firstWf) {
    const start = doc.lineStart(doc.at(firstWf.span[0]));
    return { edits: [{ start, end: start, text: `${line}\n\n` }] };
  }
  const sep = doc.src.length === 0 ? "" : doc.src.endsWith("\n\n") ? "" : doc.src.endsWith("\n") ? "\n" : "\n\n";
  return { edits: [{ start: doc.src.length, end: doc.src.length, text: `${sep}${line}\n` }] };
}

/**
 * `P(code: code, lang: "")` — named arguments, one per required parameter;
 * a parameter named like a workflow input or var is passed that value.
 */
export function promptCall(name: string, params: string[], inScope: Iterable<string> = []): string {
  const scope = new Set(inScope);
  return `${name}(${params.map((p) => `${p}: ${scope.has(p) ? p : '""'}`).join(", ")})`;
}

/** The step's first `Agent.run(...)` call that passes `prompt:`, if any. */
export function agentRunWithPrompt(sites: CallSite[], kinds: Record<string, Declaration["kind"]>): CallSite | undefined {
  return sites.find((c) => c.method === "run" && kinds[c.receiver] === "agent" && c.args?.some((a) => a.name === "prompt"));
}

/** Replaces the `prompt:` argument of `site` with `expr`. */
export function setPromptArg(doc: Doc, site: CallSite, expr: string): EditResult {
  const arg = site.args?.find((a) => a.name === "prompt");
  if (!arg) throw new EditError(`${site.receiver}.${site.method}(...) has no prompt: argument`);
  const [start, end] = doc.span(arg.span);
  return { edits: [{ start, end, text: expr }] };
}

/**
 * Inserts a multi-line top-level declaration of `kind` (an agent, ...),
 * blank-line separated: after the file's last one of that kind, else before
 * its first workflow, else at the end.
 */
export function insertBlock(doc: Doc, kind: Declaration["kind"], text: string): EditResult {
  const same = doc.graph.declarations.filter((d) => d.kind === kind);
  const last = same[same.length - 1];
  if (last) {
    const end = doc.at(last.span[1]);
    return { edits: [{ start: end, end, text: `\n\n${text}` }] };
  }
  const firstWf = [...doc.graph.workflows].sort((a, b) => a.span[0] - b.span[0])[0];
  if (firstWf) {
    const start = doc.lineStart(doc.at(firstWf.span[0]));
    return { edits: [{ start, end: start, text: `${text}\n\n` }] };
  }
  const sep = doc.src.length === 0 ? "" : doc.src.endsWith("\n\n") ? "" : doc.src.endsWith("\n") ? "\n" : "\n\n";
  return { edits: [{ start: doc.src.length, end: doc.src.length, text: `${sep}${text}\n` }] };
}
