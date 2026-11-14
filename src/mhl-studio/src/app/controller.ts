// Everything that changes state outside React: files, Monaco models, the
// language server, graph refreshes, canvas edits and runs.
//
// The sync rule: a file's Monaco model is the only source of truth. Typing
// in the editor schedules `mhl graph` on the new text; a canvas action
// computes TextEdits from the current graph (sync/edits.ts), pushes them
// onto the model as one undoable step, and regraphs immediately.
import { ask, open as openDialog } from "@tauri-apps/plugin-dialog";
import { api, errorText, fileUri, joinPath } from "../lib/api";
import type { Declaration, Graph, RunEvent, RunResult, Span, StepStatus } from "../lib/types";
import { applyEdits, Doc, type EditResult, type TextEdit } from "../sync/edits";
import { exportDeclaration, resolveImport } from "../sync/imports";
import { moverFor, relinkEdits } from "../sync/relink";
import { declNameFor, newFilePath, newFileSource, uniqueName, type NewFileKind } from "../sync/templates";
import { agentName, BACKENDS, renderAgent, type Backend, type Companion, type Role } from "../sync/agentTemplates";
import { declaredNames, insertBlock } from "../sync/prompts";
import { LspClient } from "../editor/lsp";
import { JSON_LANG, LANG, MD_LANG, monaco, setupMonaco } from "../editor/monaco";
import { emptyRun, get, isJson, isMarkdown, isMh, selectTool, set, type LibraryFile, type NodeRunStatus, type Toast } from "./store";
import { installZoom } from "./zoom";
import { installPanels } from "./panels";

const models = new Map<string, monaco.editor.ITextModel>();
let lsp: LspClient | null = null;
let graphSeq = 0;
let graphTimer: ReturnType<typeof setTimeout> | undefined;
let toastTimer: ReturnType<typeof setTimeout> | undefined;
const LAST_FOLDER = "mhl-studio.lastFolder";

/** The code editor registers itself so the canvas can reveal source. */
let editorInstance: monaco.editor.IStandaloneCodeEditor | null = null;
export function registerEditor(e: monaco.editor.IStandaloneCodeEditor | null) {
  editorInstance = e;
}

export function toast(kind: Toast["kind"], text: string) {
  set({ toast: { kind, text } });
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => set({ toast: null }), kind === "error" ? 7000 : 3500);
}

export async function init() {
  await setupMonaco();
  installPanels();
  installZoom((z) => toast("info", `Zoom ${Math.round(z * 100)}%`));
  api
    .mhlVersion()
    .then((v) => set({ mhlVersion: v }))
    .catch((e) => toast("error", errorText(e)));
  let last: string | null = null;
  try {
    last = localStorage.getItem(LAST_FOLDER);
  } catch {
    /* storage unavailable */
  }
  if (last) await loadFolder(last).catch(() => undefined);
}

export async function openFolder() {
  const dir = await openDialog({ directory: true, multiple: false, title: "Open a folder of .mh workflows" });
  if (typeof dir === "string") await loadFolder(dir);
}

export async function loadFolder(root: string) {
  const { files, dirs } = await api.listFiles(root);
  for (const m of models.values()) m.dispose();
  models.clear();
  await lsp?.dispose();
  lsp = null;
  set({ root, files, dirs, activePath: null, dirty: {}, graph: null, graphSource: "", stale: false, diagnostics: [], workflow: null, selection: null, tool: null, library: [], run: emptyRun(), inspection: null });
  try {
    localStorage.setItem(LAST_FOLDER, root);
  } catch {
    /* storage unavailable */
  }
  LspClient.create(root)
    .then((c) => {
      lsp = c;
      for (const [path, m] of models) if (isMh(path)) c.didOpen(m);
    })
    .catch((e) => toast("error", `language server: ${errorText(e)}`));
  const mh = files.filter(isMh);
  const first = mh.find((f) => !f.includes("/")) ?? mh[0];
  if (first) await openFile(joinPath(root, first));
  void refreshLibrary();
}

export async function refreshFiles() {
  const { root } = get();
  if (root) set(await api.listFiles(root));
  void refreshLibrary();
}

// ---------------------------------------------------------------- library

let librarySeq = 0;

