/// <reference types="vitest/config" />
import { defineConfig, searchForWorkspaceRoot } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

// The TextMate grammar and language configuration are imported straight from
// vscode-mhl (one copy for both editors), which lives outside this package.
const vscodeMhl = fileURLToPath(new URL("../../vscode-mhl", import.meta.url));
// monaco-editor's ESM tree, for importing editor contributions without its
// package "exports" map (see src/editor/monaco-core.js).
const monacoEsm = fileURLToPath(new URL("./node_modules/monaco-editor/esm/vs", import.meta.url));

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { "monaco-esm": monacoEsm } },
  clearScreen: false,
  server: {
    port: 1420,
    strictPort: true,
    fs: { allow: [searchForWorkspaceRoot(process.cwd()), vscodeMhl] },
    watch: { ignored: ["**/src-tauri/**"] },
  },
  envPrefix: ["VITE_", "TAURI_ENV_"],
  build: {
    target: "es2022",
    chunkSizeWarningLimit: 8000,
  },
  worker: { format: "es" },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
});
