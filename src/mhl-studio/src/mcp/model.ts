// The shapes `mhl serve mcp` replies with (src/mhl-runtime/internal/mcpserver),
// as far as the test bench reads them.

export interface McpTool {
  name: string;
  description?: string;
  inputSchema?: unknown;
  outputSchema?: unknown;
  /** HTTP only: `_meta["mhl.run"]` marks a workflow `run/start` can run asynchronously. */
  _meta?: Record<string, unknown>;
}

export interface McpResource {
  uri: string;
  name?: string;
  description?: string;
  mimeType?: string;
}

/** `initialize`'s result. */
export interface ServerHello {
  protocolVersion?: string;
  serverInfo?: { name?: string; version?: string };
  capabilities?: Record<string, unknown> & { experimental?: Record<string, unknown> };
}

/** A content block of a CallToolResult. */
export interface ContentBlock {
  type: string;
  text?: string;
  uri?: string;
  name?: string;
  [k: string]: unknown;
}

export interface CallToolResult {
  content?: ContentBlock[];
  isError?: boolean;
  structuredContent?: unknown;
  _meta?: Record<string, unknown>;
}

/** What a tools/call reply says about a run that stopped at pause() (`_meta["mhl.run"]`). */
export interface PausedCall {
  reason: string;
  step?: string;
  hint?: string;
  /** Stopped on entering a stage whose inputs were not given. */
  awaitingInputs?: AwaitingInputs;
  /** Stopped at a typed gate, waiting for a decision. */
  awaiting?: AwaitingDecision;
}

/**
 * The typed gate a paused run waits at (`awaiting`, execsvc.Awaiting) —
 * `pause(reason, decision: T)` in `step`; run/resume answers it with a
 * `decision` of type T, whose JSON Schema is `schema`.
 */
export interface AwaitingDecision {
  step: string;
  index: number;
  type: string;
  schema?: Record<string, unknown>;
}

function awaitingOf(v: unknown): AwaitingDecision | undefined {
  const a = v as Partial<AwaitingDecision> | undefined;
  if (!a || typeof a.type !== "string" || typeof a.step !== "string") return undefined;
  const schema = a.schema && typeof a.schema === "object" ? (a.schema as Record<string, unknown>) : undefined;
  return { step: a.step, index: typeof a.index === "number" ? a.index : 0, type: a.type, schema };
}

/**
 * The stage inputs a paused run waits for (`awaiting_inputs`, execsvc.AwaitingInputs):
 * it stopped before `step`, on entering `stage`; run/resume must carry `inputs`
 * in its arguments. `schema` is them as one object schema.
 */
export interface AwaitingInputs {
  stage: string;
  step: string;
  inputs: string[];
  schema?: unknown;
}

function awaitingInputsOf(v: unknown): AwaitingInputs | undefined {
  const a = v as Partial<AwaitingInputs> | undefined;
  if (!a || typeof a.stage !== "string" || !Array.isArray(a.inputs)) return undefined;
  return { stage: a.stage, step: String(a.step ?? ""), inputs: a.inputs.map(String), schema: a.schema };
}

/** A run/* reply's status object (mcpserver runView). */
export interface RunStatus {
  runId: string;
  tool: string;
  state: RunState;
  startedAt?: string;
  step?: string;
  /** The named stage `step` belongs to (absent outside any). */
  stage?: string;
  stepIndex?: number;
  stepTotal?: number;
  reached?: string[];
  resumable?: boolean;
  vars?: Record<string, unknown>;
  /** A paused run's `pause(reason)`. */
  reason?: string;
  /** The stage a paused run waits to be given inputs for. */
  awaiting_inputs?: AwaitingInputs;
  /** The typed gate a paused run waits at: run/resume needs a `decision`. */
  awaiting?: AwaitingDecision;
  error?: string;
}

export type RunState = "pending" | "claimed" | "queued" | "working" | "paused" | "completed" | "failed" | "canceled";

