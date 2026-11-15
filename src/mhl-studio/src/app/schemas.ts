// Dropping a JSON Schema on the diagram (a .json file from the explorer): it
// becomes the `schema:` of a step's agent call — see sync/schemas.ts for the
// text it writes. Mirrors app/prompts.ts.
import { addToStep, insertState, takenNames, type Doc, type EditResult } from "../sync/edits";
import { schemaExprFor } from "../sync/agentTemplates";
import { addImport, importSpec, isImported, resolveImport } from "../sync/imports";
import { agentRun, declareSchema, promptWithSchema, schemaNameFor, schemaOnStdin, setSchemaArg, setSchemaInPrompt } from "../sync/schemas";
import { agentCallStatement } from "../sync/templates";
import type { Declaration } from "../lib/types";
import { edit, editLibraryFile, importedDeclarations, toast, useFromFile } from "./controller";
import { importDecl } from "./library";
import { pickAgent, type PromptTarget } from "./prompts";
import { get, type LibraryFile } from "./store";

/** A dragged schema: a declaration (of the open file, or `file`), or a JSON file (absolute path). */
export type SchemaDrag = { schema: string; file?: string } | { json: string };

const TODO = '"TODO: describe the task"';

const norm = (p: string) => p.replace(/\\/g, "/");

const folded = (agent: string) =>
  `${agent} has no place for schema: (no "\${schema}" in its args, no stdin to add it to), so the schema went into this call's prompt. ` +
  `For a native schema flag, use a structured-answer agent (+ → Agent).`;

/**
 * When `agent` is imported and ignores schemas, teaches it to read one on
 * stdin in its own file first (sync/schemas.ts schemaOnStdin). False when
 * refused; else the file it was taught in, if any, or `fold` when it has no
 * stdin to teach — the schema then goes into the call's prompt.
 */
async function teachImported(agent: string): Promise<false | { file?: string; fold?: boolean }> {
  const { graph, library } = get();
  if (!graph || graph.declarations.some((d) => d.kind === "agent" && d.name === agent)) return {};
  const decl = importedDeclarations(graph).find((d) => d.kind === "agent" && d.name === agent);
  if (decl?.schema !== "none") return {};
  const file = library.find((l) => l.declarations.includes(decl));
  if (!file) return {};
  let fold = false;
  const ok = await editLibraryFile(file, `${agent} has to take the schema there`, (doc) => {
    const r = schemaOnStdin(doc, agent);
    fold = !r;
    return r;
  });
  if (fold) return { fold };
  return ok && { file: file.rel };
}

/**
 * The toast after teaching `agent` to read a schema: its stdin now names
 * "${schema}", which the runtime requires every call to fill — so it counts
 * the open file's calls still passing none.
 */
function taughtNote(agent: string, where: string): { kind: "info" | "error"; text: string } {
  const missing = (get().graph?.workflows ?? [])
    .flatMap((w) => w.nodes.flatMap((n) => n.callSites ?? []))
    .filter((c) => c.receiver === agent && c.method === "run" && !c.args?.some((a) => a.name === "schema")).length;
  const text = `${agent}${where} now reads the schema on stdin, after the prompt — every ${agent}.run(...) must pass schema: now`;
  return missing ? { kind: "error", text: `${text}; ${missing} call${missing > 1 ? "s" : ""} in this file still pass${missing > 1 ? "" : "es"} none.` } : { kind: "info", text: `${text}.` };
}

function kindsOf(doc: Doc): Record<string, Declaration["kind"]> {
  const out: Record<string, Declaration["kind"]> = {};
  for (const d of [...doc.external, ...doc.graph.declarations]) out[d.name] = d.kind;
  return out;
}

