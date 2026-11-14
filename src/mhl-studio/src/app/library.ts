// Using another workspace file's declarations in the open one: import them,
// or drop a step that calls them (adding the import in the same undo step).
import type { EditResult } from "../sync/edits";
import { addToStep, insertState, takenNames, type Doc, type Placement } from "../sync/edits";
import { addImport } from "../sync/imports";
import { agentCallStatement, toolCallStatement } from "../sync/templates";
import { edit, useFromFile } from "./controller";
import { get, type LibraryFile } from "./store";

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
  return withImport(file, agent, (doc) => insertState(doc, wf, "agent", where, undefined, agent));
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
  return file ? withImport(file, agent, into) : edit(into);
}
