import * as vscode from "vscode";
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
  TransportKind,
} from "vscode-languageclient/node";

let client: LanguageClient | undefined;

export function activate(context: vscode.ExtensionContext) {
  context.subscriptions.push(
    vscode.commands.registerCommand("mhl.restartLanguageServer", () => restart(context))
  );

  start(context);
}

export async function deactivate(): Promise<void> {
  if (client) {
    await client.stop();
  }
}

function start(context: vscode.ExtensionContext) {
  const serverPath = resolveServerPath();

  const serverOptions: ServerOptions = {
    command: serverPath,
    args: ["lsp"],
    transport: TransportKind.stdio,
  };

  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ scheme: "file", language: "mhl" }],
  };

  client = new LanguageClient("mhl", "MHL Language Server", serverOptions, clientOptions);

  client.start().then(undefined, (err: unknown) => {
    const message = err instanceof Error ? err.message : String(err);
    vscode.window
      .showErrorMessage(
        `MHL: could not start the language server ("${serverPath} lsp"). Build it with "make build" in mhl-runtime and either add its dist/ to PATH or set "mhl.serverPath". (${message})`,
        "Open Settings"
      )
      .then((choice) => {
        if (choice === "Open Settings") {
          vscode.commands.executeCommand("workbench.action.openSettings", "mhl.serverPath");
        }
      });
  });

  context.subscriptions.push({ dispose: () => client?.stop() });
}

async function restart(context: vscode.ExtensionContext) {
  if (client) {
    await client.stop();
    client = undefined;
  }
  start(context);
}

// resolveServerPath reads the mhl.serverPath setting (default: "mhl" on
// PATH) and expands a leading ${workspaceFolder} so users can point it at a
// binary checked into the repo (e.g. mhl-runtime/dist/mhl) without an
// absolute path.
function resolveServerPath(): string {
  const configured = vscode.workspace.getConfiguration("mhl").get<string>("serverPath", "mhl");
  const folder = vscode.workspace.workspaceFolders?.[0];
  if (folder && configured.includes("${workspaceFolder}")) {
    return configured.replace(/\$\{workspaceFolder\}/g, folder.uri.fsPath);
  }
  return configured;
}
