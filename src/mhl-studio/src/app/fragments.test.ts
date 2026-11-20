import { describe, expect, it } from "vitest";
import { fragmentInfo, mergeRoot } from "./fragments";
import type { Graph } from "../lib/types";

const wf = (name: string, entry: boolean) =>
  ({ name, partial: true, nodes: entry ? [{ id: "A", entry: true }] : [{ id: "B" }], routes: [] }) as unknown as Graph["workflows"][number];

describe("fragmentInfo", () => {
  it("resolves imports and lists the partial workflows a file starts", () => {
    const info = fragmentInfo("/w/story.mh", {
      imports: [{ from: "story/impl.mh", names: [], locals: [], span: [0, 0] }],
      workflows: [wf("Story", true), wf("Other", false)],
    });
    expect(info).toEqual({ imports: ["/w/story/impl.mh"], entries: ["Story"] });
  });
});

describe("mergeRoot", () => {
  const files = [
    { path: "/w/story.mh", imports: ["/w/story/spec.mh"], entries: ["Story"] },
    { path: "/w/story/spec.mh", imports: ["/w/story/impl.mh"], entries: [] },
    { path: "/w/story/impl.mh", imports: [], entries: [] },
    { path: "/w/lone.mh", imports: [], entries: [] },
  ];
  it("is the file itself when it declares the entry fragment", () => {
    expect(mergeRoot("/w/story.mh", "Story", files)).toBe("/w/story.mh");
  });
  it("is the entry file that imports the fragment, transitively", () => {
    expect(mergeRoot("/w/story/impl.mh", "Story", files)).toBe("/w/story.mh");
  });
  it("is null for a fragment nothing imports", () => {
    expect(mergeRoot("/w/lone.mh", "Story", files)).toBeNull();
  });
  it("matches Windows paths", () => {
    expect(mergeRoot("C:\\w\\story\\impl.mh", "Story", [{ path: "C:\\w\\story.mh", imports: ["C:/w/story/impl.mh"], entries: ["Story"] }])).toBe("C:\\w\\story.mh");
  });
});
