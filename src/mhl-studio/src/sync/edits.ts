// Canvas actions → text edits. The source text is the single source of
// truth: every structural change made on the canvas or in the inspector is
// computed here as a minimal set of TextEdits against the spans `mhl graph`
// reported, applied to the editor model (so undo/redo covers both views),
// and then the graph is rebuilt from the new text. Text outside the edited
// ranges — comments, formatting — is never touched.

import type { Declaration, Graph, GraphEdge, GraphNode, Span, Workflow } from "../lib/types";
import { END, FAIL } from "../lib/types";
import { OffsetMap } from "./offsets";
import { renderState, uniqueName, STATE_PREFIX, type StateKind, type TemplateContext } from "./templates";

/** A replacement of the UTF-16 range [start, end) with text. */
export interface TextEdit {
  start: number;
  end: number;
  text: string;
}

export interface EditResult {
  edits: TextEdit[];
  /** Node id to select once the edit is applied (a newly created state). */
  select?: string;
}

export class EditError extends Error {}

const IND = "    ";

/** Applies edits to text (pure; used by tests and as a fallback). */
export function applyEdits(text: string, edits: TextEdit[]): string {
  const sorted = [...edits].sort((a, b) => b.start - a.start || b.end - a.end);
  let out = text;
  for (const e of sorted) out = out.slice(0, e.start) + e.text + out.slice(e.end);
  return out;
}

/** One source text plus the graph built from it. */
export class Doc {
  readonly map: OffsetMap;
  constructor(
    readonly src: string,
    readonly graph: Graph,
    /** Declarations imported from other files (no spans in this text). */
    readonly external: Declaration[] = [],
  ) {
    this.map = new OffsetMap(src);
  }

  /** A byte offset as a UTF-16 index. */
  at(byte: number): number {
    return this.map.toUtf16(byte);
  }

  span(s: Span): [number, number] {
    return [this.at(s[0]), this.at(s[1])];
  }

  text(s: Span): string {
    const [a, b] = this.span(s);
    return this.src.slice(a, b);
  }

  workflow(name: string): Workflow {
    const wf = this.graph.workflows.find((w) => w.name === name);
    if (!wf) throw new EditError(`workflow ${name} not found`);
    return wf;
  }

  node(wf: Workflow, id: string): GraphNode {
    const n = wf.nodes.find((x) => x.id === id);
    if (!n) throw new EditError(`step ${id} not found`);
    return n;
  }

  lineStart(i: number): number {
    return this.src.lastIndexOf("\n", i - 1) + 1;
  }

  lineEnd(i: number): number {
    const j = this.src.indexOf("\n", i);
    return j < 0 ? this.src.length : j;
  }

  /** The whitespace that starts the line containing i. */
  indentAt(i: number): string {
    const ls = this.lineStart(i);
    return /^[ \t]*/.exec(this.src.slice(ls))![0];
  }

  /** Whether only whitespace separates i from the start of its line. */
  startsLine(i: number): boolean {
    return /^[ \t]*$/.test(this.src.slice(this.lineStart(i), i));
  }

  /** Whether only whitespace follows i to the end of its line. */
  endsLine(i: number): boolean {
    return /^[ \t\r]*$/.test(this.src.slice(i, this.lineEnd(i)));
  }

  /**
   * The range to delete to remove [s, e) as whole lines when it occupies
   * them alone, swallowing one blank separator line so no double gap remains.
   */
  lineRange(s: number, e: number): [number, number] {
    let start = this.startsLine(s) ? this.lineStart(s) : s;
    let end = e;
    if (this.endsLine(e)) {
      end = Math.min(this.lineEnd(e) + 1, this.src.length);
      const blankAfter = /^[ \t\r]*\n/.exec(this.src.slice(end));
      const blankBefore = start >= 2 && /\n[ \t\r]*\n$/.test(this.src.slice(0, start));
      if (blankAfter && blankBefore) end += blankAfter[0].length;
      else if (!blankAfter && blankBefore && start > 0 && this.src[end - 1] !== "\n") {
        start = this.lineStart(start - 1);
      }
    }
    return [start, end];
  }
}

function topLevel(wf: Workflow): GraphNode[] {
  return wf.nodes.filter((n) => !n.parent);
}

/** Local declarations of a kind, then the imported ones. */
function declsOf(doc: Doc, kind: Declaration["kind"]): Declaration[] {
  return [...doc.graph.declarations, ...doc.external].filter((d) => d.kind === kind);
}

