import { create } from "zustand";
import type { Declaration, Graph, Inspection, RunEvent, RunResult } from "../lib/types";

export type View = "split" | "design" | "code";

/** A step's state in the execution view. */
export type NodeRunStatus = "running" | "succeeded" | "failed" | "paused" | "skipped";

export interface RunState {
  id: number | null;
  running: boolean;
  /** The workflow file the run belongs to. */
  path: string | null;
  session: string | null;
  events: RunEvent[];
  logs: string[];
  statuses: Record<string, NodeRunStatus>;
  /** Steps in the order they started — the path the run took. */
  visits: string[];
  result: RunResult | null;
  exitCode: number | null | undefined;
}

export const emptyRun = (): RunState => ({
  id: null,
  running: false,
  path: null,
  session: null,
  events: [],
  logs: [],
  statuses: {},
  visits: [],
  result: null,
  exitCode: undefined,
});

/** The run panel's tabs. */
export type RunTab = "execution" | "events" | "logs" | "variables";

export interface Toast {
  kind: "error" | "info";
  text: string;
}

/** What another workspace file declares, for importing into the open one. */
export interface LibraryFile {
  /** Path relative to the workspace root, "/"-separated. */
  rel: string;
  path: string;
  ok: boolean;
  declarations: Declaration[];
}

export interface State {
  root: string | null;
  /** The workspace's `.mh` and `.md` files, relative and "/"-separated. */
  files: string[];
  /** Its folders, empty ones included. */
  dirs: string[];
  /** Absolute path of the open file. */
  activePath: string | null;
  dirty: Record<string, boolean>;
  /** The last graph that parsed (kept while the text has errors). */
  graph: Graph | null;
  /** The text `graph` was built from. */
  graphSource: string;
  /** The latest text does not parse — the canvas shows `graph` read-only. */
  stale: boolean;
  diagnostics: Graph["diagnostics"];
  workflow: string | null;
  selection: string | null;
  /** The step a dragged tool method / agent would be dropped into. */
  dropTarget: string | null;
  /** What is dragged over `dropTarget`: a call to add, or a prompt/schema for its agent call. */
  dropKind: "call" | "prompt" | "schema";
  /** Every workspace file's declarations (the open file included). */
  library: LibraryFile[];
  /** The tool (and method) open in the inspector; exclusive with `selection`. */
  tool: { name: string; method: string | null } | null;
  view: View;
  toast: Toast | null;
  mhlVersion: string;
  inspection: Inspection | null;
  run: RunState;
  /** Values typed into the run panel's input form, by input name. */
  runInputs: Record<string, string>;
  runTab: RunTab;
  bottomOpen: boolean;
  /** The left side bar (files + toolbox) is shown. */
  leftOpen: boolean;
  /** The right side bar (inspector) is shown. */
  rightOpen: boolean;
  /** Agent CLIs found on PATH, by command (null until looked up). */
  clis: Record<string, string> | null;
  /** The run inputs popover (under the Run button's ▾) is open. */
  inputsOpen: boolean;
  /** Imported names the diagram highlights the steps of (the imports card). */
  importFocus: string[] | null;
}

export const useStore = create<State>(() => ({
  root: null,
  files: [],
  dirs: [],
  activePath: null,
  dirty: {},
  graph: null,
  graphSource: "",
  stale: false,
  diagnostics: [],
  workflow: null,
  selection: null,
  library: [],
  dropTarget: null,
  dropKind: "call",
  tool: null,
  view: "split",
  toast: null,
  mhlVersion: "",
  inspection: null,
  run: emptyRun(),
  runInputs: {},
  runTab: "execution",
  bottomOpen: true,
  leftOpen: true,
  rightOpen: true,
  importFocus: null,
  inputsOpen: false,
  clis: null,
}));

export const set = useStore.setState;
export const get = useStore.getState;

export function currentWorkflow(s: State) {
  return s.graph?.workflows.find((w) => w.name === s.workflow) ?? null;
}

// Selecting a step closes the tool inspector (and vice versa: selectTool
// clears `selection`).
useStore.subscribe((s, prev) => {
  if (s.selection && s.selection !== prev.selection && s.tool) useStore.setState({ tool: null });
});

export function selectTool(name: string, method: string | null = null) {
  useStore.setState({ tool: { name, method }, selection: null });
}

/** A run input the form shows: name, type and declared default. */
export interface InputField {
  name: string;
  type: string;
  def?: string;
}

/**
 * The run form's inputs: the dry-run's view when there is one (it also
 * covers a typed signature's fields), else the graph's.
 */
export function inputFields(s: State): InputField[] {
  const wf = currentWorkflow(s);
  if (s.inspection?.inputs?.length) {
    return s.inspection.inputs.map((i) => ({ name: i.name, type: i.type, def: wf?.inputs.find((x) => x.name === i.name)?.default }));
  }
  return (wf?.inputs ?? []).map((i) => ({ name: i.name, type: i.type, def: i.default }));
}

/** The filled-in inputs, as `--input` pairs (blank ones fall back to their defaults). */
export function runInputPairs(s: State = useStore.getState()): [string, string][] {
  return inputFields(s)
    .filter((f) => s.runInputs[f.name] !== undefined && s.runInputs[f.name] !== "")
    .map((f) => [f.name, s.runInputs[f.name]]);
}

/**
 * The open file declares no workflow or pipeline: there is nothing to draw
 * or run, so the diagram, the states bar and the run panel are hidden and
 * the code editor takes their place. Unknown (false) until the file is graphed.
 * A Markdown or JSON file never has one.
 */
export function hasNoWorkflow(s: State): boolean {
  return !!s.activePath && (!isMh(s.activePath) || (!!s.graph && s.graph.workflows.length === 0));
}

/** A Markdown file (a prompt body, a skill, notes): edited as text, never graphed. */
export function isMarkdown(path: string | null | undefined): boolean {
  return !!path && /\.md$/i.test(path);
}

/** A JSON file (a JSON Schema for `schema X from "x.json"`, or other data): edited as text. */
export function isJson(path: string | null | undefined): boolean {
  return !!path && /\.json$/i.test(path);
}

/** An mhl source file. */
export function isMh(path: string | null | undefined): boolean {
  return !!path && path.endsWith(".mh");
}
