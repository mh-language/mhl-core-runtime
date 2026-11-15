// The Choice state: a step whose body is `goto match subject { arms }`.
// What it branches on (an input or a workflow var), the patterns that value
// can take (an enum's variants, true/false), and the text edits behind the
// inspector's rule editor — each one rewrites a span `mhl graph` reported
// for the statement (GraphNode.choice), never the whole step.
import type { Arm, Choice, Declaration, GraphNode, Workflow } from "../lib/types";
import { FAIL } from "../lib/types";
import { EditError, type Doc, type EditResult } from "./edits";

const IND = "    ";
const SIMPLE = /^[A-Za-z_]\w*(\.[A-Za-z_]\w*)*$/;

/** A value a choice can branch on: an input (with its type) or a workflow var. */
export interface ChoiceValue {
  expr: string;
  type?: string;
  source: "input" | "var";
}

/** The values in scope in every step of `wf`, inputs first. */
export function choiceValues(wf: Workflow): ChoiceValue[] {
  const inputs = wf.inputs.map((i) => ({ expr: wf.param ? `${wf.param.name}.${i.name}` : i.name, type: i.type, source: "input" as const }));
  const vars = wf.vars.filter((v) => v.kind === "var" || v.kind === "const").map((v) => ({ expr: v.name, source: "var" as const }));
  return [...inputs, ...vars];
}

/** The patterns a value of `type` can match: an enum's variants, or true/false. */
export function patternsFor(decls: Declaration[], type: string | undefined): string[] | null {
  if (!type) return null;
  const t = type.replace(/\?$/, "");
  if (t === "bool") return ["true", "false"];
  const e = decls.find((d) => d.kind === "enum" && d.name === t);
  return e?.variants?.length ? e.variants.map((v) => `${t}.${v}`) : null;
}

/** The patterns `subject` can take, when it names a typed input. */
export function subjectPatterns(decls: Declaration[], wf: Workflow, subject: string): string[] | null {
  return patternsFor(decls, choiceValues(wf).find((v) => v.expr === subject.trim())?.type);
}

/** The arm text after `->` for a target: the step name, or a `fail(...)`. */
function thenText(target: string, subject: string): string {
  if (target !== FAIL) return target;
  return SIMPLE.test(subject) ? `fail("no branch matched: \${${subject}}")` : `fail("no branch matched")`;
}

/** What a new Choice step branches on, and its first rules. */
export interface ChoiceTemplate {
  subject: string;
  /** pattern → target step (null: no step to go to yet). */
  arms: [string, string | null][];
  /** A workflow var the template needs declared (nothing else in scope). */
  needsVar?: string;
}

/**
 * The Choice a new step starts as: on an enum-typed input, one rule per
 * variant; on a bool, true/false; else on the first input or var, one rule
 * per following step, its pattern named after it. With nothing in scope it
 * branches on a new `decision` var, which an earlier step sets.
 */
export function choiceTemplate(decls: Declaration[], wf: Workflow, others: string[]): ChoiceTemplate {
  const values = choiceValues(wf);
  const rank = (v: ChoiceValue) => (v.type === "bool" ? 1 : patternsFor(decls, v.type) ? 0 : 2);
  const pick = [...values].sort((a, b) => rank(a) - rank(b))[0];
  const subject = pick?.expr ?? "decision";
  const pats = patternsFor(decls, pick?.type);
  const arms: ChoiceTemplate["arms"] = pats
    ? pats.slice(0, Math.max(1, others.length)).map((p, i) => [p, others[i] ?? null])
    : others.length
      ? others.slice(0, 2).map((t) => [`"${t.toLowerCase()}"`, t])
      : [['"a"', null]];
  return { subject, arms, ...(pick ? {} : { needsVar: "decision" }) };
}

/** The `goto match` statement of a template, one line per element. */
export function choiceLines(t: ChoiceTemplate): string[] {
  return [
    `goto match ${t.subject} {`,
    ...t.arms.map(([p, to]) => `${IND}${p} -> ${to ?? 'fail("TODO: pick a target step")'}`),
    `${IND}_ -> ${thenText(FAIL, t.subject)}`,
    `}`,
  ];
}

/** A pattern for a new rule to `target`: an uncovered variant, else one named after the step. */
export function nextPattern(decls: Declaration[], wf: Workflow, c: Choice, target: string): string {
  const used = new Set(c.arms.map((a) => a.pattern));
  const free = subjectPatterns(decls, wf, c.subject)?.find((p) => !used.has(p));
  if (free) return free;
  const base = target === FAIL ? "other" : target.toLowerCase();
  for (let i = 1; ; i++) {
    const p = `"${base}${i > 1 ? i : ""}"`;
    if (!used.has(p)) return p;
  }
}

