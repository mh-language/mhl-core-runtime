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
