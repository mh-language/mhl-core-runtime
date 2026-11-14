// Dropping a prompt on the diagram (from the toolbox, or a Markdown file
// from the explorer): picks the agent that runs it, imports what lives in
// another file, and applies the whole change as one edit — see
// sync/prompts.ts for the text it writes.
import { addToStep, insertState, takenNames, type Doc, type EditResult, type Placement } from "../sync/edits";
import { addImport, importSpec, isImported, resolveImport } from "../sync/imports";
import { agentRunWithPrompt, declarePrompt, promptCall, promptNameFor, promptParams, setPromptArg } from "../sync/prompts";
import { agentCallStatement } from "../sync/templates";
import type { Declaration } from "../lib/types";
import { edit, importedDeclarations, readText, toast, useFromFile } from "./controller";
import { importDecl } from "./library";
import { get, type LibraryFile } from "./store";

/** A dragged prompt: a declaration (of the open file, or `file`), or a Markdown file (absolute path). */
export type PromptDrag = { prompt: string; file?: string } | { md: string };

/** Where it was dropped: onto a step, or onto the diagram at a placement. */
export type PromptTarget = { step: string } | { placement: Placement };

const norm = (p: string) => p.replace(/\\/g, "/");

function kindsOf(doc: Doc): Record<string, Declaration["kind"]> {
  const out: Record<string, Declaration["kind"]> = {};
  for (const d of [...doc.external, ...doc.graph.declarations]) out[d.name] = d.kind;
  return out;
}

/** The agent a new call runs: this file's or an imported one, else one from another file (to import). */
export function pickAgent(): { name: string; file?: LibraryFile } | null {
  const { graph, activePath, library } = get();
  const here = [...(graph?.declarations ?? []), ...importedDeclarations(graph)].find((d) => d.kind === "agent");
  if (here) return { name: here.name };
  for (const f of library) {
    if (f.path === activePath) continue;
    const a = f.declarations.find((d) => d.kind === "agent");
    if (a) return { name: a.name, file: f };
  }
  return null;
}

export async function dropPrompt(src: PromptDrag, wf: string, target: PromptTarget): Promise<boolean> {
  const { graph, activePath, library } = get();
  if (!graph || !activePath) return false;

  // The prompt: a name and its required params — or a Markdown file to declare.
  let name = "";
  let params: string[] = [];
  let declare: { base: string; params: string[]; spec: string } | null = null;
  let from: LibraryFile | undefined;
  if ("md" in src) {
    const file = norm(src.md);
    const known = graph.declarations.find((d) => d.kind === "prompt" && d.detail && resolveImport(activePath, d.detail) === file);
    if (known) {
      name = known.name;
      params = known.params ?? [];
    } else {
      let text: string;
      try {
        text = await readText(src.md);
      } catch (e) {
        toast("error", `Could not read ${src.md}: ${String(e)}`);
        return false;
      }
      declare = { base: promptNameFor(src.md), params: promptParams(text), spec: importSpec(activePath, src.md) };
    }
  } else {
    const lib = src.file ? library.find((l) => l.path === src.file) : undefined;
    const decls = lib ? lib.declarations : [...graph.declarations, ...importedDeclarations(graph)];
    const d = decls.find((x) => x.kind === "prompt" && x.name === src.prompt);
    if (!d) return false;
    name = d.name;
    params = d.params ?? [];
    if (lib && !isImported(graph, activePath, d.name, lib.path)) from = lib;
  }

  // The agent, unless the drop lands on a step that already runs one.
  const step = "step" in target ? graph.workflows.find((w) => w.name === wf)?.nodes.find((n) => n.id === target.step) : undefined;
  const kinds: Record<string, Declaration["kind"]> = {};
  for (const d of [...importedDeclarations(graph), ...graph.declarations]) kinds[d.name] = d.kind;
  let agent: string | undefined;
  if (!step || !agentRunWithPrompt(step.callSites ?? [], kinds)) {
    const a = pickAgent();
    if (!a) {
      toast("error", "A prompt runs through an agent, and this workspace declares none yet. Create one with + → Agent in the explorer.");
      return false;
    }
    // An agent from another file is imported first (its own undo step).
    if (a.file && !(await importDecl(a.file, a.name))) return false;
    agent = a.name;
  }

  let declared = "";
  const apply = (doc: Doc): EditResult => {
    const edits: EditResult["edits"] = [];
    let p = name;
    let ps = params;
    if (declare) {
      const r = declarePrompt(doc, declare.base, declare.params, declare.spec);
      edits.push(...r.edits);
      p = declared = r.name;
      ps = declare.params;
    }
    const w = doc.workflow(wf);
    if ("step" in target) {
      const expr = promptCall(p, ps, takenNames(doc, wf, target.step));
      const site = agentRunWithPrompt(doc.node(w, target.step).callSites ?? [], kindsOf(doc));
      const r = site ? setPromptArg(doc, site, expr) : addToStep(doc, wf, target.step, agentCallStatement(agent!, takenNames(doc, wf, target.step), expr));
      return { edits: [...edits, ...r.edits], select: target.step };
    }
    const expr = promptCall(p, ps, [...w.inputs.map((i) => i.name), ...w.vars.map((v) => v.name)]);
    const r = insertState(doc, wf, "agent", target.placement, undefined, agent, expr);
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
  if (ok && declare) toast("info", `Declared prompt ${declared} from ${declare.spec}`);
  return ok;
}
