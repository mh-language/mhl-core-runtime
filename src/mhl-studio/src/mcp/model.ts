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
}

/** A run/* reply's status object (mcpserver runView). */
export interface RunStatus {
  runId: string;
  tool: string;
  state: RunState;
  startedAt?: string;
  step?: string;
  stepIndex?: number;
  stepTotal?: number;
  reached?: string[];
  resumable?: boolean;
  vars?: Record<string, unknown>;
  /** A paused run's `pause(reason)`. */
  reason?: string;
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
  const run = (result as CallToolResult | null)?._meta?.["mhl.run"] as { state?: string; reason?: unknown; step?: unknown; hint?: unknown } | undefined;
  if (run?.state !== "paused") return null;
  return {
    reason: typeof run.reason === "string" ? run.reason : JSON.stringify(run.reason ?? "paused"),
    step: typeof run.step === "string" ? run.step : undefined,
    hint: typeof run.hint === "string" ? run.hint : undefined,
  };
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
