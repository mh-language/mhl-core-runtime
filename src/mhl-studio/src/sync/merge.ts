// A parallel group's `merge (branches) { ... }` block: the explicit join that
// combines what its branches wrote. Without one, two branches assigning the
// same workflow var different values fails the run; with one, the merge has
// to assign that var itself, reading each branch's value as
// `branches.<Step>.<var>`. Adding one writes those assignments for the vars
// it can see more than one branch assigning.
import type { GraphNode, Workflow } from "../lib/types";
import { EditError, type Doc, type EditResult } from "./edits";

const IND = "    ";

/** The workflow vars (by name) the branch's body assigns: `x = ...` or `self.x = ...`. */
function assigned(body: string, vars: string[]): Set<string> {
  const out = new Set<string>();
  for (const m of body.matchAll(/(?:^|[^\w.])(?:self\.)?([A-Za-z_]\w*)\s*=(?!=)/g)) {
    if (vars.includes(m[1])) out.add(m[1]);
  }
  return out;
}

/** Whether a workflow var starts as an array (`var x = [...]`). */
function isArray(doc: Doc, wf: Workflow, name: string): boolean {
  const v = wf.vars.find((x) => x.name === name);
  return !!v && /=\s*\[/.test(doc.text(v.span));
}

/**
 * The vars two or more of the group's branches assign — the ones that, as
 * things stand, make the run fail at the barrier — each with those branches.
 */
export function sharedWrites(doc: Doc, wf: Workflow, group: GraphNode): { name: string; by: string[] }[] {
  const names = wf.vars.filter((v) => v.kind === "var").map((v) => v.name);
  const by = new Map<string, string[]>();
  for (const id of group.children ?? []) {
    const n = doc.node(wf, id);
    const body = doc.src.slice(doc.at(n.bodyOpen) + 1, doc.at(n.bodyClose));
    for (const name of assigned(body, names)) by.set(name, [...(by.get(name) ?? []), id]);
  }
  return [...by].filter(([, steps]) => steps.length > 1).map(([name, steps]) => ({ name, by: steps }));
}

/** The body lines of a new merge for `group`. */
export function mergeBody(doc: Doc, wf: Workflow, group: GraphNode, param = "branches"): string[] {
  const shared = sharedWrites(doc, wf, group);
  if (!shared.length) {
    const eg = (group.children ?? []).slice(0, 2).map((c) => `${param}.${c}.<var>`).join(", ");
    return [`// each branch's vars: ${eg}`];
  }
  return shared.map(({ name, by }) =>
    isArray(doc, wf, name)
      ? `self.${name} = [${by.map((s) => `...${param}.${s}.${name}`).join(", ")}]`
      : `self.${name} = ${param}.${by[0]}.${name}  // TODO: combine with ${by.slice(1).map((s) => `${param}.${s}.${name}`).join(", ")}`,
  );
}

function groupOf(doc: Doc, wfName: string, id: string): { wf: Workflow; g: GraphNode } {
  const wf = doc.workflow(wfName);
  const g = doc.node(wf, id);
  if (g.kind !== "parallel") throw new EditError(`${id} is not a parallel group`);
  return { wf, g };
}

/** Adds a `merge (branches) { ... }` as the last member of parallel group `id`. */
export function addMerge(doc: Doc, wfName: string, id: string): EditResult {
  const { wf, g } = groupOf(doc, wfName, id);
  if (g.merge) throw new EditError(`${id} already has a merge`);
  const close = doc.at(g.bodyClose);
  const indent = doc.indentAt(doc.at(g.span[0])) + IND;
  const lines = [`${indent}merge (branches) {`, ...mergeBody(doc, wf, g).map((l) => indent + IND + l), `${indent}}`];
  const text = lines.join("\n");
  if (doc.startsLine(close)) {
    const ls = doc.lineStart(close);
    return { edits: [{ start: ls, end: ls, text: `\n${text}\n` }], select: id };
  }
  return { edits: [{ start: close, end: close, text: `\n${text}\n${doc.indentAt(close)}` }], select: id };
}

/** Removes parallel group `id`'s merge block. */
export function removeMerge(doc: Doc, wfName: string, id: string): EditResult {
  const { g } = groupOf(doc, wfName, id);
  if (!g.merge) return { edits: [] };
  const [s, e] = doc.span(g.merge.span);
  const [start, end] = doc.lineRange(s, e);
  return { edits: [{ start, end, text: "" }], select: id };
}
