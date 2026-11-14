// A small LSP client for `mhl lsp`, wired straight into Monaco's provider
// APIs. The server's feature set is small (diagnostics, completion, hover,
// signature help, definition), so this stays far lighter than pulling in
// the VS Code API stack monaco-languageclient needs. Transport: the Tauri
// backend frames messages over the sidecar's stdio (src-tauri/src/lsp.rs).
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import { api, fileUri } from "../lib/api";
import { LANG, monaco } from "./monaco";

type Json = Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any

interface LspPosition {
  line: number;
  character: number;
}
interface LspRange {
  start: LspPosition;
  end: LspPosition;
}

const toPos = (p: monaco.IPosition): LspPosition => ({ line: p.lineNumber - 1, character: p.column - 1 });
const toRange = (r: LspRange): monaco.IRange => ({
  startLineNumber: r.start.line + 1,
  startColumn: r.start.character + 1,
  endLineNumber: r.end.line + 1,
  endColumn: r.end.character + 1,
});

function markdown(contents: unknown): monaco.IMarkdownString[] {
  const one = (c: unknown): monaco.IMarkdownString =>
    typeof c === "string" ? { value: c } : { value: (c as Json).kind === "plaintext" ? (c as Json).value : ((c as Json).value ?? "") };
  return Array.isArray(contents) ? contents.map(one) : [one(contents)];
}

const COMPLETION_KIND: Record<number, monaco.languages.CompletionItemKind> = {
  2: monaco.languages.CompletionItemKind.Method,
  3: monaco.languages.CompletionItemKind.Function,
  5: monaco.languages.CompletionItemKind.Field,
  6: monaco.languages.CompletionItemKind.Variable,
  7: monaco.languages.CompletionItemKind.Class,
  9: monaco.languages.CompletionItemKind.Module,
  10: monaco.languages.CompletionItemKind.Property,
  13: monaco.languages.CompletionItemKind.Enum,
  14: monaco.languages.CompletionItemKind.Keyword,
  15: monaco.languages.CompletionItemKind.Snippet,
  21: monaco.languages.CompletionItemKind.Constant,
  22: monaco.languages.CompletionItemKind.Struct,
};

