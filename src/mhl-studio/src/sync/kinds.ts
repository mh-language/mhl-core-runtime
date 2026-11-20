// What a `pipeline` cannot do that a `workflow` can. A pipeline runs its
// steps in order: the runtime's lint rejects `goto` (and so `goto match`)
// and `route` outside a workflow (internal/lang/lint/checks.go). The studio
// asks here before offering an action, so it never writes code that lint
// would then refuse.
import { END, FAIL, type Workflow } from "../lib/types";
import type { StateKind } from "./templates";

/** Palette states whose code needs `goto`: a Choice is a `goto match`. */
const WORKFLOW_ONLY: ReadonlySet<StateKind> = new Set(["choice"]);

/** Whether `wf` takes `goto`, `goto match` and `route` — only a `workflow` does. */
export function allowsGoto(wf: Pick<Workflow, "kind">): boolean {
  return wf.kind === "workflow";
}

/** Whether the palette state `kind` can be added to `wf`. */
export function stateAllowed(wf: Pick<Workflow, "kind">, kind: StateKind): boolean {
  return allowsGoto(wf) || !WORKFLOW_ONLY.has(kind);
}

/**
 * Whether a transition to `to` can be drawn in `wf`: in a pipeline only to
 * End (`complete()`) or Fail (`fail(…)`), never to a step.
 */
export function transitionAllowed(wf: Pick<Workflow, "kind">, to: string): boolean {
  return allowsGoto(wf) || to === END || to === FAIL;
}

/** Why `stateAllowed` said no, for a tooltip or an error. */
export function workflowOnlyReason(wf: Pick<Workflow, "name">, what: string): string {
  return `${what} needs a workflow: pipeline ${wf.name} runs its steps in order, without goto. Change \`pipeline\` to \`workflow\` to branch.`;
}