function bodyIndent(doc: Doc, wf: Workflow): string {
  const first = topLevel(wf)[0];
  if (first) return doc.indentAt(doc.at(first.span[0]));
  return doc.indentAt(doc.at(wf.span[0])) + IND;
}

export type Placement = { after: string } | { before: string } | { into: string } | { end: true };

/** Inserts a new palette state into a workflow. */
export function insertState(
  doc: Doc,
  wfName: string,
  kind: StateKind,
  where: Placement,
  call?: TemplateContext["call"],
  agent?: string,
  prompt?: string,
  schema?: string,
): EditResult {
  const wf = doc.workflow(wfName);
  const taken = [...wf.nodes.map((n) => n.id), ...wf.routes.map((r) => r.name), ...wf.externals];
  const name = uniqueName(STATE_PREFIX[kind], taken);
  const tops = topLevel(wf);
  const ctx = (indent: string) => ({
    name,
    indent,
    agents: declsOf(doc, "agent"),
    tools: declsOf(doc, "tool"),
    others: tops.map((n) => n.id),
    call,
    agent,
    prompt,
    schema,
  });

  if ("into" in where) {
    const group = doc.node(wf, where.into);
    if (group.kind !== "parallel") throw new EditError(`${group.id} is not a parallel group`);
    if (kind === "parallel" || kind === "choice") {
      throw new EditError("a parallel branch cannot itself branch: no goto or nested parallel inside a parallel group");
    }
    const close = doc.at(group.bodyClose);
    const indent = doc.indentAt(doc.at(group.span[0])) + IND;
    const text = renderState(kind, ctx(indent));
    if (doc.startsLine(close)) return { edits: [{ start: doc.lineStart(close), end: doc.lineStart(close), text: text + "\n" }], select: name };
    return { edits: [{ start: close, end: close, text: "\n" + text + "\n" + doc.indentAt(close) }], select: name };
  }

  let anchor: GraphNode | undefined;
  if ("after" in where || "before" in where) {
    const id = "after" in where ? where.after : where.before;
    anchor = doc.node(wf, id);
    if (anchor.parent) anchor = doc.node(wf, anchor.parent);
  }
  if ("before" in where && anchor) {
    const start = doc.at(anchor.span[0]);
    const indent = doc.indentAt(start);
    const text = renderState(kind, ctx(indent)).slice(indent.length);
    return { edits: [{ start, end: start, text: text + "\n\n" + indent }], select: name };
  }
  if (!anchor && tops.length > 0) anchor = tops[tops.length - 1];
  if (anchor) {
    const end = doc.at(anchor.span[1]);
    const indent = doc.indentAt(doc.at(anchor.span[0]));
    return { edits: [{ start: end, end, text: "\n\n" + renderState(kind, ctx(indent)) }], select: name };
  }
  // An empty workflow body.
  const close = doc.at(wf.bodyClose);
  const indent = bodyIndent(doc, wf);
  const text = renderState(kind, ctx(indent));
  if (doc.startsLine(close)) return { edits: [{ start: doc.lineStart(close), end: doc.lineStart(close), text: text + "\n" }], select: name };
  return { edits: [{ start: close, end: close, text: "\n" + text + "\n" + doc.indentAt(close) }], select: name };
}

export function deleteNode(doc: Doc, wfName: string, id: string): EditResult {
  const wf = doc.workflow(wfName);
  const n = doc.node(wf, id);
  if (n.parent) {
    const group = doc.node(wf, n.parent);
    if ((group.children ?? []).length <= 1) {
      throw new EditError(`${id} is the only branch of ${group.id}; delete the parallel group instead`);
    }
  }
  const [s, e] = doc.span(n.span);
  const [start, end] = doc.lineRange(s, e);
  return { edits: [{ start, end, text: "" }] };
}

/** Every span in wf that names step `id` (its declaration and references). */
function referenceSpans(wf: Workflow, id: string): Span[] {
  const spans: Span[] = [];
  const seen = new Set<number>();
  const add = (s?: Span) => {
    if (s && !seen.has(s[0])) {
      seen.add(s[0]);
      spans.push(s);
    }
  };
  for (const e of wf.edges) if (e.to === id) add(e.span);
  for (const r of wf.routes) for (const a of r.arms) if (a.target === id) add(a.targetSpan);
  return spans;
}