export async function dropSchema(src: SchemaDrag, wf: string, target: PromptTarget): Promise<boolean> {
  const { graph, activePath, library } = get();
  if (!graph || !activePath) return false;

  // The schema: a declared name, or a JSON file to declare.
  let name = "";
  let declare: { base: string; spec: string } | null = null;
  let from: LibraryFile | undefined;
  if ("json" in src) {
    const file = norm(src.json);
    const known = graph.declarations.find((d) => d.kind === "schema" && d.detail && resolveImport(activePath, d.detail) === file);
    if (known) name = known.name;
    else declare = { base: schemaNameFor(src.json), spec: importSpec(activePath, src.json) };
  } else {
    const lib = src.file ? library.find((l) => l.path === src.file) : undefined;
    const decls = lib ? lib.declarations : [...graph.declarations, ...importedDeclarations(graph)];
    const d = decls.find((x) => x.kind === "schema" && x.name === src.schema);
    if (!d) return false;
    name = d.name;
    if (lib && !isImported(graph, activePath, d.name, lib.path)) from = lib;
  }

  // The agent, unless the drop lands on a step that already runs one.
  const step = "step" in target ? graph.workflows.find((w) => w.name === wf)?.nodes.find((n) => n.id === target.step) : undefined;
  const kinds: Record<string, Declaration["kind"]> = {};
  for (const d of [...importedDeclarations(graph), ...graph.declarations]) kinds[d.name] = d.kind;
  let agent: string | undefined;
  if (!step || !agentRun(step.callSites ?? [], kinds)) {
    const a = pickAgent();
    if (!a) {
      toast("error", "A schema shapes an agent's answer, and this workspace declares no agent yet. Create one with + → Agent in the explorer.");
      return false;
    }
    if (a.file && !(await importDecl(a.file, a.name))) return false;
    agent = a.name;
  }
  const receiver = agent ?? agentRun(step!.callSites ?? [], kinds)!.receiver;
  const imported = await teachImported(receiver);
  if (!imported) return false;

  let declared = "";
  let taught = "";
  let fold = !!imported.fold;
  const apply = (doc: Doc): EditResult => {
    const edits: EditResult["edits"] = [];
    let s = name;
    if (declare) {
      const r = declareSchema(doc, declare.base, declare.spec);
      edits.push(...r.edits);
      s = declared = r.name;
    }
    // The form the receiving agent takes: the schema's text, or its file
    // path for Codex (--output-schema). An agent declared here that ignores
    // schemas is taught to read one on stdin (an imported one already was);
    // with no stdin to teach, the schema is folded into the call's prompt.
    const decls = [...doc.external, ...doc.graph.declarations];
    const exprFor = (agentName: string): string | null => {
      if (fold) return null;
      const expr = schemaExprFor(s, decls.find((d) => d.kind === "agent" && d.name === agentName));
      if (expr) return expr;
      const r = schemaOnStdin(doc, agentName);
      if (!r) {
        fold = true;
        return null;
      }
      edits.push(...r.edits);
      taught = agentName;
      return `${s}.content`;
    };
    const content = `${s}.content`;
    if ("step" in target) {
      const site = agentRun(doc.node(doc.workflow(wf), target.step).callSites ?? [], kindsOf(doc));
      let r: EditResult;
      if (site) {
        const expr = exprFor(site.receiver);
        r = expr ? setSchemaArg(doc, site, expr) : setSchemaInPrompt(doc, site, content);
      } else {
        const expr = exprFor(agent!);
        const taken = takenNames(doc, wf, target.step);
        r = addToStep(doc, wf, target.step, expr ? agentCallStatement(agent!, taken, undefined, expr) : agentCallStatement(agent!, taken, promptWithSchema(TODO, content)));
      }
      return { edits: [...edits, ...r.edits], select: target.step };
    }
    const expr = exprFor(agent!);
    const r = expr
      ? insertState(doc, wf, "agent", target.placement, undefined, agent, undefined, expr)
      : insertState(doc, wf, "agent", target.placement, undefined, agent, promptWithSchema(TODO, content));
    return { edits: [...edits, ...r.edits], select: r.select };
  };

  const lib = from;
  const ok = lib
    ? await useFromFile(lib, name, (doc) => {
        const imp = addImport(doc, activePath, name, lib.path);
        const r = apply(doc);
        return { edits: [...imp.edits, ...r.edits], select: r.select };
      })
    : await edit(apply);
  const note = taught ? taughtNote(taught, "") : imported.file ? taughtNote(receiver, ` (${imported.file})`) : null;
  if (ok && fold) toast("info", folded(receiver));
  else if (ok && note) toast(note.kind, note.text);
  else if (ok && declare) toast("info", `Declared schema ${declared} from ${declare.spec}`);
  return ok;
}
