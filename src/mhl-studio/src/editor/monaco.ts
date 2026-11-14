// Monaco, bundled locally (no CDN — the app must work offline), with the
// `mhl` language: TextMate highlighting from vscode-mhl's grammar via Shiki,
// and brackets/comments from vscode-mhl's language-configuration.json.
import * as monaco from "./monaco-core.js";
import { createHighlighterCore } from "shiki/core";
import { createJavaScriptRegexEngine } from "shiki/engine/javascript";
import { shikiToMonaco } from "@shikijs/monaco";
import githubDark from "shiki/themes/github-dark-default.mjs";
import githubLight from "shiki/themes/github-light-default.mjs";
import grammar from "../../../../vscode-mhl/syntaxes/mhl.tmLanguage.json";
import langConf from "../../../../vscode-mhl/language-configuration.json";
import markdown from "shiki/langs/markdown.mjs";
import json from "shiki/langs/json.mjs";

self.MonacoEnvironment = {
  getWorker: () =>
    new Worker(new URL("../../node_modules/monaco-editor/esm/vs/editor/editor.worker.js", import.meta.url), { type: "module" }),
};

export const LANG = "mhl";
/** Markdown files: prompt bodies (`prompt X(...) from "x.md"`), skills, notes. */
export const MD_LANG = "markdown";
/** JSON files: JSON Schemas (`schema X from "x.json"`), data. */
export const JSON_LANG = "json";
export const THEME_DARK = "github-dark-default";
export const THEME_LIGHT = "github-light-default";

let ready: Promise<typeof monaco> | null = null;

/** Registers the mhl language once; resolves when highlighting is ready. */
export function setupMonaco(): Promise<typeof monaco> {
  if (ready) return ready;
  ready = (async () => {
    monaco.languages.register({ id: LANG, extensions: [".mh"], aliases: ["MHL", "mhl"] });
    monaco.languages.register({ id: MD_LANG, extensions: [".md"], aliases: ["Markdown"] });
    monaco.languages.register({ id: JSON_LANG, extensions: [".json"], aliases: ["JSON"] });
    monaco.languages.setLanguageConfiguration(JSON_LANG, {
      brackets: [["{", "}"], ["[", "]"]],
      autoClosingPairs: [{ open: "{", close: "}" }, { open: "[", close: "]" }, { open: '"', close: '"', notIn: ["string"] }],
      indentationRules: { increaseIndentPattern: /[{[]\s*$/, decreaseIndentPattern: /^\s*[}\]]/ },
    });
    monaco.languages.setLanguageConfiguration(LANG, {
      comments: { lineComment: langConf.comments.lineComment },
      brackets: langConf.brackets as [string, string][],
      autoClosingPairs: langConf.autoClosingPairs,
      surroundingPairs: langConf.surroundingPairs.map(([open, close]) => ({ open, close })),
      indentationRules: {
        increaseIndentPattern: /[{[(]\s*(\/\/.*)?$/,
        decreaseIndentPattern: /^\s*[}\])]/,
      },
    });
    const highlighter = await createHighlighterCore({
      themes: [githubDark, githubLight],
      langs: [{ ...(grammar as object), name: LANG } as never, ...markdown, ...json],
      engine: createJavaScriptRegexEngine({ forgiving: true }),
    });
    shikiToMonaco(highlighter, monaco);
    return monaco;
  })();
  return ready;
}

export function prefersDark(): boolean {
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? false;
}

export { monaco };
