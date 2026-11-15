import { describe, expect, it } from "vitest";
import { panelAction, togglePanel } from "./panels";
import { get } from "./store";

const key = (k: Partial<KeyboardEvent>) => ({ key: "", code: "", metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, ...k });

describe("panels", () => {
  it("maps ⌘B / ⌥⌘B / ⌘J on macOS and Ctrl elsewhere", () => {
    expect(panelAction(key({ key: "b", code: "KeyB", metaKey: true }), true)).toBe("left");
    expect(panelAction(key({ key: "∫", code: "KeyB", metaKey: true, altKey: true }), true)).toBe("right");
    expect(panelAction(key({ key: "j", code: "KeyJ", metaKey: true }), true)).toBe("bottom");
    expect(panelAction(key({ key: "b", code: "KeyB", ctrlKey: true }), false)).toBe("left");
    expect(panelAction(key({ key: "b", code: "KeyB", ctrlKey: true }), true)).toBeNull();
    expect(panelAction(key({ key: "b", code: "KeyB" }), true)).toBeNull();
    expect(panelAction(key({ key: "B", code: "KeyB", metaKey: true, shiftKey: true }), true)).toBeNull();
    expect(panelAction(key({ key: "s", code: "KeyS", metaKey: true }), true)).toBeNull();
  });

  it("toggles the store flag", () => {
    const before = get().rightOpen;
    togglePanel("right");
    expect(get().rightOpen).toBe(!before);
    togglePanel("right");
    expect(get().rightOpen).toBe(before);
  });
});
