import { describe, expect, it } from "vitest";
import { flatten } from "./JsonView";
import { decodeEmbedded } from "./json";

const text = (v: unknown, folded: string[] = [], raw: string[] = []) =>
  flatten(v, new Set(folded), new Set(raw)).lines.map((l) => "  ".repeat(l.depth) + l.toks.map((t) => (t.k === "chip" ? `[${t.s}] ` : t.s)).join(""));

describe("flatten", () => {
  it("lays JSON out one value per line, with commas", () => {
    expect(text({ a: 1, b: [true, null], c: {} })).toEqual(["{", '  "a": 1,', '  "b": [', "    true,", "    null", "  ],", '  "c": {}', "}"]);
  });

  it("folds a node to one line", () => {
    expect(text({ a: { x: 1, y: 2 }, b: [1] }, ["$/a"])).toEqual(["{", '  "a": { … 2 keys },', '  "b": [', "    1", "  ]", "}"]);
  });

  it("marks a decoded JSON string, and shows the original when asked", () => {
    const v = decodeEmbedded({ reply: '{"ok":true}' });
    expect(text(v)).toEqual(["{", '  "reply": [JSON] {', '    "ok": true', "  }", "}"]);
    expect(text(v, [], ["$/reply"])).toEqual(["{", '  "reply": [text] "{\\"ok\\":true}"', "}"]);
  });

  it("starts a very long array folded, and unfolds it on request", () => {
    const big = Array.from({ length: 300 }, (_, i) => i);
    expect(text(big)).toEqual(["[ … 300 items ]"]);
    expect(text(big, ["$"])).toHaveLength(302);
  });
});
