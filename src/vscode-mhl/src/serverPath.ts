// The binary started when no bundled runtime applies: `mhl` on PATH.
export const PATH_SERVER = "mhl";

// BundledManifest is bin/mhl.json, written by scripts/vendor-runtime.mjs next
// to the vendored binary.
export interface BundledManifest {
  target: string;
  version: string;
}

// bundledMatchesHost reports whether a runtime vendored for `target` (a VS Code
// platform target such as darwin-arm64 or alpine-x64) runs on this host. A
// local `vsce package` without --target would carry whatever bin/ held, so the
// extension checks rather than assumes. alpine-* binaries are the static linux
// build, so they run on any linux of that arch.
export function bundledMatchesHost(target: string, platform: string, arch: string): boolean {
  const [os, cpu] = target.split("-");
  const osMatches = os === platform || (os === "alpine" && platform === "linux");
  return osMatches && cpu === arch;
}

// resolveServerPath picks the binary that serves `mhl lsp`:
//
//   1. mhl.serverPath, when set, with ${workspaceFolder} expanded so users can
//      point it at a binary inside the repo (e.g. src/mhl-runtime/dist/mhl);
//   2. otherwise the runtime bundled with the extension (`bundled`), if any;
//   3. otherwise `mhl` on PATH.
//
// VS Code already drops workspace-defined values of this setting in Restricted
// Mode (package.json's restrictedConfigurations), but a user-level
// "${workspaceFolder}/..." would still resolve into the untrusted folder, so
// that case falls back as if the setting were empty. The bundled binary ships
// with the extension, not the workspace, so it is safe in Restricted Mode.
export function resolveServerPath(
  configured: string,
  folderPath: string | undefined,
  trusted: boolean,
  bundled?: string
): string {
  const fallback = bundled ?? PATH_SERVER;
  const value = configured.trim();
  if (value === "") {
    return fallback;
  }
  if (!value.includes("${workspaceFolder}")) {
    return value;
  }
  if (!trusted) {
    return fallback;
  }
  if (folderPath === undefined) {
    return value;
  }
  return value.replace(/\$\{workspaceFolder\}/g, () => folderPath);
}
