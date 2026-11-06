export const DEFAULT_SERVER_PATH = "mhl";

// resolveServerPath expands ${workspaceFolder} in the mhl.serverPath setting so
// users can point it at a binary inside the repo (e.g. mhl-runtime/dist/mhl)
// without an absolute path.
//
// VS Code already drops workspace-defined values of this setting in Restricted
// Mode (package.json's restrictedConfigurations), but a user-level
// "${workspaceFolder}/..." would still resolve into the untrusted folder, so
// that case falls back to the default instead of running the folder's binary.
export function resolveServerPath(
  configured: string,
  folderPath: string | undefined,
  trusted: boolean
): string {
  if (!configured.includes("${workspaceFolder}")) {
    return configured;
  }
  if (!trusted) {
    return DEFAULT_SERVER_PATH;
  }
  if (folderPath === undefined) {
    return configured;
  }
  return configured.replace(/\$\{workspaceFolder\}/g, () => folderPath);
}
