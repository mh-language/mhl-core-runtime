// A workflow's header — `[internal] [partial] [loop] workflow Name(...): T [max N]` —
// edited in place: the `loop` prefix and the `max <N>` clause (sugar for
// `repeat { max_iterations: N }`, valid only on a loop). Each edit rewrites
// text inside Workflow.headerSpan, never the body.
import { EditError, type Doc, type EditResult } from "./edits";

/** A loop's iteration ceiling when one is switched on with none given. */
export const DEFAULT_MAX = 3;

const MAX_CLAUSE = /\s+max\s+\d+\s*$/;

function header(doc: Doc, wfName: string) {
  const wf = doc.workflow(wfName);
  const [start, end] = doc.span(wf.headerSpan);
  return { wf, start, text: doc.src.slice(start, end) };
}

/**
 * Makes `wfName` a `loop` workflow/pipeline (with `max <DEFAULT_MAX>`, so it
 * cannot run forever) or a plain one again (dropping its `max`, which only a
 * loop accepts).
 */
export function setLoop(doc: Doc, wfName: string, on: boolean): EditResult {
  const { wf, start, text } = header(doc, wfName);
  if (!!wf.loop === on) return { edits: [] };
  const kw = /\b(pipeline|workflow)\b/.exec(text);
  if (!kw) throw new EditError(`cannot find the ${wf.kind} keyword in ${wfName}'s header`);
  if (on) {
    const end = start + text.length;
    return {
      edits: [
        { start: start + kw.index, end: start + kw.index, text: "loop " },
        { start: end, end, text: ` max ${DEFAULT_MAX}` },
      ],
    };
  }
  const loop = /\bloop\s+/.exec(text.slice(0, kw.index));
  const edits = loop ? [{ start: start + loop.index, end: start + loop.index + loop[0].length, text: "" }] : [];
  const max = MAX_CLAUSE.exec(text);
  if (max) edits.push({ start: start + max.index, end: start + text.length, text: "" });
  return { edits };
}

/** Sets a loop's `max <N>` clause; "" removes it (the loop then needs stop_when or break). */
export function setMax(doc: Doc, wfName: string, value: string): EditResult {
  const { wf, start, text } = header(doc, wfName);
  if (!wf.loop) throw new EditError(`max only applies to a loop ${wf.kind} — turn Loop on first`);
  const n = value.replace(/\D/g, "").replace(/^0+/, "");
  const max = MAX_CLAUSE.exec(text);
  const end = start + text.length;
  if (max) return { edits: [{ start: start + max.index, end, text: n ? ` max ${n}` : "" }] };
  return { edits: n ? [{ start: end, end, text: ` max ${n}` }] : [] };
}
