// Mirrors of the JSON documents the vendored `mhl` produces.
// `mhl graph` → internal/lang/flow (src/mhl-runtime/internal/lang/flow/flow.go).
// Every Span is a half-open [start, end) range of UTF-8 *byte* offsets into
// the source the graph was built from — convert with sync/offsets.ts.

export type Span = [number, number];

export interface Diagnostic {
  file?: string;
  line: number;
  column: number;
  message: string;
  /** "warning" for a lint warning (advice, never blocking); absent for an error. */
  severity?: "warning";
}

export interface Declaration {
  kind: "agent" | "tool" | "router" | "memory" | "extension" | "prompt" | "skill" | "schema" | "type" | "enum" | "alias" | "test";
  name: string;
  span: Span;
  exported?: boolean;
  /** An extension's kind (mcp, a2a, ...); a prompt's or schema's `from` path; an agent's literal `command`. */
  detail?: string;
  inputs?: string[];
  /** A prompt's parameters a call must pass (those with no default). */
  params?: string[];
  /** An agent: where it puts a call's `schema:` ("args", "stdin", "none"; absent = unknown). */
  schema?: "args" | "stdin" | "none";
  /** A tool's methods, `internal` ones included. */
  methods?: ToolMethod[];
  /** An enum's variants, in order. */
  variants?: string[];
  /** A prompt's `for` variant patterns, in order (e.g. "claude/*"); the default arm is `detail`/the body. */
  targets?: string[];
  // Set for a tool only: its name, the braces around its members, and every
  // `Name.` call site in this file.
  nameSpan?: Span;
  bodyOpen?: number;
  bodyClose?: number;
  refs?: Span[];
}

/** One tool method parameter, `name: Type = default`. */
export interface MethodArg {
  name: string;
  type?: string;
  default?: string;
  span: Span;
}

export interface ToolMethod {
  name: string;
  params: string[];
  args: MethodArg[];
  returns?: string;
  internal?: boolean;
  span: Span;
  /** From `internal`/the name through the return type. */
  headerSpan: Span;
  nameSpan: Span;
  /** "expr": the expression after `->`; "block": `{` through `}`. */
  bodyKind: "expr" | "block";
  bodySpan: Span;
}

export interface WfInput {
  name: string;
  type: string;
  default?: string;
  span: Span;
  /** Declared in another fragment of a merged workflow (absolute path); `span` indexes that file. */
  file?: string;
  /** Declared in a `partial ... stage S` fragment: the run asks for it on entering stage S, not at the start. */
  stage?: string;
  /** Its `///` documentation, the lines right above it. */
  doc?: string;
}

export interface Arm {
  pattern: string;
  target?: string;
  fail?: boolean;
  wildcard?: boolean;
  span: Span;
  /** The pattern alone (`_` for the wildcard). */
  patternSpan: Span;
  /** What follows `->`: the target name, or the whole `fail(...)`. */
  thenSpan: Span;
  targetSpan?: Span;
}

/**
 * A parallel group's `merge (param) { ... }`: runs once after every branch,
 * as the step `stepName` ("<group>.merge") in run events, with param bound
 * to each branch's vars by step name.
 */
export interface Merge {
  stepName: string;
  /** "" for `merge { ... }` (no branches object). */
  param: string;
  paramSpan: Span;
  /** `merge` through the closing brace. */
  span: Span;
  bodyOpen: number;
  bodyClose: number;
  calls: string[];
  callSites: CallSite[];
}

/** A step's top-level `goto match subject { arms }` — a Choice state. */
export interface Choice {
  subject: string;
  subjectSpan: Span;
  /** `goto` through the closing `}`. */
  span: Span;
  /** The closing `}`. */
  close: number;
  arms: Arm[];
}

/** A fragment's header stage — `partial workflow X stage Name {` (flow.StageGroup): the fragment's steps, a named phase. */
export interface StageGroup {
  name: string;
  /** Its steps' and parallel groups' ids, in order. */
  members: string[];
  span: Span;
  headerSpan: Span;
  nameSpan: Span;
  bodyOpen: number;
  bodyClose: number;
  /** As WfInput.file. */
  file?: string;
}

export interface Route {
  name: string;
  /** `entry route`: the run starts by dispatching on it (the start edges are its arms). */
  entry?: boolean;
  params: string[];
  span: Span;
  arms: Arm[];
  /** As WfInput.file. */
  file?: string;
}

export interface GraphNode {
  id: string;
  kind: "step" | "parallel";
  name: string;
  parent?: string;
  /** Declared in another fragment of a merged workflow (absolute path): its spans, and its edges', index that file. */
  file?: string;
  /** The stage of the fragment a top-level step or parallel group belongs to. */
  stage?: string;
  children?: string[];
  entry?: boolean;
  timeout?: string;
  span: Span;
  headerSpan: Span;
  nameSpan: Span;
  timeoutSpan?: Span;
  bodyOpen: number;
  bodyClose: number;
  calls: string[];
  /** Every `Receiver.method(...)` call in the body, in source order. */
  callSites: CallSite[];
  /** The body's first top-level `goto match`, if any. */
  choice?: Choice;
  /** A parallel group's own `var`s (declared before its branches; live only while it runs). */
  vars?: { name: string; kind: string; span: Span }[];
  /** A parallel group's `merge (param) { ... }` block, if any. */
  merge?: Merge;
  exits: string[];
  /** The decision types of the step's gates — `pause(reason, decision: T)` — in source order. */
  decisions?: string[];
  fallsThrough: boolean;
}