function setLibraryFile(rel: string, path: string, g: { ok: boolean; declarations: Declaration[] }) {
  set((s) => {
    const entry: LibraryFile = { rel, path, ok: g.ok, declarations: g.ok ? g.declarations : (s.library.find((l) => l.path === path)?.declarations ?? []) };
    const rest = s.library.filter((l) => l.path !== path);
    return { library: [...rest, entry].sort((a, b) => a.rel.localeCompare(b.rel)) };
  });
}

/** Graphs every workspace file (its open buffer, else the saved text) for the import browser. */
export async function refreshLibrary() {
  const { root, files } = get();
  if (!root) return;
  const seq = ++librarySeq;
  const out: LibraryFile[] = [];
  for (const rel of files.filter(isMh)) {
    const path = joinPath(root, rel);
    try {
      const src = models.get(path)?.getValue() ?? (await api.readFile(path));
      const g = await api.graph(path, src);
      out.push({ rel, path, ok: g.ok, declarations: g.declarations });
    } catch {
      out.push({ rel, path, ok: false, declarations: [] });
    }
    if (seq !== librarySeq) return;
  }
  set({ library: out });
}

/** Declarations the open file imports from other workspace files. */
export function importedDeclarations(graph: Graph | null = get().graph, activePath = get().activePath): Declaration[] {
  if (!graph || !activePath) return [];
  const out: Declaration[] = [];
  for (const imp of graph.imports) {
    const target = resolveImport(activePath, imp.from);
    const file = get().library.find((l) => l.path.replace(/\\/g, "/") === target);
    if (!file) continue;
    for (const d of file.declarations) if (imp.names.length === 0 || imp.names.includes(d.name)) out.push(d);
  }
  return out;
}

/**
 * Makes `name`, declared in another workspace file, usable in the open file:
 * marks it `export` in its own file when needed (saving that file — it must
 * not have unsaved edits), then runs `then` — typically an addImport plus
 * whatever uses the name — as one edit of the open file.
 */
export async function useFromFile(file: LibraryFile, name: string, then: (doc: Doc) => EditResult): Promise<boolean> {
  const decl = file.declarations.find((d) => d.name === name);
  if (!decl) return false;
  if (!decl.exported) {
    if (get().dirty[file.path]) {
      toast("error", `Save ${file.rel} first: ${name} has to be marked export there.`);
      return false;
    }
    try {
      const model = models.get(file.path);
      const src = model?.getValue() ?? (await api.readFile(file.path));
      const g = await api.graph(file.path, src);
      if (!g.ok) throw new Error(`${file.rel} has errors`);
      const r = exportDeclaration(new Doc(src, g), name);
      const next = applyEdits(src, r.edits);
      if (model) {
        applyToModel(model, r.edits);
        await save(file.path);
      } else {
        await api.writeFile(file.path, next);
      }
      setLibraryFile(file.rel, file.path, await api.graph(file.path, next));
      toast("info", `Exported ${name} from ${file.rel}`);
    } catch (e) {
      toast("error", `Could not export ${name} from ${file.rel}: ${errorText(e)}`);
      return false;
    }
  }
  return edit(then);
}

/** `path` relative to `root`, "/"-separated. */
function relPath(root: string, path: string): string {
  const r = root.replace(/\\/g, "/").replace(/\/+$/, "") + "/";
  const p = path.replace(/\\/g, "/");
  return p.startsWith(r) ? p.slice(r.length) : p;
}

/** A file's text: its open buffer, else what is saved. */
export async function readText(path: string): Promise<string> {
  return models.get(path)?.getValue() ?? (await api.readFile(path));
}

export function activeModel(): monaco.editor.ITextModel | null {
  const p = get().activePath;
  return p ? (models.get(p) ?? null) : null;
}

/** The editor buffer of `path`, holding `text`, registered with the language server. */
function createModel(path: string, text: string): monaco.editor.ITextModel {
  const mh = isMh(path);
  const lang = mh ? LANG : isMarkdown(path) ? MD_LANG : isJson(path) ? JSON_LANG : "plaintext";
  const model = monaco.editor.createModel(text, lang, monaco.Uri.parse(fileUri(path)));
  model.updateOptions(mh ? { tabSize: 4, insertSpaces: true } : { tabSize: 2, insertSpaces: true });
  models.set(path, model);
  model.onDidChangeContent(() => onModelChange(path));
  if (mh) lsp?.didOpen(model);
  return model;
}