/** The run may still change on its own, so it is worth polling. */
export function isActive(state: RunState | undefined): boolean {
  return state === "pending" || state === "claimed" || state === "queued" || state === "working";
}

/** The mhl_run_* control tools: HTTP's synthetic tools/list entries that bridge to run/*. */
export function isControlTool(name: string): boolean {
  return name.startsWith("mhl_run_");
}

/** The workflow can be started with run/start (HTTP only). */
export function supportsAsync(t: McpTool): boolean {
  return !!t._meta?.["mhl.run"];
}

/** Where a run of a workflow may suspend for a human (`_meta["mhl.run"].pauses`). */
export interface PausePoints {
  /** Steps that call pause(...) — a decision gate or a plain pause. */
  steps: string[];
  /** Stages whose inputs have no default: the run stops on entering them to ask. */
  stages: string[];
}

/**
 * Where the tool's run may pause, as the server lists it; null when it never
 * does (or the server does not say — over stdio, where only tools/call exists).
 */
export function pausePoints(t: McpTool): PausePoints | null {
  const p = (t._meta?.["mhl.run"] as { pauses?: Partial<PausePoints> } | undefined)?.pauses;
  if (!p) return null;
  return { steps: Array.isArray(p.steps) ? p.steps.map(String) : [], stages: Array.isArray(p.stages) ? p.stages.map(String) : [] };
}

/** The run/* family is advertised (HTTP only). */
export function hasRunMethods(hello: ServerHello | null): boolean {
  return !!hello?.capabilities?.experimental?.["mhl.run"];
}

/**
 * The pause a tools/call reply reports, or null. A paused run's reply is not
 * an error and carries the partial vars like a completed one; only
 * `_meta["mhl.run"].state` tells them apart (mcpserver pausedToolResult).
 */
export function pausedCall(result: unknown): PausedCall | null {
  const run = (result as CallToolResult | null)?._meta?.["mhl.run"] as { state?: string; reason?: unknown; step?: unknown; hint?: unknown; awaiting_inputs?: unknown; awaiting?: unknown } | undefined;
  if (run?.state !== "paused") return null;
  return {
    reason: typeof run.reason === "string" ? run.reason : JSON.stringify(run.reason ?? "paused"),
    step: typeof run.step === "string" ? run.step : undefined,
    hint: typeof run.hint === "string" ? run.hint : undefined,
    awaitingInputs: awaitingInputsOf(run.awaiting_inputs),
    awaiting: awaitingOf(run.awaiting),
  };
}

/**
 * The fields run/resume's form shows for a run: a run paused on entering a
 * stage waits for that stage's inputs — only those, each required; otherwise
 * every input, all optional (blank keeps the run's value).
 */
export function resumeFields<F extends { name: string; required: boolean }>(fields: F[], status: Pick<RunStatus, "state" | "awaiting_inputs"> | undefined): F[] {
  const waiting = status?.state === "paused" ? status.awaiting_inputs : undefined;
  if (waiting) return fields.filter((f) => waiting.inputs.includes(f.name)).map((f) => ({ ...f, required: true }));
  return fields.map((f) => ({ ...f, required: false }));
}

/**
 * How far along a run is, 0–100. A run that has stopped (completed, failed,
 * canceled) is a full bar. Otherwise it is the distinct steps reached over the
 * declared ones, not `stepIndex/stepTotal`: stepIndex is the current step's
 * place in declaration order, which says nothing about progress once a
 * workflow's goto jumps around (a run that ends at its 2nd declared step
 * would read 2/9). Capped below full until the run actually ends.
 */
export function runProgress(st: Pick<RunStatus, "state" | "reached" | "stepTotal">): number {
  if (st.state === "completed" || st.state === "failed" || st.state === "canceled") return 100;
  const done = new Set(st.reached ?? []).size;
  if (!st.stepTotal || !done) return isActive(st.state) ? 5 : 0;
  return Math.min(95, Math.max(5, Math.round((done / st.stepTotal) * 100)));
}
