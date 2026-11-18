// Making a reply readable. A workflow's vars often hold an agent's raw
// output — a JSON document as a string, sometimes several (stream-json
// lines). `decodeEmbedded` turns such strings back into values, wrapped in
// `Embedded` so the viewer can mark them and show the original string.

/**
 * A string that held JSON, decoded: one object or array ("json"), or
 * several, one per line ("jsonl" — `value` is then the array of them).
 */
export class Embedded {
  constructor(
    readonly value: unknown,
    readonly source: string,
    readonly kind: "json" | "jsonl" = "json",
  ) {}
}

/** The JSON a string holds, when it is a whole object or array; else undefined. */
export function parseEmbedded(s: string): unknown {
  const t = s.trim();
  if (t.length < 2) return undefined;
  const open = t[0];
  const close = t[t.length - 1];
  if (!((open === "{" && close === "}") || (open === "[" && close === "]"))) return undefined;
  try {
    return JSON.parse(t);
  } catch {
    return undefined;
  }
}

/**
 * The documents a JSON Lines string holds (two or more lines, each a whole
 * object or array; blank lines skipped) — an agent's stream-json output.
 * Undefined when any line isn't one.
 */
export function parseJsonLines(s: string): unknown[] | undefined {
  const lines = s.split(/\r?\n/).filter((l) => l.trim() !== "");
  if (lines.length < 2) return undefined;
  const docs: unknown[] = [];
  for (const l of lines) {
    const doc = parseEmbedded(l);
    if (doc === undefined) return undefined;
    docs.push(doc);
  }
  return docs;
}

/** `v` with every string holding JSON decoded (recursively, so JSON inside decoded JSON too). */
export function decodeEmbedded(v: unknown, depth = 0): unknown {
  if (depth > 32) return v;
  if (typeof v === "string") {
    const inner = parseEmbedded(v);
    if (inner !== undefined) return new Embedded(decodeEmbedded(inner, depth + 1), v);
    const docs = parseJsonLines(v);
    return docs === undefined ? v : new Embedded(decodeEmbedded(docs, depth + 1), v, "jsonl");
  }
  if (Array.isArray(v)) return v.map((x) => decodeEmbedded(x, depth + 1));
  if (v && typeof v === "object") return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, decodeEmbedded(x, depth + 1)]));
  return v;
}

/** The value without its `Embedded` wrappers. */
export function plain(v: unknown): unknown {
  if (v instanceof Embedded) return plain(v.value);
  if (Array.isArray(v)) return v.map(plain);
  if (v && typeof v === "object") return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, plain(x)]));
  return v;
}

/** UTF-8 size of `v` as JSON, for "13.9 KB". */
export function byteSize(v: unknown): number {
  const text = typeof v === "string" ? v : (JSON.stringify(v) ?? "");
  return new TextEncoder().encode(text).length;
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(2)} KB`;
  return `${(n / 1024 / 1024).toFixed(2)} MB`;
}

export interface Table {
  columns: string[];
  rows: unknown[][];
  /** The rows are an object's fields (key/value), not array items. */
  keyed: boolean;
}

/**
 * The value as a table, when it reads as one: an array of objects (a
 * column per key, first seen first) or a plain object (a row per field).
 * Null for anything else.
 */
export function asTable(v: unknown): Table | null {
  v = v instanceof Embedded ? v.value : v;
  if (Array.isArray(v)) {
    const items = v.map((x) => (x instanceof Embedded ? x.value : x));
    if (!items.length || !items.every((x) => x && typeof x === "object" && !Array.isArray(x))) return null;
    const columns: string[] = [];
    for (const it of items) for (const k of Object.keys(it as object)) if (!columns.includes(k)) columns.push(k);
    return { columns, rows: items.map((it) => columns.map((c) => (it as Record<string, unknown>)[c])), keyed: false };
  }
  if (v && typeof v === "object") {
    return { columns: ["key", "value"], rows: Object.entries(v).map(([k, x]) => [k, x]), keyed: true };
  }
  return null;
}

/** One table cell: a primitive as is, anything else as compact JSON. */
export function cellText(v: unknown): string {
  v = plain(v);
  if (v === undefined) return "";
  if (typeof v === "string") return v;
  return JSON.stringify(v);
}