export interface CallSite {
  receiver: string;
  method: string;
  /** The call's source, whitespace collapsed. */
  text: string;
  /** Receiver through the closing parenthesis. */
  span: Span;
  /** The named arguments (`name: value`), each with its value's span. */
  args?: { name: string; span: Span }[];
}

export type EdgeKind = "next" | "goto" | "match" | "route" | "end" | "fail";

export interface GraphEdge {
  from: string;
  to: string;
  kind: EdgeKind;
  label?: string;
  via?: string;
  span?: Span;
}

export interface Workflow {
  name: string;
  kind: "workflow" | "pipeline";
  loop?: boolean;
  partial?: boolean;
  /** Shown whole, from every fragment (`mhl graph --merged`): members with `file` come from another fragment. */
  merged?: boolean;
  internal?: boolean;
  max?: string;
  description?: string;
  param?: { name: string; type: string };
  returns?: string;
  span: Span;
  headerSpan: Span;
  nameSpan: Span;
  bodyOpen: number;
  bodyClose: number;
  inputs: WfInput[];
  /** `file`: declared in that other fragment (absolute path) — `span` indexes that file. */
  vars: { name: string; kind: string; span: Span; file?: string }[];
  props: { name: string; span: Span; file?: string }[];
  routes: Route[];
  /** The stages — each a fragment's header stage, a named phase; `goto Name` enters one at its first member. */
  stages?: StageGroup[];
  nodes: GraphNode[];
  edges: GraphEdge[];
  externals: string[];
}

export interface Graph {
  version: number;
  ok: boolean;
  file: string;
  diagnostics: Diagnostic[];
  /** `locals` parallels `names`: what each item is bound to here (its `as` alias, if any). */
  imports: { from: string; names: string[]; locals: string[]; span: Span }[];
  declarations: Declaration[];
  workflows: Workflow[];
}

/** A gate a paused run waits at (execsvc.Awaiting): `schema` is its decision type as JSON Schema. */
export interface Awaiting {
  step: string;
  index: number;
  type: string;
  schema?: Record<string, unknown>;
  /** What the program binds the decision to (`var approved = pause(...)`). */
  name?: string;
  /** The `///` doc above that statement: what the decision means. */
  description?: string;
}

/**
 * The stage inputs a paused run waits for (execsvc.AwaitingInputs): the run
 * stopped before `step`, on entering `stage`; resume it with `inputs` as `--input`s.
 */
export interface AwaitingInputs {
  stage: string;
  step: string;
  inputs: string[];
  schema?: Record<string, unknown>;
}

export const START = "$start";
export const END = "$end";
export const FAIL = "$fail";

// `mhl run --dry-run --format json` (execsvc.Inspection + cli wrapper).
export interface Inspection {
  ok: boolean;
  dry_run: true;
  error?: string;
  warnings?: string[];
  pipeline?: string;
  kind?: string;
  steps?: string[];
  inputs?: { name: string; type: string; supplied: boolean; stage?: string; doc?: string }[];
  missing_inputs?: string[];
  unknown_inputs?: string[];
  invalid_inputs?: string[];
}

// `mhl run --format jsonl` events (internal/cli/run_events.go), plus the
// studio backend's own stdout/stderr/exit wrappers (src-tauri/src/run.rs).
export type RunEvent =
  | { type: "run_start"; pipeline: string; session: string; steps: string[]; ts: string }
  | { type: "step_start"; step: string; index: number; total: number; ts: string }
  | {
      type: "step_end";
      step: string;
      index: number;
      status: StepStatus;
      target?: string;
      error?: string;
      ms: number;
      ts: string;
    }
  | { type: "log"; text: string; ts: string }
  | ({ type: "result" } & RunResult)
  | { type: "stdout" | "stderr"; text: string }
  | { type: "exit"; code: number | null };

export type StepStatus = "ok" | "failed" | "goto" | "pause" | "break" | "complete";

export interface RunResult {
  ok: boolean;
  pipeline?: string;
  session?: string;
  executed?: string[];
  skipped?: string[];
  resumed?: boolean;
  paused?: boolean;
  pause_reason?: unknown;
  /** The gate a paused run waits at — `pause(..., decision: T)`; resume it with a decision of T. */
  awaiting?: Awaiting;
  /** The stage a paused run waits to be given inputs for; resume it with only those. */
  awaiting_inputs?: AwaitingInputs;
  broke?: boolean;
  break_reason?: unknown;
  vars?: Record<string, unknown>;
  error?: string;
  kind?: string;
  step?: string;
  hint?: string;
  /** On an `invalid_inputs` failure of a resume: the stage the run is still paused at, waiting for its inputs. */
  stage?: string;
  missing?: string[];
  /** On `invalid_inputs`: the inputs given a value not of their declared type. */
  invalid?: string[];
  /** On `invalid_inputs` from a resume: nothing ran, the run can be resumed again. */
  resumable?: boolean;
}
