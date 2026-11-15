import { describe, expect, it } from "vitest";
import { centerPoint } from "./viewport";

describe("centerPoint", () => {
  it("centers a diagram that fits", () => {
    expect(centerPoint({ x: 100, y: 50, width: 400, height: 300 }, { width: 1000, height: 800 }, 1)).toEqual({ x: 300, y: 200 });
  });

  it("keeps the top of a tall diagram in view, centered horizontally", () => {
    // 1200 tall at zoom 1 in an 800-high canvas: the top sits 24px below the edge.
    const p = centerPoint({ x: 0, y: 0, width: 400, height: 1200 }, { width: 1000, height: 800 }, 1);
    expect(p).toEqual({ x: 200, y: -24 + 400 });
  });

  it("measures in flow units at other zooms", () => {
    // zoom 2: the canvas shows 400 flow units; a 300-tall diagram plus 2×12 padding fits.
    expect(centerPoint({ x: 0, y: 0, width: 100, height: 300 }, { width: 800, height: 800 }, 2)).toEqual({ x: 50, y: 150 });
  });
});
