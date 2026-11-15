// Tool builder actions → text edits, on the same terms as sync/edits.ts:
// every change is a minimal edit against the spans `mhl graph` reported for
// a `tool` declaration (its name, braces and call sites; each method's
// header and body), so comments and formatting elsewhere stay as written.

import type { Declaration, ToolMethod } from "../lib/types";
import { Doc, EditError, type EditResult, type TextEdit } from "./edits";
import { IDENT, newToolSource, uniqueName } from "./templates";

const IND = "    ";

export function findTool(doc: Doc, name: string): Declaration {
  const t = doc.graph.declarations.find((d) => d.kind === "tool" && d.name === name);
  if (!t) throw new EditError(`tool ${name} not found`);
  return t;
}

export function findMethod(t: Declaration, name: string): ToolMethod {
  const m = t.methods?.find((x) => x.name === name);
  if (!m) throw new EditError(`${t.name} has no method ${name}`);
  return m;
}

function takenNames(doc: Doc): string[] {
  return [...doc.graph.declarations.map((d) => d.name), ...doc.graph.workflows.map((w) => w.name), ...doc.graph.imports.flatMap((i) => i.names)];
}

/**
 * Adds a new tool with one example method: after the last tool of the file,
 * else before the first workflow, else at the end.
 */
export function addTool(doc: Doc): EditResult & { name: string } {
  const name = uniqueName("NewTool", takenNames(doc));
  const text = newToolSource(name);
  const tools = doc.graph.declarations.filter((d) => d.kind === "tool");
  const lastTool = tools[tools.length - 1];
  if (lastTool) {
    const end = doc.at(lastTool.span[1]);
    return { edits: [{ start: end, end, text: "\n\n" + text }], name };
  }
  const firstWf = [...doc.graph.workflows].sort((a, b) => a.span[0] - b.span[0])[0];
  if (firstWf) {
    const start = doc.lineStart(doc.at(firstWf.span[0]));
    return { edits: [{ start, end: start, text: text + "\n\n" }], name };
  }
  const sep = doc.src.length === 0 ? "" : doc.src.endsWith("\n\n") ? "" : doc.src.endsWith("\n") ? "\n" : "\n\n";
  return { edits: [{ start: doc.src.length, end: doc.src.length, text: sep + text + "\n" }], name };
}

/** Renames a tool and its `Name.` call sites in this file. */
export function renameTool(doc: Doc, name: string, next: string): EditResult {
  const t = findTool(doc, name);
  if (next === name) return { edits: [] };
  if (!IDENT.test(next)) throw new EditError(`"${next}" is not a valid name`);
  if (takenNames(doc).includes(next)) throw new EditError(`${next} is already declared in this file`);
  const edits = [t.nameSpan!, ...(t.refs ?? [])].map((s) => {
    const [start, end] = doc.span(s);
    return { start, end, text: next };
  });
  return { edits };
}

export function deleteTool(doc: Doc, name: string): EditResult {
  const t = findTool(doc, name);
  const [s, e] = doc.span(t.span);
  const [start, end] = doc.lineRange(s, e);
  return { edits: [{ start, end, text: "" }] };
}

/** The indentation of a tool's members. */
function memberIndent(doc: Doc, t: Declaration): string {
  const first = t.methods?.[0];
  if (first && doc.startsLine(doc.at(first.span[0]))) return doc.indentAt(doc.at(first.span[0]));
  return doc.indentAt(doc.at(t.span[0])) + IND;
}