/** Drops the editor buffer of `path` (its unsaved text with it). */
function disposeModel(path: string) {
  const model = models.get(path);
  if (!model) return;
  if (isMh(path)) lsp?.didClose(model);
  model.dispose();
  models.delete(path);
}

export async function openFile(path: string) {
  let model = models.get(path);
  if (!model) model = createModel(path, await api.readFile(path));
  set({ activePath: path, selection: null, tool: null, graph: null, graphSource: "", stale: false, diagnostics: [], workflow: null, inspection: null, importFocus: null });
  // Markdown and JSON have no graph: the code view shows them, the diagram stays away.
  if (isMh(path)) await refreshGraph();
}

function onModelChange(path: string) {
  const model = models.get(path);
  if (!model) return;
  set((s) => ({ dirty: { ...s.dirty, [path]: true } }));
  if (!isMh(path)) return;
  lsp?.didChange(model);
  if (path === get().activePath) {
    clearTimeout(graphTimer);
    graphTimer = setTimeout(() => void refreshGraph(), 250);
  }
}

/** Rebuilds the graph from the active model's current text. */
export async function refreshGraph(): Promise<void> {
  clearTimeout(graphTimer);
  const path = get().activePath;
  const model = activeModel();
  if (!path || !model || !isMh(path)) return;
  const text = model.getValue();
  const seq = ++graphSeq;
  try {
    const g = await api.graph(path, text);
    if (seq !== graphSeq || get().activePath !== path) return;
    if (g.ok) {
      const keep = g.workflows.some((w) => w.name === get().workflow);
      const workflow = keep ? get().workflow : (g.workflows[0]?.name ?? null);
      const wf = g.workflows.find((w) => w.name === workflow);
      const selection = wf?.nodes.some((n) => n.id === get().selection) ? get().selection : null;
      const t = get().tool;
      const tool = t && g.declarations.some((d) => d.kind === "tool" && d.name === t.name) ? t : null;
      set({ graph: g, graphSource: text, stale: false, diagnostics: g.diagnostics, workflow, selection, tool });
      const root = get().root;
      if (root) setLibraryFile(relPath(root, path), path, g);
    } else {
      set({ stale: true, diagnostics: g.diagnostics });
    }
  } catch (e) {
    if (seq === graphSeq) toast("error", errorText(e));
  }
}

function applyToModel(model: monaco.editor.ITextModel, edits: TextEdit[]) {
  model.pushStackElement();
  model.pushEditOperations(
    [],
    edits.map((e) => ({
      range: monaco.Range.fromPositions(model.getPositionAt(e.start), model.getPositionAt(e.end)),
      text: e.text,
    })),
    () => null,
  );
  model.pushStackElement();
}

/**
 * Runs a canvas/inspector action: builds a Doc from the current text and its
 * graph (regraphing first if the text moved on), applies the resulting edits
 * to the model and regraphs. Returns false when the action was refused.
 */
export async function edit(action: (doc: Doc) => EditResult): Promise<boolean> {
  const model = activeModel();
  if (!model) return false;
  if (get().graphSource !== model.getValue() || get().stale) await refreshGraph();
  const { graph, graphSource, stale } = get();
  if (!graph || stale || graphSource !== model.getValue()) {
    toast("error", "The code has errors — fix them in the code view before editing the diagram.");
    return false;
  }
  let result: EditResult;
  try {
    result = action(new Doc(graphSource, graph, importedDeclarations(graph)));
  } catch (e) {
    toast("error", errorText(e));
    return false;
  }
  if (result.edits.length === 0) return true;
  applyToModel(model, result.edits);
  await refreshGraph();
  if (result.select) set({ selection: result.select });
  return true;
}

export function undo() {
  void activeModel()?.undo();
}

export function redo() {
  void activeModel()?.redo();
}

/** Selects and shows a byte span of the active file in the code editor. */
export function revealSpan(span: Span) {
  const model = activeModel();
  const { graphSource } = get();
  if (!model || !editorInstance) return;
  const doc = new Doc(graphSource, get().graph!);
  const [a, b] = doc.span(span);
  const range = monaco.Range.fromPositions(model.getPositionAt(a), model.getPositionAt(b));
  if (get().view === "design") set({ view: "split" });
  editorInstance.setSelection(range);
  editorInstance.revealRangeInCenter(range);
  editorInstance.focus();
}

