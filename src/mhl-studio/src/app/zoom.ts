// Whole-app zoom, like VS Code's: ⌘+ / ⌘- / ⌘0 on macOS, Ctrl+ / Ctrl- /
// Ctrl+0 elsewhere. It scales the native webview (Tauri's setZoom), so the
// diagram, the editor and every panel grow together and pointer coordinates
// stay correct. The level is remembered across launches.
import { getCurrentWebview } from "@tauri-apps/api/webview";
import { monaco } from "../editor/monaco";

const KEY = "mhl-studio.zoom";
/** The zoom steps, as in a browser. */
export const LEVELS = [0.5, 0.67, 0.75, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2, 2.5, 3];

let level = 1;
let onChange: (zoom: number) => void = () => undefined;

function load(): number {
  try {
    const v = Number(localStorage.getItem(KEY));
    return LEVELS.includes(v) ? v : 1;
  } catch {
    return 1;
  }
}

/** The next level up (dir 1) or down (dir -1) from `from`. */
export function step(from: number, dir: 1 | -1): number {
  if (dir > 0) return LEVELS.find((l) => l > from + 1e-9) ?? LEVELS[LEVELS.length - 1];
  return [...LEVELS].reverse().find((l) => l < from - 1e-9) ?? LEVELS[0];
}

export async function setZoom(z: number) {
  level = z;
  try {
    await getCurrentWebview().setZoom(z);
  } catch {
    // Not inside Tauri (a plain browser during development): CSS fallback.
    (document.documentElement.style as CSSStyleDeclaration & { zoom: string }).zoom = String(z);
  }
  try {
    localStorage.setItem(KEY, String(z));
  } catch {
    /* storage unavailable */
  }
  // Monaco caches glyph widths; re-measure so the cursor lines up.
  monaco.editor.remeasureFonts();
  onChange(z);
}

/** Which zoom action a key event asks for, if any. */
export function zoomAction(e: Pick<KeyboardEvent, "key" | "code" | "metaKey" | "ctrlKey" | "altKey">, isMac: boolean): "in" | "out" | "reset" | null {
  const mod = isMac ? e.metaKey : e.ctrlKey;
  if (!mod || e.altKey) return null;
  // `key` follows the keyboard layout (ABNT, US, ...); `code` covers layouts
  // where "+" needs Shift and the numeric keypad.
  if (e.key === "+" || e.key === "=" || e.code === "Equal" || e.code === "NumpadAdd") return "in";
  if (e.key === "-" || e.key === "_" || e.code === "Minus" || e.code === "NumpadSubtract") return "out";
  if (e.key === "0" || e.code === "Digit0" || e.code === "Numpad0") return "reset";
  return null;
}

/**
 * Restores the saved level and installs the shortcuts. The listener runs in
 * the capture phase so it wins over Monaco and React Flow, which would
 * otherwise consume the keys while they have focus.
 */
export function installZoom(changed: (zoom: number) => void) {
  onChange = changed;
  const isMac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
  const saved = load();
  if (saved !== 1) void setZoom(saved);
  window.addEventListener(
    "keydown",
    (e) => {
      const action = zoomAction(e, isMac);
      if (!action) return;
      e.preventDefault();
      e.stopPropagation();
      void setZoom(action === "reset" ? 1 : step(level, action === "in" ? 1 : -1));
    },
    true,
  );
}
