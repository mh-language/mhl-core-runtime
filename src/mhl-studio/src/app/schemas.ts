// Dropping a JSON Schema on the diagram (from the toolbox, or a .json file
// from the explorer): it becomes the `schema:` of a step's agent call — see
// sync/schemas.ts for the text it writes. Mirrors app/prompts.ts.
import { addToStep, EditError, insertState, takenNames, type Doc, type EditResult } from "../sync/edits";
import { schemaExprFor } from "../sync/agentTemplates";
import { addImport, importSpec, isImported, resolveImport } from "../sync/imports";
import { agentRun, declareSchema, schemaNameFor, setSchemaArg } from "../sync/schemas";
import { agentCallStatement } from "../sync/templates";
import type { Declaration } from "../lib/types";
import { edit, importedDeclarations, toast, useFromFile } from "./controller";
import { importDecl } from "./library";
import { pickAgent, type PromptTarget } from "./prompts";
import { get, type LibraryFile } from "./store";

/** A dragged schema: a declaration (of the open file, or `file`), or a JSON file (absolute path). */
export type SchemaDrag = { schema: string; file?: string } | { json: string };

const norm = (p: string) => p.replace(/\\/g, "/");

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

  let declared = "";
  const apply = (doc: Doc): EditResult => {
    const edits: EditResult["edits"] = [];
    let s = name;
    if (declare) {
      const r = declareSchema(doc, declare.base, declare.spec);
      edits.push(...r.edits);
      s = declared = r.name;
    }
    // The form the receiving agent takes: the schema's text, or its file
    // path for Codex (--output-schema); refused when the agent ignores it.
    const decls = [...doc.external, ...doc.graph.declarations];
    const exprFor = (agentName: string): string => {
      const expr = schemaExprFor(s, decls.find((d) => d.kind === "agent" && d.name === agentName));
      if (!expr)
        throw new EditError(
          `${agentName} ignores schema: — no "\${schema}" in its args or stdin (Devin has no schema flag: put the schema in the prompt). The structured-answer agent templates take one.`,
        );
      return expr;
    };
    if ("step" in target) {
      const site = agentRun(doc.node(doc.workflow(wf), target.step).callSites ?? [], kindsOf(doc));
      const r = site
        ? setSchemaArg(doc, site, exprFor(site.receiver))
        : addToStep(doc, wf, target.step, agentCallStatement(agent!, takenNames(doc, wf, target.step), undefined, exprFor(agent!)));
      return { edits: [...edits, ...r.edits], select: target.step };
    }
    const r = insertState(doc, wf, "agent", target.placement, undefined, agent, undefined, exprFor(agent!));
    return { edits: [...edits, ...r.edits], select: r.select };
  };

  if (from) {
    const lib = from;
    return useFromFile(lib, name, (doc) => {
      const imp = addImport(doc, activePath, name, lib.path);
      const r = apply(doc);
      return { edits: [...imp.edits, ...r.edits], select: r.select };
    });
  }
  const ok = await edit(apply);
  if (ok && declare) toast("info", `Declared schema ${declared} from ${declare.spec}`);
  return ok;
}