export async function save(path = get().activePath): Promise<void> {
  if (!path || !get().dirty[path]) return;
  const model = models.get(path);
  if (!model) return;
  await api.writeFile(path, model.getValue());
  set((s) => ({ dirty: { ...s.dirty, [path]: false } }));
}

/**
 * Creates `relName` (".mh", or ".prompt.md" for a Markdown prompt,
 * appended when missing) holding a new declaration of `kind`, named after
 * the file, and opens it.
 */
export async function newFile(relName: string, kind: NewFileKind = "workflow") {
  const { root } = get();
  if (!root) return;
  const typed = relName.trim().replace(/^[\\/]+/, "");
  if (!typed) return;
  const rel = newFilePath(typed, kind);
  const name = declNameFor(rel, kind === "workflow" ? "Workflow" : "Main");
  const path = joinPath(root, rel);
  try {
    await api.createFile(path, newFileSource(kind, name));
  } catch (e) {
    toast("error", errorText(e));
    return;
  }
  await refreshFiles();
  await openFile(path);
  if (kind === "tool") selectTool(name);
}

/** Looks up which agent CLIs are on PATH (cached; `force` looks again). */
export async function loadClis(force = false): Promise<Record<string, string>> {
  const known = get().clis;
  if (known && !force) return known;
  try {
    const clis = await api.findClis(BACKENDS.map((b) => b.command));
    set({ clis });
    return clis;
  } catch {
    return known ?? {};
  }
}

/** Writes a template's companion files into `dir` (relative), keeping any that already exist. */
async function writeCompanions(root: string, dir: string, companions: Companion[]): Promise<void> {
  for (const c of companions) {
    try {
      await api.createFile(joinPath(root, dir ? `${dir}/${c.name}` : c.name), c.content);
    } catch {
      /* already there: the user may have tuned it */
    }
  }
}

/** Creates `relName` (".mh" added when missing) holding a ready-made agent, plus its companion files, and opens it. */
export async function newAgentFile(relName: string, backend: Backend, role: Role) {
  const { root } = get();
  const typed = relName.trim().replace(/^[\\/]+/, "");
  if (!root || !typed) return;
  const rel = newFilePath(typed, "agent");
  const dir = rel.includes("/") ? rel.slice(0, rel.lastIndexOf("/")) : "";
  const t = renderAgent(backend, role, { dir });
  const path = joinPath(root, rel);
  try {
    await api.createFile(path, t.source + "\n");
  } catch (e) {
    toast("error", errorText(e));
    return;
  }
  await writeCompanions(root, dir, t.companions);
  await refreshFiles();
  await openFile(path);
}

/** Adds a ready-made agent to the open file (plus its companion files next to it). */
export async function insertAgentTemplate(backend: Backend, role: Role): Promise<boolean> {
  const { root, activePath } = get();
  if (!root || !activePath || !isMh(activePath)) return false;
  const rel = relPath(root, activePath);
  const dir = rel.includes("/") ? rel.slice(0, rel.lastIndexOf("/")) : "";
  let companions: Companion[] = [];
  let added = "";
  const ok = await edit((doc) => {
    added = uniqueName(agentName(backend, role), declaredNames(doc));
    const t = renderAgent(backend, role, { name: added, dir });
    companions = t.companions;
    return insertBlock(doc, "agent", t.source);
  });
  if (!ok) return false;
  await writeCompanions(root, dir, companions);
  if (companions.length) await refreshFiles();
  toast("info", `Added agent ${added}`);
  return true;
}

/** Creates the folder `relName` (and any missing parents) in the workspace. */
export async function newFolder(relName: string): Promise<boolean> {
  const { root } = get();
  const rel = relName.trim().replace(/^[\\/]+|[\\/]+$/g, "");
  if (!root || !rel) return false;
  try {
    await api.createDir(joinPath(root, rel));
  } catch (e) {
    toast("error", errorText(e));
    return false;
  }
  await refreshFiles();
  return true;
}

