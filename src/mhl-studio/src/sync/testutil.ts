// Test helper: graphs a source with the real vendored `mhl` (the same
// binary the app ships), so edit tests check round trips against the actual
// parser rather than a hand-written fixture.
import { execFileSync } from "node:child_process";
import { existsSync, readdirSync } from "node:fs";
import { join, resolve } from "node:path";
import type { Graph } from "../lib/types";

function findMhl(): string | null {
  if (process.env.MHL && existsSync(process.env.MHL)) return process.env.MHL;
  const bin = resolve(__dirname, "../../src-tauri/binaries");
  if (existsSync(bin)) {
    const f = readdirSync(bin).find((n) => n.startsWith("mhl-"));
    if (f) return join(bin, f);
  }
  const dist = resolve(__dirname, "../../../mhl-runtime/dist/mhl");
  return existsSync(dist) ? dist : null;
}

export const MHL = findMhl();

export function graphOf(src: string): Graph {
  if (!MHL) throw new Error("no mhl binary: run `npm run sidecar`");
  const out = execFileSync(MHL, ["graph", "test.mh", "--stdin"], { input: src, encoding: "utf8" });
  return JSON.parse(out) as Graph;
}
