import { describe, expect, it } from "vitest";
import { asTable, byteSize, cellText, decodeEmbedded, Embedded, formatBytes, parseEmbedded, parseJsonLines, plain } from "./json";

describe("decodeEmbedded", () => {
  it("decodes strings that hold a JSON object or array, at any depth", () => {
    const reply = ['{"type":"result","usage":{"input_tokens":2}}', "plain text", "[1,2]"];
    const out = decodeEmbedded({ reply, n: 1 }) as { reply: unknown[]; n: number };
    expect(out.n).toBe(1);
    expect(out.reply[0]).toBeInstanceOf(Embedded);
    expect((out.reply[0] as Embedded).value).toEqual({ type: "result", usage: { input_tokens: 2 } });
    expect((out.reply[0] as Embedded).source).toBe(reply[0]);
    expect(out.reply[1]).toBe("plain text");
    expect((out.reply[2] as Embedded).value).toEqual([1, 2]);
  });

  it("decodes JSON inside decoded JSON", () => {
    const inner = JSON.stringify({ summary: "Pong" });
    const outer = JSON.stringify({ result: inner });
    const v = decodeEmbedded(outer) as Embedded;
    const result = (v.value as { result: Embedded }).result;
    expect(result).toBeInstanceOf(Embedded);
    expect(result.value).toEqual({ summary: "Pong" });
    expect(plain(v)).toEqual({ result: { summary: "Pong" } });
  });

  it("decodes JSON Lines (stream-json output) as an array, and the JSON inside it", () => {
    const text = JSON.stringify({ summary: "Pong" });
    const src = [
      JSON.stringify({ type: "thread.started", thread_id: "t1" }),
      JSON.stringify({ type: "item.completed", item: { type: "agent_message", text } }),
      "",
      JSON.stringify({ type: "turn.completed", usage: { output_tokens: 24 } }),
    ].join("\n");
    const v = decodeEmbedded({ reply: [src] }) as { reply: Embedded[] };
    const e = v.reply[0];
    expect(e).toBeInstanceOf(Embedded);
    expect(e.kind).toBe("jsonl");
    expect(e.source).toBe(src);
    const docs = e.value as { item?: { text: Embedded } }[];
    expect(docs).toHaveLength(3);
    expect(docs[1].item!.text).toBeInstanceOf(Embedded);
    expect(plain(e)).toEqual([
      { type: "thread.started", thread_id: "t1" },
      { type: "item.completed", item: { type: "agent_message", text: { summary: "Pong" } } },
      { type: "turn.completed", usage: { output_tokens: 24 } },
    ]);
  });

  it("only takes text as JSON Lines when every line is a document", () => {
    expect(parseJsonLines('{"a":1}')).toBeUndefined();
    expect(parseJsonLines('{"a":1}\nnot json')).toBeUndefined();
    expect(parseJsonLines('{"a":1}\r\n[2]\n')).toEqual([{ a: 1 }, [2]]);
    expect(decodeEmbedded("line one\nline two")).toBe("line one\nline two");
  });

  it("leaves alone what only looks like JSON", () => {
    expect(parseEmbedded("{not json}")).toBeUndefined();
    expect(parseEmbedded("42")).toBeUndefined();
    expect(parseEmbedded('"s"')).toBeUndefined();
    expect(parseEmbedded("{")).toBeUndefined();
    expect(parseEmbedded("  {} ")).toEqual({});
  });
});

describe("asTable", () => {
  it("makes an array of objects a table, a column per key in first-seen order", () => {
    expect(asTable([{ a: 1, b: 2 }, { b: 3, c: 4 }])).toEqual({
      columns: ["a", "b", "c"],
      rows: [
        [1, 2, undefined],
        [undefined, 3, 4],
      ],
      keyed: false,
    });
  });

  it("makes an object a key/value table", () => {
    expect(asTable({ x: 1, y: [2] })).toEqual({ columns: ["key", "value"], rows: [["x", 1], ["y", [2]]], keyed: true });
  });

  it("sees through embedded JSON, and refuses what isn't tabular", () => {
    expect(asTable(decodeEmbedded('[{"a":1}]'))?.columns).toEqual(["a"]);
    expect(asTable([1, 2])).toBeNull();
    expect(asTable([])).toBeNull();
    expect(asTable("s")).toBeNull();
  });

  it("renders cells compactly", () => {
    expect(cellText("x")).toBe("x");
    expect(cellText(undefined)).toBe("");
    expect(cellText({ a: [1] })).toBe('{"a":[1]}');
    expect(cellText(decodeEmbedded('{"a":1}'))).toBe('{"a":1}');
  });
});

describe("sizes", () => {
  it("counts UTF-8 bytes and formats them", () => {
    expect(byteSize("é")).toBe(2);
    expect(byteSize({ a: 1 })).toBe(7);
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(14254)).toBe("13.92 KB");
    expect(formatBytes(3 * 1024 * 1024)).toBe("3.00 MB");
  });
});