/** Whether `path` is `target` or inside folder `target` ("/"-normalized compare). */
function within(path: string, target: string): boolean {
  const p = path.replace(/\\/g, "/");
  const t = target.replace(/\\/g, "/").replace(/\/+$/, "");
  return p === t || p.startsWith(`${t}/`);
}

/**
 * Moves or renames a workspace file or folder (relative paths), keeping the
 * program working: every `import ... from` and `prompt ... from` path that
 * pointed at a moved file — and a moved `.mh` file's own relative paths —
 * is rewritten. An open buffer moves with its file, unsaved edits included.
 * A file that does not parse is left alone (it is reported).
 */
export async function movePath(fromRel: string, toRel: string): Promise<boolean> {
  const { root, files } = get();
  const to = toRel.trim().replace(/^[\\/]+|[\\/]+$/g, "");
  if (!root || !to || to === fromRel) return false;
  const fromAbs = joinPath(root, fromRel);
  const toAbs = joinPath(root, to);
  const move = moverFor(fromAbs, toAbs);
  const native = (p: string) => joinPath(root, relPath(root, move(p)));

  // 1. Plan the reference updates on today's text, before anything moves.
  const plans: { path: string; text: string; edits: TextEdit[] }[] = [];
  const skipped: string[] = [];
  for (const rel of files.filter(isMh)) {
    const path = joinPath(root, rel);
    try {
      const text = await readText(path);
      const g = await api.graph(path, text);
      if (!g.ok) {
        skipped.push(rel);
        continue;
      }
      const edits = relinkEdits(new Doc(text, g), path, move);
      if (edits.length) plans.push({ path, text, edits });
    } catch {
      skipped.push(rel);
    }
  }

  // 2. Move it on disk.
  try {
    await api.renamePath(fromAbs, toAbs);
  } catch (e) {
    toast("error", errorText(e));
    return false;
  }

  // 3. Open buffers follow their files.
  const dirty = { ...get().dirty };
  let activePath = get().activePath;
  for (const [path, model] of [...models]) {
    if (!within(path, fromAbs)) continue;
    const next = native(path);
    const text = model.getValue();
    const wasDirty = !!dirty[path];
    disposeModel(path);
    createModel(next, text);
    delete dirty[path];
    if (wasDirty) dirty[next] = true;
    if (activePath === path) activePath = next;
  }
  set({ dirty, activePath });

  // 4. Rewrite the references: in an open buffer (saved unless it already
  // had unsaved edits), else straight on disk.
  for (const plan of plans) {
    const path = native(plan.path);
    const model = models.get(path);
    try {
      if (model) {
        const wasDirty = !!get().dirty[path];
        applyToModel(model, plan.edits);
        if (!wasDirty) {
          set((s) => ({ dirty: { ...s.dirty, [path]: true } }));
          await save(path);
        }
      } else {
        await api.writeFile(path, applyEdits(plan.text, plan.edits));
      }
    } catch (e) {
      toast("error", `Could not update the paths in ${relPath(root, path)}: ${errorText(e)}`);
    }
  }

  await refreshFiles();
  if (get().activePath) await refreshGraph();
  const updated = plans.length ? ` — updated paths in ${plans.length} file${plans.length > 1 ? "s" : ""}` : "";
  const warn = skipped.length ? `. Not checked (they have errors): ${skipped.join(", ")}` : "";
  toast(skipped.length ? "error" : "info", `Moved ${fromRel} → ${to}${updated}${warn}`);
  return true;
}

/** Moves a workspace file or folder (relative path) to the Trash, after asking. */
export async function deletePath(rel: string, isDir: boolean): Promise<boolean> {
  const { root } = get();
  if (!root) return false;
  const abs = joinPath(root, rel);
  const unsaved = Object.entries(get().dirty).filter(([p, d]) => d && within(p, abs)).length;
  const what = isDir ? `the folder ${rel} and everything in it` : rel;
  const note = unsaved ? `\n\n${unsaved} file${unsaved > 1 ? "s have" : " has"} unsaved changes that will be lost.` : "";
  let ok = false;
  try {
    ok = await ask(`Move ${what} to the Trash?${note}`, { title: "mhl studio", kind: "warning", okLabel: "Move to Trash", cancelLabel: "Cancel" });
  } catch {
    ok = window.confirm(`Move ${what} to the Trash?${note}`);
  }
  if (!ok) return false;
  try {
    await api.deletePath(abs);
  } catch (e) {
    toast("error", errorText(e));
    return false;
  }
  const dirty = { ...get().dirty };
  for (const path of [...models.keys()]) {
    if (!within(path, abs)) continue;
    disposeModel(path);
    delete dirty[path];
  }
  const active = get().activePath;
  set({ dirty });
  await refreshFiles();
  if (active && within(active, abs)) {
    const next = get().files.filter(isMh)[0];
    if (next) await openFile(joinPath(root, next));
    else set({ activePath: null, graph: null, graphSource: "", stale: false, diagnostics: [], workflow: null, selection: null, tool: null, inspection: null });
  }
  toast("info", `Moved ${rel} to the Trash`);
  return true;
}