export function renameNode(doc: Doc, wfName: string, id: string, next: string): EditResult {
  const wf = doc.workflow(wfName);
  const n = doc.node(wf, id);
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(next)) throw new EditError(`"${next}" is not a valid name`);
  if (next === id) return { edits: [] };
  if (wf.nodes.some((x) => x.id === next) || wf.routes.some((r) => r.name === next)) {
    throw new EditError(`${next} already exists in ${wf.name}`);
  }
  const edits: TextEdit[] = [n.nameSpan, ...referenceSpans(wf, id)].map((s) => {
    const [start, end] = doc.span(s);
    return { start, end, text: next };
  });
  return { edits, select: next };
}

export function renameWorkflow(doc: Doc, wfName: string, next: string): EditResult {
  const wf = doc.workflow(wfName);
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(next)) throw new EditError(`"${next}" is not a valid name`);
  const [start, end] = doc.span(wf.nameSpan);
  return { edits: [{ start, end, text: next }] };
}

export function setTimeoutClause(doc: Doc, wfName: string, id: string, value: string): EditResult {
  const n = doc.node(doc.workflow(wfName), id);
  const v = value.trim();
  if (v && !/^[0-9]+(ms|s|m|h|d)$/.test(v)) throw new EditError(`"${v}" is not a duration (30s, 5m, 2h, ...)`);
  if (n.timeoutSpan) {
    const [s, e] = doc.span(n.timeoutSpan);
    if (v) return { edits: [{ start: s, end: e, text: `timeout ${v}` }] };
    const start = doc.src[s - 1] === " " ? s - 1 : s;
    return { edits: [{ start, end: e, text: "" }] };
  }
  if (!v) return { edits: [] };
  const at = doc.at(n.nameSpan[1]);
  return { edits: [{ start: at, end: at, text: ` timeout ${v}` }] };
}

/** Appends one statement as the last line of a step body. */
function appendStatement(doc: Doc, n: GraphNode, stmt: string): TextEdit {
  const open = doc.at(n.bodyOpen);
  const close = doc.at(n.bodyClose);
  const indent = doc.indentAt(doc.at(n.span[0]));
  const inner = doc.src.slice(open + 1, close);
  if (inner.trim() === "") {
    return { start: open + 1, end: close, text: `\n${indent}${IND}${stmt}\n${indent}` };
  }
  if (doc.startsLine(close)) {
    const ls = doc.lineStart(close);
    return { start: ls, end: ls, text: `${indent}${IND}${stmt}\n` };
  }
  // `step A { x = 1 }` — break the closing brace onto its own line.
  let s = close;
  while (s > open + 1 && /[ \t]/.test(doc.src[s - 1])) s--;
  return { start: s, end: close, text: `\n${indent}${IND}${stmt}\n${indent}` };
}

function statementFor(to: string): string {
  if (to === END) return "complete()";
  if (to === FAIL) return 'fail("TODO: reason")';
  return `goto ${to}`;
}

/** Makes `from` transfer control to `to` (a step, END or FAIL). */
export function connect(doc: Doc, wfName: string, from: string, to: string): EditResult {
  const wf = doc.workflow(wfName);
  const n = doc.node(wf, from);
  if (n.parent) throw new EditError("a step inside a parallel group cannot goto; connect the group instead");
  if (n.kind === "parallel") throw new EditError("a parallel group always continues with the next step; connect from a step");
  if (to !== END && to !== FAIL) {
    const target = doc.node(wf, to);
    if (target.parent) throw new EditError(`${to} is inside a parallel group; a goto must target a top-level step`);
  }
  if (wf.kind !== "workflow" && to !== END && to !== FAIL) {
    throw new EditError(`goto is only allowed in a workflow; ${wf.name} is a pipeline`);
  }
  const tops = topLevel(wf);
  const i = tops.findIndex((x) => x.id === from);
  if (n.fallsThrough && tops[i + 1]?.id === to) return { edits: [] };
  if (n.fallsThrough && i === tops.length - 1 && to === END) return { edits: [] };
  // Retarget an existing unconditional trailing `goto X`.
  if (to !== END && to !== FAIL) {
    const plain = wf.edges.filter((e) => e.from === from && e.kind === "goto" && !e.label && e.span);
    if (plain.length === 1 && !n.fallsThrough) {
      const [start, end] = doc.span(plain[0].span!);
      return { edits: [{ start, end, text: to }] };
    }
  }
  return { edits: [appendStatement(doc, n, statementFor(to))] };
}

