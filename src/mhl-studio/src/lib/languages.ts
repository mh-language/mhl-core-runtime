// The editor language of a workspace file that is not mhl, Markdown, JSON or
// an env file (those have their own): by name, else by extension, else by a
// `#!` line — `agilespec` with `#!/usr/bin/env python3` is Python. "plaintext"
// when nothing says.

/** The languages highlighted besides mhl, Markdown, JSON and dotenv (shiki grammars). */
export const EXTRA_LANGS = ["python", "shellscript", "yaml", "toml", "javascript", "typescript", "css", "ini", "xml", "dockerfile", "make", "sql"] as const;
export type ExtraLang = (typeof EXTRA_LANGS)[number];

const BY_NAME: Record<string, ExtraLang> = { Dockerfile: "dockerfile", Makefile: "make", makefile: "make", GNUmakefile: "make" };

const BY_EXT: Record<string, ExtraLang> = {
  py: "python",
  pyi: "python",
  sh: "shellscript",
  bash: "shellscript",
  zsh: "shellscript",
  yml: "yaml",
  yaml: "yaml",
  toml: "toml",
  js: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  ts: "typescript",
  mts: "typescript",
  cts: "typescript",
  css: "css",
  ini: "ini",
  cfg: "ini",
  gitignore: "ini",
  gitattributes: "ini",
  xml: "xml",
  svg: "xml",
  sql: "sql",
  mk: "make",
};

const SHEBANG: [RegExp, ExtraLang][] = [
  [/\bpython[0-9.]*\b/, "python"],
  [/\b(ba|z|da|k)?sh\b/, "shellscript"],
  [/\b(node|deno|bun)\b/, "javascript"],
];

export function languageOf(path: string, text = ""): ExtraLang | "plaintext" {
  const name = path.split(/[\\/]/).pop() ?? path;
  if (BY_NAME[name]) return BY_NAME[name];
  if (name.startsWith("Dockerfile.")) return "dockerfile";
  const dot = name.lastIndexOf(".");
  // `.gitignore` is all extension; `x.tar.gz` takes its last one.
  const ext = dot >= 0 ? name.slice(dot + 1).toLowerCase() : "";
  if (ext && BY_EXT[ext]) return BY_EXT[ext];
  const first = text.slice(0, text.indexOf("\n") >>> 0);
  if (first.startsWith("#!")) for (const [re, lang] of SHEBANG) if (re.test(first)) return lang;
  return "plaintext";
}
