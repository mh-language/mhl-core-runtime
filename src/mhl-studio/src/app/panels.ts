// Showing and hiding the side bars and the bottom run panel, like VS Code:
// ⌘B toggles the left side bar (files + toolbox), ⌥⌘B the right one
// (inspector), ⌘J the run panel — Ctrl instead of ⌘ off macOS. Which ones
// are open is remembered across launches.
import { get, set, useStore } from "./store";

const KEY = "mhl-studio.panels";

export type Panel = "left" | "right" | "bottom";

const FIELD = { left: "leftOpen", right: "rightOpen", bottom: "bottomOpen" } as const;

/** Which panel a key event asks to toggle, if any. */
export function panelAction(e: Pick<KeyboardEvent, "key" | "code" | "metaKey" | "ctrlKey" | "altKey" | "shiftKey">, isMac: boolean): Panel | null {
  const mod = isMac ? e.metaKey : e.ctrlKey;
  if (!mod || e.shiftKey) return null;
  // With ⌥ held, macOS turns `key` into a symbol ("∫"), so match `code` too.
  const is = (letter: string) => e.key.toLowerCase() === letter || e.code === `Key${letter.toUpperCase()}`;
  if (is("b")) return e.altKey ? "right" : "left";
  if (is("j") && !e.altKey) return "bottom";
  return null;
}

export function togglePanel(p: Panel) {
  const f = FIELD[p];
  set({ [f]: !get()[f] });
}

/** Restores the saved layout, installs the shortcuts and saves every change. */
export function installPanels() {
  try {
    const saved = JSON.parse(localStorage.getItem(KEY) ?? "{}") as Partial<Record<Panel, boolean>>;
    const patch: Record<string, boolean> = {};
    for (const p of Object.keys(FIELD) as Panel[]) if (typeof saved[p] === "boolean") patch[FIELD[p]] = saved[p]!;
    set(patch);
  } catch {
    /* storage unavailable or corrupt: keep the defaults */
  }
  useStore.subscribe((s, prev) => {
    if (s.leftOpen === prev.leftOpen && s.rightOpen === prev.rightOpen && s.bottomOpen === prev.bottomOpen) return;
    try {
      localStorage.setItem(KEY, JSON.stringify({ left: s.leftOpen, right: s.rightOpen, bottom: s.bottomOpen }));
    } catch {
      /* storage unavailable */
    }
  });
  const isMac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
  // Capture phase, so Monaco and React Flow don't swallow the keys.
  window.addEventListener(
    "keydown",
    (e) => {
      const p = panelAction(e, isMac);
      if (!p) return;
      e.preventDefault();
      e.stopPropagation();
      togglePanel(p);
    },
    true,
  );
}
