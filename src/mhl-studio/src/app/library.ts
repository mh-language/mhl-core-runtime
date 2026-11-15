// Using another workspace file's declarations in the open one: import them,
// or drop a step that calls them (adding the import in the same undo step).
import type { EditResult } from "../sync/edits";
import { addToStep, insertState, takenNames, type Doc, type Placement } from "../sync/edits";
import { addImport, addImports, clashes, exportDeclaration, IMPORTABLE, isImported } from "../sync/imports";
import { agentCallStatement, toolCallStatement } from "../sync/templates";
import { edit, editLibraryFile, importedDeclarations, toast, useFromFile } from "./controller";
import { get, type LibraryFile } from "./store";

/**
 * After a call to `agent` was added: an agent whose args or stdin hold
 * "${schema}" fails a call without `schema:`, so say how to give it one.
 */
export function hintSchema(ok: boolean, agent: string): boolean {
  const { graph } = get();
  const decl = [...(graph?.declarations ?? []), ...importedDeclarations(graph)].find((d) => d.kind === "agent" && d.name === agent);
  if (ok && (decl?.schema === "args" || decl?.schema === "stdin"))
    toast("info", `${agent} answers in the shape of a JSON Schema: drag a .json file from the explorer onto the step to set its schema:.`);
  return ok;
}

/** Adds `import { name } from "<file>"` to the open file. */
export function importDecl(file: LibraryFile, name: string): Promise<boolean> {
  const active = get().activePath;
  if (!active) return Promise.resolve(false);
  return useFromFile(file, name, (doc) => addImport(doc, active, name, file.path));
}

function withImport(file: LibraryFile, name: string, step: (doc: Parameters<typeof insertState>[0]) => EditResult): Promise<boolean> {
  const active = get().activePath;
  if (!active) return Promise.resolve(false);
  return useFromFile(file, name, (doc) => {
    const imp = addImport(doc, active, name, file.path);
    const r = step(doc);
    return { edits: [...imp.edits, ...r.edits], select: r.select };
  });
}

/** A new step calling `tool.method` from another file, importing the tool. */
export function addToolCallStep(file: LibraryFile, tool: string, method: string, wf: string, where: Placement): Promise<boolean> {
  const m = file.declarations.find((d) => d.name === tool)?.methods?.find((x) => x.name === method);
  if (!m) return Promise.resolve(false);
  return withImport(file, tool, (doc) => insertState(doc, wf, "tool", where, { tool, method: m }));
}

/** A new step calling an agent from another file, importing it. */
export function addAgentStep(file: LibraryFile, agent: string, wf: string, where: Placement): Promise<boolean> {
  return withImport(file, agent, (doc) => insertState(doc, wf, "agent", where, undefined, agent)).then((ok) => hintSchema(ok, agent));
}

/** The edit that adds a `var x = Tool.method(…)` line to step `id`. */
function toolCallInto(doc: Doc, wf: string, id: string, tool: string, method: string, file?: LibraryFile): EditResult {
  const decls = file ? file.declarations : doc.graph.declarations;
  const m = decls.find((d) => d.name === tool)?.methods?.find((x) => x.name === method);
  if (!m) return { edits: [] };
  return addToStep(doc, wf, id, toolCallStatement(tool, m, takenNames(doc, wf, id)));
}

/**
 * Drops a tool method onto step `id`: the step gets a line calling it (and
 * the open file an import, when the tool lives in `file`).
 */
export function addToolCallToStep(file: LibraryFile | undefined, tool: string, method: string, wf: string, id: string): Promise<boolean> {
  if (!file) return edit((doc) => toolCallInto(doc, wf, id, tool, method));
  return withImport(file, tool, (doc) => toolCallInto(doc, wf, id, tool, method, file));
}

/** Drops an agent onto step `id`: the step gets a `var reply = Agent.run(…)` line. */
export function addAgentCallToStep(file: LibraryFile | undefined, agent: string, wf: string, id: string): Promise<boolean> {
  const into = (doc: Doc) => addToStep(doc, wf, id, agentCallStatement(agent, takenNames(doc, wf, id)));
  return (file ? withImport(file, agent, into) : edit(into)).then((ok) => hintSchema(ok, agent));
}

/**
 * Imports another workspace file into the open one — dropped on the diagram's
 * Imports card: every importable declaration it has, marking `export` in that
 * file the ones that lack it (one save there), then one `import { ... }` here
 * (one undo step). Names the open file already uses are left out, and said so.
 */
export async function importFile(file: LibraryFile): Promise<boolean> {
  const { graph, activePath } = get();
  if (!graph || !activePath) return false;
  if (file.path === activePath) return false;
  if (!file.ok) {
    toast("error", `${file.rel} has errors — fix them before importing it.`);
    return false;
  }
  const decls = file.declarations.filter((d) => IMPORTABLE.has(d.kind));
  const fresh = decls.filter((d) => !isImported(graph, activePath, d.name, file.path));
  if (decls.length === 0) {
    toast("info", `${file.rel} declares nothing to import.`);
    return false;
  }
  if (fresh.length === 0) {
    toast("info", `Everything ${file.rel} declares is already imported.`);
    return true;
  }
  const taken = fresh.filter((d) => clashes(graph, d.name)).map((d) => d.name);
  const names = fresh.map((d) => d.name).filter((n) => !taken.includes(n));
  if (names.length === 0) {
    toast("error", `${taken.join(", ")} from ${file.rel} would clash with names this file already has.`);
    return false;
  }
  const unexported = fresh.filter((d) => !d.exported && names.includes(d.name)).map((d) => d.name);
  if (unexported.length) {
    const ok = await editLibraryFile(file, `${unexported.join(", ")} must be marked export there`, (doc) => ({
      edits: unexported.flatMap((n) => exportDeclaration(doc, n).edits),
    }));
    if (!ok) return false;
  }
  const ok = await edit((doc) => addImports(doc, activePath, names, file.path));
  if (ok) {
    const parts = [`Imported ${names.join(", ")} from ${file.rel}`];
    if (unexported.length) parts.push(`marked export there: ${unexported.join(", ")}`);
    if (taken.length) parts.push(`left out (name already used here): ${taken.join(", ")}`);
    toast(taken.length ? "error" : "info", parts.join(" — "));
  }
  return ok;
}
