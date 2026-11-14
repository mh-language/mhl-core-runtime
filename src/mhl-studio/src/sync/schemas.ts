// Using a JSON Schema from the diagram: `Agent.run` takes the schema's text
// as `schema:`, so a dropped schema becomes that argument of a step's agent
// call — `Ask.run(prompt: ..., schema: Answer.content)`. A JSON file dropped
// from the explorer is declared first: `schema Answer from "./answer.json"`.

import type { CallSite, Declaration } from "../lib/types";
import { Doc, type EditResult } from "./edits";
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