export class LspClient {
  private nextId = 1;
  private pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: Error) => void }>();
  private versions = new Map<string, number>();
  private unlisten: UnlistenFn[] = [];
  private disposables: monaco.IDisposable[] = [];
  private ready: Promise<void>;

  private constructor(private readonly root: string) {
    this.ready = this.start();
  }

  static async create(root: string): Promise<LspClient> {
    const c = new LspClient(root);
    await c.ready;
    return c;
  }

  private async start() {
    this.unlisten.push(await listen<string>("lsp://message", (e) => this.onMessage(e.payload)));
    await api.lspStart(this.root);
    await this.request("initialize", {
      processId: null,
      rootUri: fileUri(this.root),
      workspaceFolders: [{ uri: fileUri(this.root), name: "workspace" }],
      capabilities: {
        textDocument: {
          completion: { completionItem: { snippetSupport: true, documentationFormat: ["markdown", "plaintext"] } },
          hover: { contentFormat: ["markdown", "plaintext"] },
          signatureHelp: { signatureInformation: { documentationFormat: ["markdown", "plaintext"] } },
          publishDiagnostics: {},
        },
      },
    });
    this.notify("initialized", {});
    this.registerProviders();
  }

  private onMessage(raw: string) {
    let msg: Json;
    try {
      msg = JSON.parse(raw);
    } catch {
      return;
    }
    if (typeof msg.id === "number" && this.pending.has(msg.id) && !msg.method) {
      const p = this.pending.get(msg.id)!;
      this.pending.delete(msg.id);
      if (msg.error) p.reject(new Error(msg.error.message));
      else p.resolve(msg.result);
      return;
    }
    if (msg.method === "textDocument/publishDiagnostics") {
      const model = monaco.editor.getModel(monaco.Uri.parse(msg.params.uri));
      if (!model) return;
      monaco.editor.setModelMarkers(
        model,
        "mhl",
        (msg.params.diagnostics as Json[]).map((d) => ({
          ...toRange(d.range),
          message: d.message,
          severity: d.severity === 2 ? monaco.MarkerSeverity.Warning : monaco.MarkerSeverity.Error,
          source: "mhl",
        })),
      );
    }
  }

  request<T = unknown>(method: string, params: unknown): Promise<T> {
    const id = this.nextId++;
    return new Promise<T>((resolve, reject) => {
      this.pending.set(id, { resolve: resolve as (v: unknown) => void, reject });
      api.lspSend(JSON.stringify({ jsonrpc: "2.0", id, method, params })).catch((e) => {
        this.pending.delete(id);
        reject(e instanceof Error ? e : new Error(String(e)));
      });
    });
  }

  notify(method: string, params: unknown) {
    void api.lspSend(JSON.stringify({ jsonrpc: "2.0", method, params })).catch(() => undefined);
  }

  didOpen(model: monaco.editor.ITextModel) {
    const uri = model.uri.toString();
    this.versions.set(uri, 1);
    this.notify("textDocument/didOpen", { textDocument: { uri, languageId: LANG, version: 1, text: model.getValue() } });
  }

  didChange(model: monaco.editor.ITextModel) {
    const uri = model.uri.toString();
    const version = (this.versions.get(uri) ?? 1) + 1;
    this.versions.set(uri, version);
    this.notify("textDocument/didChange", { textDocument: { uri, version }, contentChanges: [{ text: model.getValue() }] });
  }

  didClose(model: monaco.editor.ITextModel) {
    this.versions.delete(model.uri.toString());
    this.notify("textDocument/didClose", { textDocument: { uri: model.uri.toString() } });
  }

  private registerProviders() {
    const doc = (model: monaco.editor.ITextModel, position: monaco.IPosition) => ({
      textDocument: { uri: model.uri.toString() },
      position: toPos(position),
    });
    this.disposables.push(
      monaco.languages.registerCompletionItemProvider(LANG, {
        triggerCharacters: [".", '"'],
        provideCompletionItems: async (model, position) => {
          const res = await this.request<Json | Json[] | null>("textDocument/completion", doc(model, position)).catch(() => null);
          const items = (Array.isArray(res) ? res : (res?.items ?? [])) as Json[];
          const word = model.getWordUntilPosition(position);
          const range = new monaco.Range(position.lineNumber, word.startColumn, position.lineNumber, word.endColumn);
          return {
            suggestions: items.map((it) => ({
              label: it.label,
              kind: COMPLETION_KIND[it.kind] ?? monaco.languages.CompletionItemKind.Text,
              detail: it.detail,
              documentation: it.documentation ? markdown(it.documentation)[0] : undefined,
              insertText: it.textEdit?.newText ?? it.insertText ?? it.label,
              insertTextRules: it.insertTextFormat === 2 ? monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet : undefined,
              range: it.textEdit?.range ? toRange(it.textEdit.range) : range,
              sortText: it.sortText,
              filterText: it.filterText,
            })),
          };
        },
      }),
      monaco.languages.registerHoverProvider(LANG, {
        provideHover: async (model, position) => {
          const res = await this.request<Json | null>("textDocument/hover", doc(model, position)).catch(() => null);
          if (!res?.contents) return null;
          return { contents: markdown(res.contents), range: res.range ? toRange(res.range) : undefined };
        },
      }),
      monaco.languages.registerSignatureHelpProvider(LANG, {
        signatureHelpTriggerCharacters: ["(", ","],
        provideSignatureHelp: async (model, position) => {
          const res = await this.request<Json | null>("textDocument/signatureHelp", doc(model, position)).catch(() => null);
          if (!res?.signatures?.length) return null;
          return {
            value: {
              signatures: (res.signatures as Json[]).map((s) => ({
                label: s.label,
                documentation: s.documentation ? markdown(s.documentation)[0] : undefined,
                parameters: ((s.parameters ?? []) as Json[]).map((p) => ({
                  label: p.label,
                  documentation: p.documentation ? markdown(p.documentation)[0] : undefined,
                })),
              })),
              activeSignature: res.activeSignature ?? 0,
              activeParameter: res.activeParameter ?? 0,
            },
            dispose: () => undefined,
          };
        },
      }),
      monaco.languages.registerDefinitionProvider(LANG, {
        provideDefinition: async (model, position) => {
          const res = await this.request<Json | Json[] | null>("textDocument/definition", doc(model, position)).catch(() => null);
          const locs = (Array.isArray(res) ? res : res ? [res] : []) as Json[];
          return locs.map((l) => ({ uri: monaco.Uri.parse(l.uri ?? l.targetUri), range: toRange(l.range ?? l.targetSelectionRange) }));
        },
      }),
    );
  }

  async dispose() {
    this.disposables.forEach((d) => d.dispose());
    this.unlisten.forEach((u) => u());
    this.pending.forEach((p) => p.reject(new Error("language server stopped")));
    this.pending.clear();
    await api.lspStop().catch(() => undefined);
  }
}
