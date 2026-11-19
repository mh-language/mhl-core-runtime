// The color theme: follow the system (the default), or force light or dark.
// The toolbar's theme button cycles through the three; the choice is
// remembered across launches. The resolved scheme drives the CSS tokens
// (`<html data-theme>`), React Flow's colorMode, Monaco's theme (CodeEditor)
// and the native window's title bar.
import { getCurrentWindow } from "@tauri-apps/api/window";
import { get, set, useStore, type Scheme, type ThemePref } from "./store";

const KEY = "mhl-studio.theme";

/** The button's cycle order. */
export const THEMES: ThemePref[] = ["system", "light", "dark"];

/** The preference after `t` in the cycle. */
export function nextTheme(t: ThemePref): ThemePref {
  return THEMES[(THEMES.indexOf(t) + 1) % THEMES.length];
}

/** What a preference shows, given whether the system is dark. */
export function resolveTheme(pref: ThemePref, systemDark: boolean): Scheme {
  return pref === "system" ? (systemDark ? "dark" : "light") : pref;
}

/** A stored value, or "system" when absent or unknown. */
export function parseTheme(raw: string | null | undefined): ThemePref {
  return THEMES.includes(raw as ThemePref) ? (raw as ThemePref) : "system";
}

const query = () => window.matchMedia?.("(prefers-color-scheme: dark)");

function apply(pref: ThemePref, scheme: Scheme) {
  document.documentElement.dataset.theme = scheme;
  // The title bar: forced to match, or back to following the OS (null).
  void getCurrentWindow()
    .setTheme(pref === "system" ? null : pref)
    .catch(() => undefined); // not inside Tauri (a plain browser during development)
}

export function setTheme(pref: ThemePref) {
  set({ theme: pref, scheme: resolveTheme(pref, query()?.matches ?? false) });
}

export function cycleTheme() {
  setTheme(nextTheme(get().theme));
}

/** Restores the saved preference, then keeps the page, the window and the store in step with it. */
export function installTheme() {
  let saved: string | null = null;
  try {
    saved = localStorage.getItem(KEY);
  } catch {
    /* storage unavailable: follow the system */
  }
  const pref = parseTheme(saved);
  setTheme(pref);
  apply(pref, get().scheme);
  useStore.subscribe((s, prev) => {
    if (s.theme === prev.theme && s.scheme === prev.scheme) return;
    apply(s.theme, s.scheme);
    if (s.theme === prev.theme) return;
    try {
      localStorage.setItem(KEY, s.theme);
    } catch {
      /* storage unavailable */
    }
  });
  // The OS switched (or the window stopped being forced): re-resolve "system".
  query()?.addEventListener("change", (e) => {
    if (get().theme === "system") set({ scheme: e.matches ? "dark" : "light" });
  });
}