function choiceOf(doc: Doc, wfName: string, step: string): { wf: Workflow; n: GraphNode; c: Choice } {
  const wf = doc.workflow(wfName);
  const n = doc.node(wf, step);
  if (!n.choice) throw new EditError(`${step} has no goto match`);
  return { wf, n, c: n.choice };
}

function armAt(c: Choice, i: number): Arm {
  const a = c.arms[i];
  if (!a) throw new EditError("that rule no longer exists");
  return a;
}

export function setSubject(doc: Doc, wfName: string, step: string, expr: string): EditResult {
  const { c } = choiceOf(doc, wfName, step);
  const v = expr.trim();
  if (!v) throw new EditError("a choice needs a value to branch on");
  const [start, end] = doc.span(c.subjectSpan);
  const edits = [{ start, end, text: v }];
  // Keep a generated default's message naming the value.
  const d = c.arms.find((a) => a.wildcard && a.fail);
  if (d && SIMPLE.test(c.subject) && doc.text(d.thenSpan) === thenText(FAIL, c.subject)) {
    const [s, e] = doc.span(d.thenSpan);
    edits.push({ start: s, end: e, text: thenText(FAIL, v) });
  }
  return { edits };
}

export function setArmPattern(doc: Doc, wfName: string, step: string, i: number, pattern: string): EditResult {
  const { c } = choiceOf(doc, wfName, step);
  const a = armAt(c, i);
  const p = pattern.trim();
  if (!p) throw new EditError("a rule needs a value to match — remove it instead");
  if (p === "_" && c.arms.some((x, j) => j !== i && x.wildcard)) throw new EditError("there is already a default rule (_)");
  const [start, end] = doc.span(a.patternSpan);
  return { edits: [{ start, end, text: p }] };
}

/** Points rule `i` at a step, or makes it fail. */
export function setArmTarget(doc: Doc, wfName: string, step: string, i: number, target: string): EditResult {
  const { c } = choiceOf(doc, wfName, step);
  const a = armAt(c, i);
  if (target === FAIL ? a.fail : a.target === target) return { edits: [] };
  const [start, end] = doc.span(a.thenSpan);
  return { edits: [{ start, end, text: thenText(target, c.subject) }] };
}

/** Adds `pattern -> target`, before the default rule (or as the default, for `_`). */
export function addArm(doc: Doc, wfName: string, step: string, pattern: string, target: string): EditResult {
  const { c } = choiceOf(doc, wfName, step);
  const p = pattern.trim();
  if (!p) throw new EditError("a rule needs a value to match");
  const def = c.arms.find((a) => a.wildcard);
  if (p === "_" && def) throw new EditError("there is already a default rule (_)");
  if (c.arms.some((a) => a.pattern === p)) throw new EditError(`${p} already has a rule`);
  const text = `${p} -> ${thenText(target, c.subject)}`;
  const anchor = doc.at(p !== "_" && def ? def.span[0] : c.close);
  const first = c.arms[0];
  const indent = first && doc.startsLine(doc.at(first.span[0])) ? doc.indentAt(doc.at(first.span[0])) : doc.indentAt(doc.at(c.span[0])) + IND;
  if (doc.startsLine(anchor)) {
    const ls = doc.lineStart(anchor);
    return { edits: [{ start: ls, end: ls, text: `${indent}${text}\n` }] };
  }
  return { edits: [{ start: anchor, end: anchor, text: p !== "_" && def ? `${text} ` : ` ${text} ` }] };
}

export function removeArm(doc: Doc, wfName: string, step: string, i: number): EditResult {
  const { c } = choiceOf(doc, wfName, step);
  const a = armAt(c, i);
  if (c.arms.length === 1) throw new EditError("a choice needs at least one rule — delete the step instead");
  const [s, e] = doc.span(a.span);
  if (doc.startsLine(s) && doc.endsLine(e)) {
    return { edits: [{ start: doc.lineStart(s), end: Math.min(doc.lineEnd(e) + 1, doc.src.length), text: "" }] };
  }
  let end = e;
  while (/[ \t,]/.test(doc.src[end] ?? "")) end++;
  return { edits: [{ start: s, end, text: "" }] };
}
