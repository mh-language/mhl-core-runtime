// A read-only JSON viewer in the style of a code editor: line numbers,
// syntax colours, folding. Strings that hold JSON (json.ts `Embedded`) are
// shown decoded, marked with a "JSON" chip that switches back to the
// original string.
import { useMemo, useState } from "react";
import { ChevronDown, ChevronRight, Copy, ChevronsDownUp, ChevronsUpDown } from "lucide-react";
import { ICON } from "../lib/icons";
import { Embedded, plain } from "./json";

type TokKind = "key" | "str" | "num" | "bool" | "null" | "punct" | "muted";
type Tok = { k: TokKind; s: string } | { k: "chip"; s: string; path: string };

interface Line {
  depth: number;
  toks: Tok[];
  /** A foldable node's path. */
  fold?: string;
  collapsed?: boolean;
}

/** Arrays this long start folded. */
const BIG = 200;

function isCollection(v: unknown): v is unknown[] | Record<string, unknown> {
  return !!v && typeof v === "object" && !(v instanceof Embedded);
}

function primitive(v: unknown): Tok {
  if (v === null || v === undefined) return { k: "null", s: "null" };
  if (typeof v === "string") return { k: "str", s: JSON.stringify(v) };
  if (typeof v === "number") return { k: "num", s: String(v) };
  if (typeof v === "boolean") return { k: "bool", s: String(v) };
  return { k: "str", s: JSON.stringify(v) };
}

/** The lines `v` renders as, given which paths are folded and which embedded strings show raw. */
export function flatten(v: unknown, folded: Set<string>, raw: Set<string>): { lines: Line[]; paths: string[] } {
  const lines: Line[] = [];
  const paths: string[] = [];
  const walk = (val: unknown, path: string, depth: number, prefix: Tok[], comma: boolean) => {
    const tail: Tok[] = comma ? [{ k: "punct", s: "," }] : [];
    if (val instanceof Embedded) {
      if (raw.has(path)) {
        lines.push({ depth, toks: [...prefix, { k: "chip", s: "text", path }, primitive(val.source), ...tail] });
        return;
      }
      prefix = [...prefix, { k: "chip", s: val.kind === "jsonl" ? "JSONL" : "JSON", path }];
      val = val.value;
    }
    if (!isCollection(val)) {
      lines.push({ depth, toks: [...prefix, primitive(val), ...tail] });
      return;
    }
    const arr = Array.isArray(val);
    const entries: [string | null, unknown][] = Array.isArray(val) ? val.map((x: unknown) => [null, x]) : Object.entries(val);
    const [open, close] = arr ? ["[", "]"] : ["{", "}"];
    if (entries.length === 0) {
      lines.push({ depth, toks: [...prefix, { k: "punct", s: open + close }, ...tail] });
      return;
    }
    paths.push(path);
    const isFolded = folded.has(path) !== (arr && entries.length > BIG);
    if (isFolded) {
      const n = `${entries.length} ${arr ? (entries.length === 1 ? "item" : "items") : entries.length === 1 ? "key" : "keys"}`;
      lines.push({ depth, fold: path, collapsed: true, toks: [...prefix, { k: "punct", s: open }, { k: "muted", s: ` … ${n} ` }, { k: "punct", s: close }, ...tail] });
      return;
    }
    lines.push({ depth, fold: path, toks: [...prefix, { k: "punct", s: open }] });
    entries.forEach(([key, x], i) => {
      const kp: Tok[] = key === null ? [] : [{ k: "key", s: JSON.stringify(key) }, { k: "punct", s: ": " }];
      walk(x, `${path}/${key ?? i}`, depth + 1, kp, i < entries.length - 1);
    });
    lines.push({ depth, toks: [{ k: "punct", s: close }, ...tail] });
  };
  walk(v, "$", 0, [], false);
  return { lines, paths };
}

export function JsonView({ value, maxHeight }: { value: unknown; maxHeight?: number | string }) {
  const [folded, setFolded] = useState<Set<string>>(new Set());
  const [raw, setRaw] = useState<Set<string>>(new Set());
  const { lines, paths } = useMemo(() => flatten(value, folded, raw), [value, folded, raw]);
  const toggle = (set: Set<string>, path: string, update: (s: Set<string>) => void) => {
    const next = new Set(set);
    if (next.has(path)) next.delete(path);
    else next.add(path);
    update(next);
  };
  // The paths with no folding applied: what "collapse all" folds.
  const allPaths = useMemo(() => flatten(value, new Set(), raw).paths, [value, raw]);
  const width = String(lines.length).length;
  return (
    <div className="jv" style={{ maxHeight }}>
      <div className="jv-tools">
        <button className="icon-btn" title="Expand all" onClick={() => setFolded(new Set())}>
          <ChevronsUpDown {...ICON} size={14} />
        </button>
        <button className="icon-btn" title="Collapse all" onClick={() => setFolded(new Set(allPaths.filter((p) => p !== "$")))}>
          <ChevronsDownUp {...ICON} size={14} />
        </button>
        <button className="icon-btn" title="Copy the JSON (embedded strings decoded)" onClick={() => void navigator.clipboard?.writeText(JSON.stringify(plain(value), null, 2))}>
          <Copy {...ICON} size={14} />
        </button>
      </div>
      <div className="jv-lines" data-paths={paths.length}>
        {lines.map((l, i) => (
          <div key={i} className="jv-row">
            <span className="jv-ln" style={{ width: `${width + 1}ch` }}>
              {i + 1}
            </span>
            <span className="jv-fold">
              {l.fold && (
                <button className="jv-caret" aria-label={l.collapsed ? "Expand" : "Collapse"} onClick={() => toggle(folded, l.fold!, setFolded)}>
                  {l.collapsed ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
                </button>
              )}
            </span>
            <span className="jv-code" style={{ paddingLeft: `${l.depth * 2}ch` }}>
              {l.toks.map((t, j) =>
                t.k === "chip" ? (
                  <button
                    key={j}
                    className={`jv-chip ${t.s === "text" ? "raw" : ""}`}
                    title={
                      t.s === "JSON"
                        ? "This string holds JSON, shown decoded — click for the original string"
                        : t.s === "JSONL"
                          ? "This string holds JSON Lines (one document per line), shown decoded as an array — click for the original string"
                          : "Show decoded"
                    }
                    onClick={() => toggle(raw, t.path, setRaw)}
                  >
                    {t.s === "JSON" ? "JSON string" : t.s === "JSONL" ? "JSON lines" : "decode"}
                  </button>
                ) : (
                  <span key={j} className={`jv-${t.k}`}>
                    {t.s}
                  </span>
                ),
              )}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
