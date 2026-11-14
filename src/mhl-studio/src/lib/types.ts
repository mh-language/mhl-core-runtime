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
}

export interface Arm {
  pattern: string;
  target?: string;
  fail?: boolean;
  span: Span;
  targetSpan?: Span;
}

export interface Route {
  name: string;
  params: string[];
  span: Span;
  arms: Arm[];
}

export interface GraphNode {
  id: string;
  kind: "step" | "parallel";
  name: string;
  parent?: string;
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
  exits: string[];
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
  vars: { name: string; kind: string; span: Span }[];
  props: { name: string; span: Span }[];
  routes: Route[];
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
  inputs?: { name: string; type: string; supplied: boolean }[];
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
  broke?: boolean;
  break_reason?: unknown;
  vars?: Record<string, unknown>;
  error?: string;
  kind?: string;
  step?: string;
  hint?: string;
  missing?: string[];
}
