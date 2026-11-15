// A parallel group's `merge (branches) { ... }` block: the explicit join that
// combines what its branches wrote. Without one, two branches assigning the
// same workflow var different values fails the run; with one, the merge has
// to assign that var itself, reading each branch's value as
// `branches.<Step>.<var>`. Adding one writes those assignments for the vars
// it can see more than one branch assigning.
import type { GraphNode, Workflow } from "../lib/types";
import { EditError, type Doc, type EditResult } from "./edits";

const IND = "    ";

/**
 * The workflow vars (by name) the branch's body assigns: `x = ...`,
 * `self.x = ...`, or `x.append(...)` as a statement (which assigns x).
 */
function assigned(body: string, vars: string[]): Set<string> {
  const out = new Set<string>();
  for (const m of body.matchAll(/(?:^|[^\w.])(?:self\.)?([A-Za-z_]\w*)\s*(?:=(?!=)|\.append\()/g)) {
    if (vars.includes(m[1])) out.add(m[1]);
  }
  return out;
}

/** Whether a workflow var, or one of group's own, starts as an array (`var x = [...]`). */
function isArray(doc: Doc, wf: Workflow, name: string, group?: GraphNode): boolean {
  const v = [...(group?.vars ?? []), ...wf.vars].find((x) => x.name === name);
  return !!v && /=\s*\[/.test(doc.text(v.span));
}

/**
 * The vars two or more of the group's branches assign — the ones that, as
 * things stand, make the run fail at the barrier — each with those branches.
 */
export function sharedWrites(doc: Doc, wf: Workflow, group: GraphNode): { name: string; by: string[] }[] {
  const names = [...(group.vars ?? []), ...wf.vars.filter((v) => v.kind === "var")].map((v) => v.name);
  const by = new Map<string, string[]>();
  for (const id of group.children ?? []) {
    const n = doc.node(wf, id);
    const body = doc.src.slice(doc.at(n.bodyOpen) + 1, doc.at(n.bodyClose));
    for (const name of assigned(body, names)) by.set(name, [...(by.get(name) ?? []), id]);
  }
  return [...by].filter(([, steps]) => steps.length > 1).map(([name, steps]) => ({ name, by: steps }));
}

/**
 * The body lines of a new merge for `group`. It needs the branches object
 * (`param`) only to combine a var several branches wrote; otherwise it reads
 * the group's vars through self.
 */
export function mergeBody(doc: Doc, wf: Workflow, group: GraphNode, param = "branches"): string[] {
  const shared = sharedWrites(doc, wf, group);
  if (!shared.length) {
    const own = (group.vars ?? []).map((v) => `self.${v.name}`);
    return [own.length ? `// runs after every branch; the group's vars: ${own.join(", ")}` : "// runs once after every branch"];
  }
  return shared.map(({ name, by }) =>
    isArray(doc, wf, name, group)
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

/**
 * Adds a `merge (branches) { ... }` as the last member of parallel group
 * `id`, ending with `then` (a `goto X` the group leaves through, say).
 */
export function addMerge(doc: Doc, wfName: string, id: string, then: string[] = []): EditResult {
  const { wf, g } = groupOf(doc, wfName, id);
  if (g.merge) throw new EditError(`${id} already has a merge`);
  const close = doc.at(g.bodyClose);
  const indent = doc.indentAt(doc.at(g.span[0])) + IND;
  const shared = sharedWrites(doc, wf, g).length > 0;
  let body = mergeBody(doc, wf, g);
  if (then.length && body.length === 1 && body[0].startsWith("//")) body = [];
  const lines = [`${indent}merge${shared ? " (branches)" : ""} {`, ...[...body, ...then].map((l) => indent + IND + l), `${indent}}`];
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

const IDENT = /^[A-Za-z_]\w*$/;

/**
 * Declares `var name = value` in parallel group `id`: after its last var, or
 * before its first branch. It lives only while the group runs, so it must not
 * reuse a workflow input or var name, or another var of the group.
 */
export function addGroupVar(doc: Doc, wfName: string, id: string, name: string, value: string): EditResult {
  const { wf, g } = groupOf(doc, wfName, id);
  const n = name.trim();
  if (!IDENT.test(n)) throw new EditError(`"${n}" is not a valid name`);
  if ([...wf.vars, ...wf.inputs].some((v) => v.name === n)) throw new EditError(`${n} is already a workflow input or var — a group var needs a name of its own`);
  if ((g.vars ?? []).some((v) => v.name === n)) throw new EditError(`${id} already has a var ${n}`);
  const v = value.trim() || '""';
  const last = g.vars?.[g.vars.length - 1];
  if (last) {
    const end = doc.at(last.span[1]);
    return { edits: [{ start: end, end, text: `\n${doc.indentAt(doc.at(last.span[0]))}var ${n} = ${v}` }], select: id };
  }
  const first = doc.node(wf, g.children![0]);
  const start = doc.at(first.span[0]);
  const indent = doc.indentAt(start);
  if (doc.startsLine(start)) {
    const ls = doc.lineStart(start);
    return { edits: [{ start: ls, end: ls, text: `${indent}var ${n} = ${v}\n\n` }], select: id };
  }
  return { edits: [{ start, end: start, text: `var ${n} = ${v} ` }], select: id };
}

/** Removes var `name` of parallel group `id`. */
export function removeGroupVar(doc: Doc, wfName: string, id: string, name: string): EditResult {
  const { g } = groupOf(doc, wfName, id);
  const v = (g.vars ?? []).find((x) => x.name === name);
  if (!v) return { edits: [] };
  const [s, e] = doc.span(v.span);
  const [start, end] = doc.lineRange(s, e);
  return { edits: [{ start, end, text: "" }], select: id };
}