/** Appends a new method (`method(input: string): string -> input`) to a tool. */
export function addMethod(doc: Doc, toolName: string): EditResult & { name: string } {
  const t = findTool(doc, toolName);
  const name = uniqueName("method", (t.methods ?? []).map((m) => m.name));
  const indent = memberIndent(doc, t);
  const line = `${name}(input: string): string -> input`;
  const open = doc.at(t.bodyOpen!);
  const close = doc.at(t.bodyClose!);
  const toolIndent = doc.indentAt(doc.at(t.span[0]));
  if (doc.src.slice(open + 1, close).trim() === "") {
    return { edits: [{ start: open + 1, end: close, text: `\n${indent}${line}\n${toolIndent}` }], name };
  }
  if (doc.startsLine(close)) {
    const ls = doc.lineStart(close);
    return { edits: [{ start: ls, end: ls, text: `${indent}${line}\n` }], name };
  }
  // `tool T { a() -> 1 }` — break the closing brace onto its own line.
  let s = close;
  while (s > open + 1 && /[ \t]/.test(doc.src[s - 1])) s--;
  return { edits: [{ start: s, end: close, text: `\n${indent}${line}\n${toolIndent}` }], name };
}

export function deleteMethod(doc: Doc, toolName: string, methodName: string): EditResult {
  const m = findMethod(findTool(doc, toolName), methodName);
  const [s, e] = doc.span(m.span);
  const [start, end] = doc.lineRange(s, e);
  return { edits: [{ start, end, text: "" }] };
}

export interface ArgSpec {
  name: string;
  type: string;
  default: string;
}

export interface Signature {
  name: string;
  internal: boolean;
  args: ArgSpec[];
  returns: string;
}

export function signatureOf(m: ToolMethod): Signature {
  return {
    name: m.name,
    internal: !!m.internal,
    args: m.args.map((a) => ({ name: a.name, type: a.type ?? "", default: a.default ?? "" })),
    returns: m.returns ?? "",
  };
}

/** `internal name(a: T, b: U = d): R` */
export function renderHeader(sig: Signature): string {
  const args = sig.args.map((a) => `${a.name.trim()}${a.type.trim() ? `: ${a.type.trim()}` : ""}${a.default.trim() ? ` = ${a.default.trim()}` : ""}`);
  return `${sig.internal ? "internal " : ""}${sig.name.trim()}(${args.join(", ")})${sig.returns.trim() ? `: ${sig.returns.trim()}` : ""}`;
}

function checkSignature(t: Declaration, current: string, sig: Signature) {
  const name = sig.name.trim();
  if (!IDENT.test(name)) throw new EditError(`"${name}" is not a valid method name`);
  if (name !== current && t.methods?.some((m) => m.name === name)) throw new EditError(`${t.name} already has a method ${name}`);
  const seen = new Set<string>();
  let defaulted = false;
  for (const a of sig.args) {
    const n = a.name.trim();
    if (!IDENT.test(n)) throw new EditError(`"${n}" is not a valid parameter name`);
    if (seen.has(n)) throw new EditError(`parameter ${n} is declared twice`);
    seen.add(n);
    if (a.default.trim()) defaulted = true;
    else if (defaulted) throw new EditError(`parameter ${n} needs a default: parameters after one with a default must have one too`);
  }
}

/** The spans naming method `name` of tool t: `T.name` call sites and `self.name` inside t. */
function methodRefs(doc: Doc, t: Declaration, name: string): [number, number][] {
  const out: [number, number][] = [];
  for (const r of t.refs ?? []) {
    const end = doc.at(r[1]);
    const m = /^\s*\.\s*([A-Za-z_][A-Za-z0-9_]*)/.exec(doc.src.slice(end, end + 200));
    if (m && m[1] === name) {
      const s = end + m[0].length - name.length;
      out.push([s, s + name.length]);
    }
  }
  const [a, b] = doc.span(t.span);
  const re = new RegExp(`\\bself\\s*\\.\\s*(${name})\\b`, "g");
  const body = doc.src.slice(a, b);
  for (let m = re.exec(body); m; m = re.exec(body)) {
    const s = a + m.index + m[0].length - name.length;
    out.push([s, s + name.length]);
  }
  return out;
}

/**
 * Rewrites a method's signature. A rename also rewrites its call sites in
 * this file (`T.old(...)`, `self.old(...)`).
 */
