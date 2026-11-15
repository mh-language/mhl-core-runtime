// Using a JSON Schema from the diagram: `Agent.run` takes the schema's text
// as `schema:`, so a dropped schema becomes that argument of a step's agent
// call — `Ask.run(prompt: ..., schema: Answer.content)`. A JSON file dropped
// from the explorer is declared first: `schema Answer from "./answer.json"`.

import type { CallSite, Declaration } from "../lib/types";
import { Doc, EditError, type EditResult } from "./edits";
import { declaredNames, insertDeclaration } from "./prompts";
import { declNameFor, uniqueName } from "./templates";

/** A schema name from its file: `schemas/code_review.schema.json` → `CodeReview`. */
export function schemaNameFor(path: string): string {
  const base = path.split(/[\\/]/).pop()!.replace(/(\.schema)?\.json$/i, "");
  return declNameFor(`${base}.mh`, "Schema");
}

/** Declares `schema Name from "spec"` (after the last schema, else before the first workflow). */
export function declareSchema(doc: Doc, base: string, spec: string): EditResult & { name: string } {
  const name = uniqueName(base, declaredNames(doc));
  return { ...insertDeclaration(doc, "schema", `schema ${name} from ${JSON.stringify(spec)}`), name };
}

/** The `schema:` value for a declared schema: its JSON text. */
export function schemaArg(name: string): string {
  return `${name}.content`;
}

/** The step's first `Agent.run(...)` call, if any. */
export function agentRun(sites: CallSite[], kinds: Record<string, Declaration["kind"]>): CallSite | undefined {
  return sites.find((c) => c.method === "run" && kinds[c.receiver] === "agent");
}

/** Sets the `schema:` argument of agent call `site` to `expr`: replaced when present, else appended. */
export function setSchemaArg(doc: Doc, site: CallSite, expr: string): EditResult {
  const arg = site.args?.find((a) => a.name === "schema");
  if (arg) {
    const [start, end] = doc.span(arg.span);
    return { edits: [{ start, end, text: expr }] };
  }
  const [, end] = doc.span(site.span);
  const close = end - 1; // the call's ")"
  let i = close - 1;
  while (i >= 0 && /\s/.test(doc.src[i])) i--;
  // Right after the last argument, so a line break before ")" stays put.
  const sep = doc.src[i] === "(" ? "" : ", ";
  return { edits: [{ start: i + 1, end: i + 1, text: `${sep}schema: ${expr}` }] };
}

/** What an agent's stdin says before a schema it was taught to read (see schemaOnStdin). */
export const SCHEMA_NOTE = "Answer only with JSON that validates against this JSON Schema:";

/**
 * Teaches agent `name`, declared in `doc`, to take a call's `schema:` — for an
 * agent whose args/stdin have no "${schema}", which the runtime would silently
 * drop. Appends SCHEMA_NOTE and "${schema}" to the end of its literal `stdin:`,
 * after the prompt: any CLI that reads stdin gets the schema with no flag of its
 * own. Null when the agent has no literal string stdin to extend.
 */
export function schemaOnStdin(doc: Doc, name: string): EditResult | null {
  const decl = doc.graph.declarations.find((d) => d.kind === "agent" && d.name === name);
  if (!decl) return null;
  const [from, to] = doc.span(decl.span);
  const body = doc.src.slice(from, to);
  const m = /(^|\n)[ \t]*stdin[ \t]*:[ \t]*(?=")/.exec(body);
  if (!m) return null;
  const open = from + m.index + m[0].length;
  if (doc.src.startsWith('"""', open)) {
    const close = doc.src.indexOf('"""', open + 3);
    if (close < 0 || close > to) return null;
    return { edits: [{ start: close, end: close, text: `\n\n${SCHEMA_NOTE}\n\${schema}` }] };
  }
  const close = closingQuote(doc.src, open, to);
  if (close < 0) return null;
  return { edits: [{ start: close, end: close, text: `\\n\\n${SCHEMA_NOTE}\\n\${schema}` }] };
}

/** The index of the `"` closing the string opened at `open`, skipping escapes and `${...}`; -1 if none before `limit`. */
function closingQuote(src: string, open: number, limit: number): number {
  let depth = 0;
  for (let i = open + 1; i < limit; i++) {
    const c = src[i];
    if (c === "\\") i++;
    else if (c === "$" && src[i + 1] === "{") {
      depth++;
      i++;
    }
    else if (c === "}" && depth > 0) depth--;
    else if (c === '"' && depth === 0) return i;
  }
  return -1;
}

const FOLD = ` + "\\n\\n${SCHEMA_NOTE}\\n" + `;
const FOLDED = new RegExp(`\\s*\\+\\s*"\\\\n\\\\n${SCHEMA_NOTE.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\\\n"\\s*\\+\\s*[\\w.]+$`);

/**
 * A prompt expression with the schema folded in after it — for an agent with
 * no slot for `schema:` (no "${schema}", prompt in argv): `p + "\n\n<note>\n" + Answer.content`.
 * A schema folded in before is replaced, not stacked.
 */
export function promptWithSchema(prompt: string, schemaExpr: string): string {
  const base = prompt.trim().replace(FOLDED, "");
  // `+` binds tighter than comparisons and ?? / || / &&: keep anything but a
  // plain literal, name, call or concatenation whole.
  const bare = base.replace(/"(?:[^"\\]|\\.)*"/g, '""');
  const p = /\?\?|\|\||&&|[=!<>]=|[<>]|->/.test(bare) ? `(${base})` : base;
  return `${p}${FOLD}${schemaExpr}`;
}

/** Folds the schema into the `prompt:` argument of agent call `site` (see promptWithSchema). */
export function setSchemaInPrompt(doc: Doc, site: CallSite, schemaExpr: string): EditResult {
  const arg = site.args?.find((a) => a.name === "prompt");
  if (!arg) throw new EditError(`${site.receiver}.run(...) has no prompt: to put the schema in`);
  const [start, end] = doc.span(arg.span);
  return { edits: [{ start, end, text: promptWithSchema(doc.src.slice(start, end), schemaExpr) }] };
}
