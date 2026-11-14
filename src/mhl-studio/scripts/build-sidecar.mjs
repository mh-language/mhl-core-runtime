#!/usr/bin/env node
// Vendors the mhl runtime into the studio: builds src/mhl-runtime for the
// target the Tauri app is being built for and writes it where Tauri's
// `bundle.externalBin: ["binaries/mhl"]` expects it —
// src-tauri/binaries/mhl-<rust-target-triple>[.exe]. Same flags as the
// runtime's Makefile, so the bundled binary is the one `make build` makes.
//
//   node scripts/build-sidecar.mjs [--target <triple>]
//
// The target defaults to TAURI_ENV_TARGET_TRIPLE (set by `tauri build
// --target`) or the host triple from `rustc -vV`.
import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const runtime = resolve(here, "../../mhl-runtime");
const outDir = resolve(here, "../src-tauri/binaries");

function hostTriple() {
  const out = execFileSync("rustc", ["-vV"], { encoding: "utf8" });
  const line = out.split("\n").find((l) => l.startsWith("host:"));
  if (!line) throw new Error("cannot read host triple from `rustc -vV`");
  return line.slice("host:".length).trim();
}

/** Maps a Rust target triple onto Go's GOOS/GOARCH. */
export function goTarget(triple) {
  const arch = triple.startsWith("aarch64")
    ? "arm64"
    : triple.startsWith("x86_64")
      ? "amd64"
      : null;
  const os = triple.includes("apple-darwin")
    ? "darwin"
    : triple.includes("windows")
      ? "windows"
      : triple.includes("linux")
        ? "linux"
        : null;
  if (!arch || !os) throw new Error(`unsupported target triple ${triple}`);
  return { GOOS: os, GOARCH: arch, ext: os === "windows" ? ".exe" : "" };
}

function version() {
  try {
    return execFileSync("git", ["describe", "--tags", "--match", "v*.*.*", "--always", "--dirty"], {
      cwd: runtime,
      encoding: "utf8",
    }).trim();
  } catch {
    return "dev";
  }
}

const argTarget = process.argv.indexOf("--target");
const triple =
  (argTarget > 0 && process.argv[argTarget + 1]) || process.env.TAURI_ENV_TARGET_TRIPLE || hostTriple();
const { GOOS, GOARCH, ext } = goTarget(triple);
const out = join(outDir, `mhl-${triple}${ext}`);
mkdirSync(outDir, { recursive: true });

const ldflags = `-s -w -X github.com/mh-language/mhl-core-runtime/internal/cli.Version=${version()}`;
console.log(`[sidecar] go build ${GOOS}/${GOARCH} -> ${out}`);
execFileSync("go", ["build", "-trimpath", "-ldflags", ldflags, "-o", out, "./cmd/mhl"], {
  cwd: runtime,
  stdio: "inherit",
  env: { ...process.env, CGO_ENABLED: "0", GOOS, GOARCH },
});