/** Removes the transfer an edge stands for, when it maps to one line. */
export function removeEdge(doc: Doc, wfName: string, edge: GraphEdge): EditResult {
  const wf = doc.workflow(wfName);
  if (edge.kind === "next") {
    if (edge.from === "$start") throw new EditError("the first step always starts the workflow");
    const n = doc.node(wf, edge.from);
    if (n.kind === "parallel" || n.parent) throw new EditError("a parallel group always continues with the next step");
    return { edits: [appendStatement(doc, n, "complete()")] };
  }
  if ((edge.kind === "goto" || edge.kind === "match") && edge.span) {
    const [s] = doc.span(edge.span);
    const ls = doc.lineStart(s);
    const line = doc.src.slice(ls, doc.lineEnd(s)).trim();
    const removable =
      edge.kind === "goto"
        ? /^(if\s*\(.*\)\s*)?goto\s+(nameof\s*\(\s*)?[A-Za-z_]\w*\s*\)?$/.test(line)
        : /^.+->\s*[A-Za-z_]\w*,?$/.test(line) && !line.startsWith("goto");
    if (removable) {
      const [start, end] = doc.lineRange(ls + (doc.src.slice(ls).length - doc.src.slice(ls).trimStart().length), doc.lineEnd(s));
      return { edits: [{ start, end, text: "" }] };
    }
  }
  throw new EditError("this transition is part of a larger statement — edit it in the code view");
}

/** The step's body, dedented by one level relative to the step line. */
export function getBody(doc: Doc, wfName: string, id: string): string {
  const n = doc.node(doc.workflow(wfName), id);
  const inner = doc.src.slice(doc.at(n.bodyOpen) + 1, doc.at(n.bodyClose));
  const lines = inner.replace(/^[ \t]*\n/, "").replace(/\n[ \t]*$/, "").split("\n");
  const indent = doc.indentAt(doc.at(n.span[0])) + IND;
  if (lines.length === 1) return lines[0].trim();
  return lines.map((l) => (l.startsWith(indent) ? l.slice(indent.length) : l.trimStart())).join("\n");
}

export function setBody(doc: Doc, wfName: string, id: string, body: string): EditResult {
  const n = doc.node(doc.workflow(wfName), id);
  const indent = doc.indentAt(doc.at(n.span[0]));
  const lines = body.replace(/\s+$/, "").split("\n");
  const text = lines.length === 1 && lines[0].trim() === "" ? `\n${indent}` : `\n${lines.map((l) => (l.trim() ? indent + IND + l : "")).join("\n")}\n${indent}`;
  return { edits: [{ start: doc.at(n.bodyOpen) + 1, end: doc.at(n.bodyClose), text }] };
}

/** Swaps a top-level node with its neighbor (dir -1 = earlier, +1 = later). */
export function moveNode(doc: Doc, wfName: string, id: string, dir: -1 | 1): EditResult {
  const wf = doc.workflow(wfName);
  const n = doc.node(wf, id);
  const siblings = n.parent ? wf.nodes.filter((x) => x.parent === n.parent) : topLevel(wf);
  const i = siblings.findIndex((x) => x.id === id);
  const j = i + dir;
  if (j < 0 || j >= siblings.length) return { edits: [] };
  const a = doc.span(siblings[i].span);
  const b = doc.span(siblings[j].span);
  return {
    edits: [
      { start: a[0], end: a[1], text: doc.src.slice(b[0], b[1]) },
      { start: b[0], end: b[1], text: doc.src.slice(a[0], a[1]) },
    ],
    select: id,
  };
}

/** Inserts a body-level member line right after `{` (or after `afterSpan`). */
function insertMember(doc: Doc, wf: Workflow, line: string, afterSpan?: Span): TextEdit {
  const indent = bodyIndent(doc, wf);
  if (afterSpan) {
    const e = doc.at(afterSpan[1]);
    return { start: e, end: e, text: `\n${indent}${line}` };
  }
  const open = doc.at(wf.bodyOpen) + 1;
  return { start: open, end: open, text: `\n${indent}${line}\n` };
}

export function addInput(doc: Doc, wfName: string, name: string, type: string, def: string): EditResult {
  const wf = doc.workflow(wfName);
  if (wf.param) throw new EditError(`${wf.name} takes a typed parameter (${wf.param.name}: ${wf.param.type}); add a field to that type instead`);
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name)) throw new EditError(`"${name}" is not a valid input name`);
  if (wf.inputs.some((i) => i.name === name)) throw new EditError(`input ${name} already exists`);
  const line = `input ${name}: ${type || "string"}${def.trim() ? ` = ${def.trim()}` : ""}`;
  const last = wf.inputs[wf.inputs.length - 1];
  const desc = wf.props.find((p) => p.name === "description");
  return { edits: [insertMember(doc, wf, line, last?.span ?? desc?.span)] };
}

