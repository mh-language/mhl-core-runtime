import { useEffect, useRef } from "react";
import { activeModel, registerEditor, save } from "../app/controller";
import { useStore } from "../app/store";
import { OffsetMap } from "../sync/offsets";
import { monaco, prefersDark, THEME_DARK, THEME_LIGHT } from "./monaco";

/** The Monaco editor showing the active file's model. */
export function CodeEditor() {
  const host = useRef<HTMLDivElement>(null);
  const editor = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const activePath = useStore((s) => s.activePath);
  const graph = useStore((s) => s.graph);
  const selection = useStore((s) => s.selection);
  const workflow = useStore((s) => s.workflow);
  const graphSource = useStore((s) => s.graphSource);
  const decorations = useRef<monaco.editor.IEditorDecorationsCollection | null>(null);

  useEffect(() => {
    const e = monaco.editor.create(host.current!, {
      model: null,
      automaticLayout: true,
      theme: prefersDark() ? THEME_DARK : THEME_LIGHT,
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
    const mq = window.matchMedia?.("(prefers-color-scheme: dark)");
    const onScheme = () => monaco.editor.setTheme(mq.matches ? THEME_DARK : THEME_LIGHT);
    mq?.addEventListener("change", onScheme);
    return () => {
      mq?.removeEventListener("change", onScheme);
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

  return <div className="code-editor" ref={host} />;
}
