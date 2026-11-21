import { create } from "zustand";
import type { Declaration, Graph, Inspection, RunEvent, RunResult } from "../lib/types";
import { isMhPath } from "../lib/mhfile";
import { isEnvName } from "../sync/templates";

export type View = "split" | "design" | "code";

/** The main area: the workflow editor, or the MCP test bench (src/mcp). */
export type Mode = "editor" | "mcp";

/** The color theme the user picked (app/theme.ts): follow the OS, or force one. */
export type ThemePref = "system" | "light" | "dark";
/** The scheme actually shown. */
export type Scheme = "light" | "dark";

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
  /** Its imports, resolved to absolute "/"-separated paths (app/fragments.ts). */
  imports: string[];
  /** The partial workflows whose `entry` fragment it declares. */
  entries: string[];
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
  /** The stage whose own diagram is shown (canvas/layout.ts StageView.focus); null: the whole workflow. */
  stageFocus: string | null;
  /** The whole workflow shows every stage's steps, not one card per stage. */
  stagesExpanded: boolean;
  selection: string | null;
  /** The step a dragged tool method / agent would be dropped into. */
  dropTarget: string | null;
  /** What is dragged over `dropTarget`: a call to add, or a prompt/schema for its agent call. */
  dropKind: "call" | "prompt" | "schema";
  /** The .mh file being dragged from the explorer (absolute path), which the Imports card accepts. */
  dragFile: string | null;
  /** Imported names the diagram highlights the steps of (the imports card). */
  importFocus: string[] | null;
  /** Every workspace file's declarations (the open file included). */
  library: LibraryFile[];
  /** The tool (and method) open in the inspector; exclusive with `selection`. */
  tool: { name: string; method: string | null } | null;
  view: View;
  mode: Mode;
  toast: Toast | null;
  mhlVersion: string;
  inspection: Inspection | null;
  run: RunState;
  /** Values typed into the run panel's input form, by input name. */
  runInputs: Record<string, string>;
  runTab: RunTab;
  bottomOpen: boolean;
  /** The left side bar (the files) is shown. */
  leftOpen: boolean;
  /** The right side bar (inspector) is shown. */
  rightOpen: boolean;
  /** Agent CLIs found on PATH, by command (null until looked up). */
  clis: Record<string, string> | null;
  /** The run inputs popover (under the Run button's ▾) is open. */
  inputsOpen: boolean;
  theme: ThemePref;
  /** `theme` resolved against the OS. */
  scheme: Scheme;
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
  stageFocus: null,
  stagesExpanded: false,
  selection: null,
  library: [],
  dropTarget: null,
  dropKind: "call",
  dragFile: null,
  importFocus: null,
  tool: null,
  view: "split",
  mode: "editor",
  toast: null,
  mhlVersion: "",
  inspection: null,
  run: emptyRun(),
  runInputs: {},
  runTab: "execution",
  bottomOpen: true,
  leftOpen: true,
  rightOpen: true,
  inputsOpen: false,
  clis: null,
  theme: "system",
  scheme: "light",
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
  // Another workflow is drawn whole.
  if (s.workflow !== prev.workflow && s.stageFocus) useStore.setState({ stageFocus: null });
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

/**
 * The variants an input of `type` can take, when that type is an enum this
 * file declares or imports (`decls`); null for any other type. An optional
 * `T?` counts as `T`. The runtime takes the bare variant name (`All`, not
 * `AgentModel.All`) as an `--input` value and refuses anything else.
 */
export function enumVariants(type: string, decls: Declaration[]): string[] | null {
  const name = type.endsWith("?") ? type.slice(0, -1) : type;
  const d = decls.find((x) => x.kind === "enum" && x.name === name);
  return d?.variants?.length ? d.variants : null;
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

/** An env file `mhl run` loads (`.env` at the workspace root by default; others via --env-file): edited as text. */
export function isEnv(path: string | null | undefined): boolean {
  return !!path && isEnvName(path);
}

/** An mhl source file (`.mh`, or its `.mhl` alias). */
export function isMh(path: string | null | undefined): boolean {
  return !!path && isMhPath(path);
}
