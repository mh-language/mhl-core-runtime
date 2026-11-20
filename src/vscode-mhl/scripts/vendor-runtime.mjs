#!/usr/bin/env node
// Vendors the mhl runtime into the extension, the way the studio's
// build-sidecar.mjs does for Tauri: builds src/mhl-runtime for one VS Code
// platform target and writes it to bin/, which a platform-specific VSIX
// (`vsce package --target <target>`) then carries. The extension starts
// bin/mhl as its language server when mhl.serverPath is left empty.
//
//   node scripts/vendor-runtime.mjs [--target <vscode-target>] [--source]
//   node scripts/vendor-runtime.mjs --host-target   # print this machine's target
//
// By default it builds the runtime release pinned in package.json's
// "mhlRuntime" (a vX.Y.Z tag, which must exist locally — `git fetch --tags`),
// so the extension never ships a runtime that was not released. --source
// builds the working tree's src/mhl-runtime instead, for local development.
//
// bin/ is replaced as a whole: mhl[.exe], mhl.json (the target and runtime
// version, checked by the extension before it trusts the binary), and the
// runtime's LICENSE.md / THIRD_PARTY_NOTICES.md, which the statically linked
// BSD-licensed Go code requires in a binary distribution.
import { execFileSync } from "node:child_process";
import { chmodSync, copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const extension = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repo = resolve(extension, "../..");
const binDir = join(extension, "bin");
const MODULE = "github.com/mh-language/mhl-core-runtime";

/**
 * Maps a VS Code platform target (`vsce package --target`) onto Go's
 * GOOS/GOARCH. alpine-* gets the linux build: CGO_ENABLED=0 makes it static.
 */
export function goTarget(target) {
  const [platform, arch] = target.split("-");
  const GOOS = { win32: "windows", linux: "linux", alpine: "linux", darwin: "darwin" }[platform];
  const GOARCH = { x64: "amd64", arm64: "arm64" }[arch];
  if (!GOOS || !GOARCH || target.split("-").length !== 2) {
    throw new Error(`unsupported VS Code target ${target} (expected <win32|linux|alpine|darwin>-<x64|arm64>)`);
  }
  return { GOOS, GOARCH, ext: GOOS === "windows" ? ".exe" : "" };
}

/** This machine's VS Code target. musl (alpine) is not detected: pass --target. */
export function hostTarget(platform = process.platform, arch = process.arch) {
  return `${platform}-${arch}`;
}

const git = (args, opts = {}) => execFileSync("git", args, { cwd: repo, encoding: "utf8", ...opts });

/** The pinned runtime release, from package.json's "mhlRuntime". */
export function pinnedRuntime(pkg) {
  const tag = pkg.mhlRuntime;
  if (typeof tag !== "string" || !/^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(tag)) {
    throw new Error(`package.json "mhlRuntime" must be a runtime release tag like v1.2.3 (got: ${JSON.stringify(tag)})`);
  }
  return tag;
}

/** Exports the runtime's sources at `tag` into a temp dir; returns [repo-like root, cleanup]. */
function exportTag(tag) {
  try {
    git(["rev-parse", "-q", "--verify", `refs/tags/${tag}^{commit}`]);
  } catch {
    throw new Error(`runtime tag ${tag} is not in this clone — run: git fetch --tags`);
  }
  const root = mkdtempSync(join(tmpdir(), "mhl-runtime-"));
  const archive = execFileSync("git", ["archive", "--format=tar", tag, "--", "src/mhl-runtime", "LICENSE.md", "THIRD_PARTY_NOTICES.md"], {
    cwd: repo,
    maxBuffer: 1 << 30,
  });
  execFileSync("tar", ["-x", "-C", root], { input: archive });
  return [root, () => rmSync(root, { recursive: true, force: true })];
}

/** The working tree's runtime version, as build-sidecar.mjs computes it. */
function sourceVersion() {
  const runtime = join(repo, "src/mhl-runtime");
  try {
    const base = git(["describe", "--tags", "--match", "v*.*.*", "--always"], { cwd: runtime }).trim();
    const dirty = git(["status", "--porcelain", "--untracked-files=no", "--", "."], { cwd: runtime }).trim() !== "";
    return dirty ? `${base}-dirty` : base;
  } catch {
    return "dev";
  }
}

function main(argv) {
  if (argv.includes("--host-target")) {
    console.log(hostTarget());
    return;
  }
  const at = argv.indexOf("--target");
  const target = (at >= 0 && argv[at + 1]) || hostTarget();
  const { GOOS, GOARCH, ext } = goTarget(target);

  let root = repo;
  let version;
  let cleanup = () => {};
  if (argv.includes("--source")) {
    version = sourceVersion();
  } else {
    version = pinnedRuntime(JSON.parse(readFileSync(join(extension, "package.json"), "utf8")));
    [root, cleanup] = exportTag(version);
  }

  try {
    rmSync(binDir, { recursive: true, force: true });
    mkdirSync(binDir, { recursive: true });
    const out = join(binDir, `mhl${ext}`);
    const ldflags = `-s -w -X ${MODULE}/internal/cli.Version=${version}`;
    console.log(`[vendor] mhl ${version} -> ${GOOS}/${GOARCH} (${target}) -> ${out}`);
    execFileSync("go", ["build", "-trimpath", "-ldflags", ldflags, "-o", out, "./cmd/mhl"], {
      cwd: join(root, "src/mhl-runtime"),
      stdio: "inherit",
      env: { ...process.env, CGO_ENABLED: "0", GOOS, GOARCH },
    });
    chmodSync(out, 0o755);
    for (const notice of ["LICENSE.md", "THIRD_PARTY_NOTICES.md"]) {
      copyFileSync(join(root, notice), join(binDir, notice));
    }
    writeFileSync(join(binDir, "mhl.json"), JSON.stringify({ target, version }, null, 2) + "\n");
  } finally {
    cleanup();
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try {
    main(process.argv.slice(2));
  } catch (err) {
    console.error(`[vendor] error: ${err instanceof Error ? err.message : err}`);
    process.exit(1);
  }
}