// ---------------------------------------------------------------- runs

/**
 * Saves the file and evaluates `expr` (a tool method call) against it — the
 * tool inspector's "Test method".
 */
export async function evaluate(expr: string): Promise<RunResult | null> {
  const { root, activePath } = get();
  if (!root || !activePath) return null;
  await save();
  try {
    return await api.evaluate(root, activePath, expr);
  } catch (e) {
    toast("error", errorText(e));
    return null;
  }
}

export async function inspect(inputs: [string, string][]) {
  const { root, activePath, workflow } = get();
  if (!root || !activePath) return;
  await save();
  try {
    set({ inspection: await api.inspect(root, activePath, workflow, inputs) });
  } catch (e) {
    toast("error", errorText(e));
  }
}

const STATUS: Record<StepStatus, NodeRunStatus> = {
  ok: "succeeded",
  goto: "succeeded",
  break: "succeeded",
  complete: "succeeded",
  pause: "paused",
  failed: "failed",
};

function onRunEvent(ev: RunEvent) {
  set((s) => {
    const run = { ...s.run, events: [...s.run.events, ev].slice(-5000) };
    switch (ev.type) {
      case "run_start":
        run.session = ev.session;
        break;
      case "step_start":
        run.statuses = { ...run.statuses, [ev.step]: "running" };
        run.visits = [...run.visits, ev.step];
        break;
      case "step_end":
        run.statuses = { ...run.statuses, [ev.step]: STATUS[ev.status] ?? "succeeded" };
        break;
      case "log":
      case "stdout":
      case "stderr":
        run.logs = [...run.logs, ev.text].slice(-5000);
        break;
      case "result": {
        const { type: _t, ...result } = ev;
        void _t;
        run.result = result;
        if (result.session) run.session = result.session;
        const statuses = { ...run.statuses };
        for (const step of result.skipped ?? []) if (!statuses[step]) statuses[step] = "skipped";
        if (!result.ok && result.step) statuses[result.step] = "failed";
        run.statuses = statuses;
        break;
      }
      case "exit":
        run.running = false;
        run.id = null;
        run.exitCode = ev.code;
        break;
    }
    return { run };
  });
}

export async function startRun(inputs: [string, string][], opts: { resume?: boolean; force?: boolean } = {}) {
  const { root, activePath, workflow, run: prev } = get();
  if (!root || !activePath || prev.running) return;
  await save();
  const session = opts.resume ? (prev.session ?? undefined) : undefined;
  // A resume continues the same execution: keep what it already showed
  // (statuses, path, history) instead of repainting finished steps "skipped".
  const base =
    opts.resume && prev.path === activePath
      ? { ...prev, id: null, result: null, exitCode: undefined }
      : { ...emptyRun(), path: activePath, session: session ?? null };
  set({ run: { ...base, running: true }, bottomOpen: true });
  try {
    const id = await api.runStart(
      { cwd: root, path: activePath, workflow, inputs, resume: !!opts.resume, force: !!opts.force, session },
      onRunEvent,
    );
    set((s) => ({ run: { ...s.run, id: s.run.running ? id : null } }));
  } catch (e) {
    set((s) => ({ run: { ...s.run, running: false } }));
    toast("error", errorText(e));
  }
}

export async function cancelRun(force = false) {
  const { id } = get().run;
  if (id == null) return;
  try {
    await api.runCancel(id, force);
  } catch (e) {
    toast("error", errorText(e));
  }
}

export function clearRun() {
  set({ run: emptyRun() });
}