export function removeInput(doc: Doc, wfName: string, name: string): EditResult {
  const wf = doc.workflow(wfName);
  const input = wf.inputs.find((i) => i.name === name);
  if (!input) throw new EditError(`input ${name} not found`);
  const [s, e] = doc.span(input.span);
  const [start, end] = doc.lineRange(s, e);
  return { edits: [{ start, end, text: "" }] };
}

export function setDescription(doc: Doc, wfName: string, text: string): EditResult {
  const wf = doc.workflow(wfName);
  const prop = wf.props.find((p) => p.name === "description");
  const line = `description: ${JSON.stringify(text)}`;
  if (prop) {
    const [s, e] = doc.span(prop.span);
    if (!text) {
      const [start, end] = doc.lineRange(s, e);
      return { edits: [{ start, end, text: "" }] };
    }
    return { edits: [{ start: s, end: e, text: line }] };
  }
  if (!text) return { edits: [] };
  return { edits: [insertMember(doc, wf, line)] };
}

/** Appends a new workflow declaration to the end of the file. */
export function addWorkflow(doc: Doc, source: (name: string) => string): EditResult & { name: string } {
  const name = uniqueName("Workflow", [...doc.graph.workflows.map((w) => w.name), ...doc.graph.declarations.map((d) => d.name)]);
  const sep = doc.src.length === 0 ? "" : doc.src.endsWith("\n\n") ? "" : doc.src.endsWith("\n") ? "\n" : "\n\n";
  return { edits: [{ start: doc.src.length, end: doc.src.length, text: sep + source(name) }], name };
}

/** Names a new `var` in step `id` must not reuse: the step's own vars and the workflow's vars and inputs. */
export function takenNames(doc: Doc, wfName: string, id: string): string[] {
  const wf = doc.workflow(wfName);
  const n = doc.node(wf, id);
  const body = doc.src.slice(doc.at(n.bodyOpen), doc.at(n.bodyClose));
  const local = [...body.matchAll(/\b(?:var|const)\s+([A-Za-z_][A-Za-z0-9_]*)/g)].map((m) => m[1]);
  return [...local, ...wf.vars.map((v) => v.name), ...wf.inputs.map((i) => i.name)];
}

/**
 * Adds a statement to step `id`: as its last line, or — when the step always
 * leaves through its last statement (goto, complete(), fail(), ...) — just
 * before that statement, so the new line still runs.
 */
export function addToStep(doc: Doc, wfName: string, id: string, stmt: string): EditResult {
  const wf = doc.workflow(wfName);
  const n = doc.node(wf, id);
  if (n.kind !== "step") throw new EditError(`${id} is a parallel group — drop onto one of its steps`);
  if (n.fallsThrough) return { edits: [appendStatement(doc, n, stmt)], select: id };
  const open = doc.at(n.bodyOpen) + 1;
  const close = doc.at(n.bodyClose);
  const last = lastStatementStart(doc.src, open, close);
  if (last < 0 || !doc.startsLine(last)) return { edits: [appendStatement(doc, n, stmt)], select: id };
  const ls = doc.lineStart(last);
  return { edits: [{ start: ls, end: ls, text: `${doc.indentAt(last)}${stmt}\n` }], select: id };
}

/** Start offset of the last top-level statement in src[open, close), or -1. */
function lastStatementStart(src: string, open: number, close: number): number {
  let depth = 0;
  let last = -1;
  let lineStart = true;
  let quote: string | null = null;
  for (let i = open; i < close; i++) {
    const c = src[i];
    if (quote) {
      if (c === "\\") i++;
      else if (c === quote) quote = null;
      continue;
    }
    if (c === "/" && src[i + 1] === "/") {
      while (i < close && src[i] !== "\n") i++;
      lineStart = true;
      continue;
    }
    if (c === "\n") {
      lineStart = true;
      continue;
    }
    if (c === " " || c === "\t" || c === "\r") continue;
    if (lineStart && depth === 0) last = i;
    lineStart = false;
    if (c === '"' || c === "'" || c === "`") quote = c;
    else if (c === "{" || c === "(" || c === "[") depth++;
    else if (c === "}" || c === ")" || c === "]") depth--;
  }
  return last;
}
