import { useEffect, useRef, useState } from "react";
import { activeModel, registerEditor, save } from "../app/controller";
import { importFile } from "../app/library";
import { get, set, useStore } from "../app/store";
import { PATH_MIME } from "../canvas/mime";
import { OffsetMap } from "../sync/offsets";
import { monaco, THEME_DARK, THEME_LIGHT } from "./monaco";

/**
 * The Monaco editor showing the active file's model. A .mh file dragged from
 * the explorer and dropped here is imported (app/library.ts's importFile, as
 * on the diagram's Imports card) instead of Monaco inserting text at the drop.
 */
export function CodeEditor() {
  const host = useRef<HTMLDivElement>(null);
  const editor = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const activePath = useStore((s) => s.activePath);
  const graph = useStore((s) => s.graph);
  const selection = useStore((s) => s.selection);
  const workflow = useStore((s) => s.workflow);
  const graphSource = useStore((s) => s.graphSource);
  const decorations = useRef<monaco.editor.IEditorDecorationsCollection | null>(null);
  const dragFile = useStore((s) => s.dragFile);
  const [over, setOver] = useState(false);

  useEffect(() => {
    const e = monaco.editor.create(host.current!, {
      model: null,
      automaticLayout: true,
      theme: get().scheme === "dark" ? THEME_DARK : THEME_LIGHT,
      fontSize: 13,
      minimap: { enabled: false },
      scrollBeyondLastLine: false,
      tabSize: 4,
      renderWhitespace: "selection",
      fixedOverflowWidgets: true,
    });
    e.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => void save());
    editor.current = e;
    decorations.current = e.createDecorationsCollection();
    registerEditor(e);

    // Capture phase on the host, so Monaco's own drop handling never sees
    // the explorer's file drag (only dragFile: another .mh file).
    const el = host.current!;
    const ours = (ev: DragEvent) => !!get().dragFile && !!ev.dataTransfer?.types.includes(PATH_MIME);
    const onOver = (ev: DragEvent) => {
      if (!ours(ev)) return;
      ev.preventDefault();
      ev.stopPropagation();
      ev.dataTransfer!.dropEffect = "copy";
      setOver(true);
    };
    const onLeave = (ev: DragEvent) => {
      if (!el.contains(ev.relatedTarget as Node | null)) setOver(false);
    };
    const onDrop = (ev: DragEvent) => {
      setOver(false);
      if (!ours(ev)) return;
      ev.preventDefault();
      ev.stopPropagation();
      const path = get().dragFile;
      set({ dragFile: null });
      const file = get().library.find((l) => l.path === path);
      if (file) void importFile(file);
    };
    el.addEventListener("dragover", onOver, true);
    el.addEventListener("dragleave", onLeave, true);
    el.addEventListener("drop", onDrop, true);

    // The theme button (app/theme.ts) or the OS changed the scheme.
    const offScheme = useStore.subscribe((s, prev) => {
      if (s.scheme !== prev.scheme) monaco.editor.setTheme(s.scheme === "dark" ? THEME_DARK : THEME_LIGHT);
    });
    return () => {
      offScheme();
      el.removeEventListener("dragover", onOver, true);
      el.removeEventListener("dragleave", onLeave, true);
      el.removeEventListener("drop", onDrop, true);
      registerEditor(null);
      e.dispose();
    };
  }, []);

  useEffect(() => {
    editor.current?.setModel(activeModel());
  }, [activePath]);

  // Highlight the selected step's source range.
  useEffect(() => {
    const e = editor.current;
    const model = activeModel();
    if (!e || !model || !graph) return;
    const wf = graph.workflows.find((w) => w.name === workflow);
    const node = wf?.nodes.find((n) => n.id === selection);
    if (!node || graphSource !== model.getValue()) {
      decorations.current?.clear();
      return;
    }
    const map = new OffsetMap(graphSource);
    const start = model.getPositionAt(map.toUtf16(node.span[0]));
    const end = model.getPositionAt(map.toUtf16(node.span[1]));
    decorations.current?.set([
      {
        range: new monaco.Range(start.lineNumber, 1, end.lineNumber, 1),
        options: { isWholeLine: true, className: "selected-step-line", linesDecorationsClassName: "selected-step-gutter" },
      },
    ]);
    e.revealRangeInCenterIfOutsideViewport(new monaco.Range(start.lineNumber, 1, end.lineNumber, 1));
  }, [selection, graph, workflow, graphSource]);

  useEffect(() => {
    if (!dragFile) setOver(false);
  }, [dragFile]);

  return (
    <>
      <div className="code-editor" ref={host} />
      {over && dragFile && <div className="code-drop-hint">Drop to import {dragFile.split(/[\\/]/).pop()}</div>}
    </>
  );
}
