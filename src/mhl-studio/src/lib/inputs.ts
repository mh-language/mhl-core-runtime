// A workflow's inputs as the run asks for them: what it needs to start, then
// each stage in run order (`Workflow.stages`) with the inputs it asks for on
// entering it (`WfInput.stage`).
import type { WfInput } from "./types";

export interface InputGroup {
  /** The stage the inputs are asked for on entering; undefined for the start of the run. */
  stage?: string;
  inputs: WfInput[];
}

/**
 * Start, then every stage in `stages` order — a stage asking for nothing
 * included, so the whole journey shows. A stage input whose stage is not in
 * `stages` (a stale graph) is appended under its own stage.
 */
export function inputJourney(inputs: WfInput[], stages: { name: string }[] = []): InputGroup[] {
  const out: InputGroup[] = [{ inputs: inputs.filter((i) => !i.stage) }];
  for (const st of stages) out.push({ stage: st.name, inputs: inputs.filter((i) => i.stage === st.name) });
  for (const i of inputs) {
    if (!i.stage || out.some((g) => g.stage === i.stage)) continue;
    out.push({ stage: i.stage, inputs: inputs.filter((x) => x.stage === i.stage) });
  }
  return out;
}

/** A required input: no default — at the start, the run is refused without it; in a stage, the run stops there to ask. */
export function needsValue(i: WfInput): boolean {
  return i.default === undefined;
}

/** Why a value typed for an input won't be accepted, and the value it most likely meant. */
export interface InputProblem {
  message: string;
  /** A corrected value to offer, e.g. `["python3","-m","pytest"]` for `python3 -m pytest`. */
  fix?: string;
}

/**
 * What the runtime would refuse in `raw` as an `--input` of `type`, before
 * the run is started: a number or bool that isn't one, and a list or object
 * (`T[]`, `{...}`) that isn't JSON of that shape — the runtime decodes those
 * from JSON (types.Coerce). A blank value, a string, an enum and a named type
 * the form can't see into are left to the runtime. A `string[]` given as
 * plain text is offered as its words, the argv a command line splits into.
 */
export function inputProblem(type: string, raw: string): InputProblem | null {
  const text = raw.trim();
  const t = type.replace(/\?$/, "");
  if (!text) return null;
  if (t === "number") return Number.isFinite(Number(text)) ? null : { message: "a number" };
  if (t === "bool") return text === "true" || text === "false" ? null : { message: "true or false" };
  const list = t.endsWith("[]");
  if (!list && !t.startsWith("{")) return null;
  let v: unknown;
  try {
    v = JSON.parse(text);
  } catch {
    if (t === "string[]") return { message: `a JSON list of strings, e.g. ${JSON.stringify(text.split(/\s+/))}`, fix: JSON.stringify(text.split(/\s+/)) };
    return { message: list ? "a JSON list, e.g. [1, 2]" : "a JSON object, e.g. {\"key\": \"value\"}" };
  }
  if (list && !Array.isArray(v)) return { message: "a JSON list", ...(t === "string[]" && typeof v === "string" ? { fix: JSON.stringify([v]) } : {}) };
  if (!list && (typeof v !== "object" || v === null || Array.isArray(v))) return { message: "a JSON object" };
  if (t === "string[]" && (v as unknown[]).some((x) => typeof x !== "string")) return { message: "a JSON list of strings" };
  return null;
}
