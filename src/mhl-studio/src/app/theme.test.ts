import { describe, expect, it, vi } from "vitest";

vi.mock("@tauri-apps/api/window", () => ({ getCurrentWindow: () => ({ setTheme: async () => undefined }) }));

const { nextTheme, parseTheme, resolveTheme } = await import("./theme");

describe("theme", () => {
  it("cycles system → light → dark → system", () => {
    expect(nextTheme("system")).toBe("light");
    expect(nextTheme("light")).toBe("dark");
    expect(nextTheme("dark")).toBe("system");
  });

  it("resolves system from the OS and forces the others", () => {
    expect(resolveTheme("system", true)).toBe("dark");
    expect(resolveTheme("system", false)).toBe("light");
    expect(resolveTheme("light", true)).toBe("light");
    expect(resolveTheme("dark", false)).toBe("dark");
  });

  it("falls back to system for a missing or unknown stored value", () => {
    expect(parseTheme(null)).toBe("system");
    expect(parseTheme("sepia")).toBe("system");
    expect(parseTheme("dark")).toBe("dark");
  });
});
