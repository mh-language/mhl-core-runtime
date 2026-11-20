// Keeping env files out of git: when the studio creates (or renames to) an
// env file, the workspace root's .gitignore gets a line for it — unless a
// line already covers it, or it is a committed template (.env.example).

import { isEnvName } from "./templates";

/** Env files meant to be committed: the documented keys, without values. */
const TEMPLATES = [".env.example", ".env.sample", ".env.template", ".env.dist"];

function baseOf(rel: string): string {
  return rel.split(/[\\/]/).pop()!;
}

/** Whether `rel` (workspace-relative) is an env file that must stay out of git. */
export function isSecretEnv(rel: string): boolean {
  return isEnvName(rel) && !TEMPLATES.includes(baseOf(rel).toLowerCase());
}

/** The .gitignore line for `rel`: `.env` (any folder) for a `.env`, else the anchored path. */
export function ignoreLine(rel: string): string {
  const norm = rel.replace(/\\/g, "/").replace(/^\/+/, "");
  return baseOf(norm) === ".env" && !norm.includes("/") ? ".env" : `/${norm}`;
}

/** The patterns, one of which, as a whole line, already ignores `rel`. */
function covering(rel: string): string[] {
  const norm = rel.replace(/\\/g, "/").replace(/^\/+/, "");
  const base = baseOf(norm);
  const out = [norm, `/${norm}`, ".env*", "*.env*"];
  if (!norm.includes("/")) out.push(base);
  if (base === ".env") out.push(".env", "*.env");
  if (base.startsWith(".env.")) out.push(".env.*");
  if (base.endsWith(".env")) out.push("*.env");
  return out;
}

/**
 * `.gitignore`'s new text with a line ignoring `rel`, or null when nothing
 * needs adding (already covered, or not a secret env file). `existing` is
 * the current text, null when there is no .gitignore yet.
 */
export function withEnvIgnored(existing: string | null, rel: string): string | null {
  if (!isSecretEnv(rel)) return null;
  const lines = (existing ?? "").split(/\r?\n/).map((l) => l.trim());
  const cover = covering(rel);
  if (lines.some((l) => cover.includes(l))) return null;
  const text = existing ?? "";
  const sep = text === "" || text.endsWith("\n") ? "" : "\n";
  const header = lines.some((l) => l === "# Secrets (added by mhl studio)") ? "" : `${text === "" ? "" : "\n"}# Secrets (added by mhl studio)\n`;
  return `${text}${sep}${header}${ignoreLine(rel)}\n`;
}
