import { describe, expect, it, vi } from "vitest";

vi.mock("@tauri-apps/api/webview", () => ({ getCurrentWebview: () => ({ setZoom: async () => undefined }) }));
vi.mock("../editor/monaco", () => ({ monaco: { editor: { remeasureFonts: () => undefined } } }));

const { step, zoomAction, LEVELS } = await import("./zoom");

const key = (k: Partial<KeyboardEvent>) => ({ key: "", code: "", metaKey: false, ctrlKey: false, altKey: false, ...k });

describe("zoom", () => {
  it("steps through the levels and clamps at both ends", () => {
    expect(step(1, 1)).toBe(1.1);
    expect(step(1, -1)).toBe(0.9);
    expect(step(LEVELS[LEVELS.length - 1], 1)).toBe(LEVELS[LEVELS.length - 1]);
    expect(step(LEVELS[0], -1)).toBe(LEVELS[0]);
  });

  it("uses ⌘ on macOS and Ctrl elsewhere", () => {
    expect(zoomAction(key({ key: "=", code: "Equal", metaKey: true }), true)).toBe("in");
    expect(zoomAction(key({ key: "=", code: "Equal", ctrlKey: true }), true)).toBeNull();
    expect(zoomAction(key({ key: "+", code: "Equal", ctrlKey: true }), false)).toBe("in");
    expect(zoomAction(key({ key: "-", code: "Minus", ctrlKey: true }), false)).toBe("out");
    expect(zoomAction(key({ key: "0", code: "Digit0", metaKey: true }), true)).toBe("reset");
    expect(zoomAction(key({ key: "+", code: "NumpadAdd", metaKey: true }), true)).toBe("in");
    expect(zoomAction(key({ key: "s", code: "KeyS", metaKey: true }), true)).toBeNull();
    expect(zoomAction(key({ key: "=", code: "Equal" }), true)).toBeNull();
  });
});
