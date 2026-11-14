// Typed wrappers over the Tauri commands in src-tauri/src.
import { Channel, invoke } from "@tauri-apps/api/core";
import type { Graph, Inspection, RunEvent, RunResult } from "./types";

export const api = {
  /** The workspace's `.mh`/`.md` files and its folders (src-tauri/src/workspace.rs). */
  listFiles: (root: string) => invoke<{ files: string[]; dirs: string[] }>("list_files", { root }),
  createDir: (path: string) => invoke<void>("create_dir", { path }),
  /** Moves/renames a file or folder; never overwrites (workspace.rs `rename_path`). */
  renamePath: (from: string, to: string) => invoke<void>("rename_path", { from, to }),
  /** Moves a file or folder to the system Trash. */
  deletePath: (path: string) => invoke<void>("delete_path", { path }),
  readFile: (path: string) => invoke<string>("read_file", { path }),
  writeFile: (path: string, contents: string) => invoke<void>("write_file", { path, contents }),
  createFile: (path: string, contents: string) => invoke<void>("create_file", { path, contents }),
  mhlVersion: () => invoke<string>("mhl_version"),
  graph: (path: string, source: string) => invoke<Graph>("graph", { path, source }),
  inspect: (cwd: string, path: string, workflow: string | null, inputs: [string, string][]) =>
    invoke<Inspection>("inspect", { cwd, path, workflow, inputs }),
  /** Evaluates an expression against a saved file's declarations (src-tauri/src/mhl.rs `evaluate`). */
  evaluate: (cwd: string, path: string, expr: string) => invoke<RunResult>("evaluate", { cwd, path, expr }),
  runStart: (
    request: {
      cwd: string;
      path: string;
      workflow: string | null;
      inputs: [string, string][];
      resume: boolean;
      force: boolean;
      session?: string;
    },
    onEvent: (ev: RunEvent) => void,
  ) => {
    const channel = new Channel<RunEvent>();
    channel.onmessage = onEvent;
    return invoke<number>("run_start", { request, onEvent: channel });
  },
  runCancel: (id: number, force: boolean) => invoke<void>("run_cancel", { id, force }),
  /** Where each named CLI is on the (login-shell) PATH; absent ones are left out (shellenv.rs). */
  findClis: (names: string[]) => invoke<Record<string, string>>("find_clis", { names }),
  lspStart: (cwd: string) => invoke<void>("lsp_start", { cwd }),
  lspSend: (message: string) => invoke<void>("lsp_send", { message }),
  lspStop: () => invoke<void>("lsp_stop"),
};

export function joinPath(root: string, rel: string): string {
  const sep = root.includes("\\") && !root.includes("/") ? "\\" : "/";
  return root.replace(/[\\/]+$/, "") + sep + rel.split("/").join(sep);
}

export function fileUri(path: string): string {
  const p = path.replace(/\\/g, "/");
  return "file://" + (p.startsWith("/") ? "" : "/") + p.split("/").map(encodeURIComponent).join("/");
}

export function errorText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