export function setSignature(doc: Doc, toolName: string, methodName: string, sig: Signature): EditResult {
  const t = findTool(doc, toolName);
  const m = findMethod(t, methodName);
  checkSignature(t, methodName, sig);
  const [hs, he] = doc.span(m.headerSpan);
  const edits: TextEdit[] = [{ start: hs, end: he, text: renderHeader(sig) }];
  const next = sig.name.trim();
  if (next !== methodName) {
    for (const [start, end] of methodRefs(doc, t, methodName)) {
      if (start >= hs && end <= he) continue;
      edits.push({ start, end, text: next });
    }
  }
  return { edits };
}

function dedent(lines: string[], indent: string): string[] {
  return lines.map((l) => (l.startsWith(indent) ? l.slice(indent.length) : l.trimStart()));
}

/** A method's body as an editor shows it: the expression, or the block's statements, dedented. */
export function getMethodBody(doc: Doc, toolName: string, methodName: string): { kind: ToolMethod["bodyKind"]; text: string } {
  const m = findMethod(findTool(doc, toolName), methodName);
  const indent = doc.indentAt(doc.at(m.span[0])) + IND;
  const raw = doc.text(m.bodySpan);
  if (m.bodyKind === "expr") {
    const [first, ...rest] = raw.split("\n");
    return { kind: "expr", text: [first, ...dedent(rest, indent)].join("\n") };
  }
  const inner = raw.slice(1, -1).replace(/^[ \t]*\n/, "").replace(/\n[ \t]*$/, "");
  const lines = inner.split("\n");
  if (lines.length === 1) return { kind: "block", text: lines[0].trim() };
  return { kind: "block", text: dedent(lines, indent).join("\n") };
}

/**
 * Replaces a method's body. kind "expr" writes `-> expression`; "block"
 * writes `{ statements }` — so switching kind converts the method.
 */
export function setMethodBody(doc: Doc, toolName: string, methodName: string, kind: ToolMethod["bodyKind"], text: string): EditResult {
  const m = findMethod(findTool(doc, toolName), methodName);
  const indent = doc.indentAt(doc.at(m.span[0]));
  const lines = text.replace(/\s+$/, "").replace(/^\s*\n/, "").split("\n");
  let body: string;
  if (kind === "expr") {
    if (!text.trim()) throw new EditError("an expression body cannot be empty — switch to a block for a method that returns nothing");
    body = ` -> ${lines.map((l, i) => (i === 0 ? l.trim() : l.trim() ? indent + IND + l : "")).join("\n")}`;
  } else if (!text.trim()) {
    body = ` {\n${indent}}`;
  } else {
    body = ` {\n${lines.map((l) => (l.trim() ? indent + IND + l : "")).join("\n")}\n${indent}}`;
  }
  return { edits: [{ start: doc.at(m.headerSpan[1]), end: doc.at(m.span[1]), text: body }] };
}

/** Turns an expression body into a block that returns it, and back when the block is a lone `return`. */
export function convertedBody(kind: ToolMethod["bodyKind"], text: string): { kind: ToolMethod["bodyKind"]; text: string } {
  if (kind === "expr") return { kind: "block", text: `return ${text.trim()}` };
  const only = /^return\s+([\s\S]+)$/.exec(text.trim());
  if (only && !only[1].includes("\n")) return { kind: "expr", text: only[1].trim() };
  throw new EditError("only a block made of a single `return value` can become an expression");
}

/**
 * The call a "Test method" run evaluates: `T.m(v1, v2)`, with each argument
 * an mhl expression as typed. Trailing blanks of defaulted parameters are
 * left out so their defaults apply.
 */
export function testCall(toolName: string, m: ToolMethod, values: string[]): string {
  const vals = m.args.map((_, i) => (values[i] ?? "").trim());
  let n = vals.length;
  while (n > 0 && !vals[n - 1] && m.args[n - 1].default !== undefined) n--;
  for (let i = 0; i < n; i++) {
    if (!vals[i]) {
      const a = m.args[i];
      if (a.default !== undefined) throw new EditError(`give ${a.name} a value: a later argument is set, so its default cannot apply`);
      throw new EditError(`${a.name} is required`);
    }
  }
  return `${toolName}.${m.name}(${vals.slice(0, n).join(", ")})`;
}
