import * as os from "os";
import * as vscode from "vscode";
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
  TransportKind,
} from "vscode-languageclient/node";
import { DEFAULT_SERVER_PATH, resolveServerPath } from "./serverPath";

const INSTALL_URL = "https://github.com/mh-language/mhl-core-runtime#install";

let client: LanguageClient | undefined;

export function activate(context: vscode.ExtensionContext) {
  context.subscriptions.push(
    vscode.commands.registerCommand("mhl.restartLanguageServer", restart),
    vscode.commands.registerCommand(
      "mhl.showReferences",
      async (uriValue: string, position: vscode.Position) => {
        const uri = vscode.Uri.parse(uriValue);
        const locations = await vscode.commands.executeCommand<vscode.Location[]>(
          "vscode.executeReferenceProvider",
          uri,
          new vscode.Position(position.line, position.character)
        );
        await vscode.commands.executeCommand(
          "editor.action.showReferences",
          uri,
          new vscode.Position(position.line, position.character),
          locations ?? []
        );
      }
    ),
    vscode.workspace.onDidGrantWorkspaceTrust(restart),
    { dispose: stopClient }
  );

  start();
}

export function deactivate(): Promise<void> {
  return stopClient();
}

function start() {
  const serverPath = configuredServerPath();

  const serverOptions: ServerOptions = {
    command: serverPath,
    args: ["lsp"],
    transport: TransportKind.stdio,
    // The default cwd is the workspace folder, where Windows looks for a bare
    // `mhl` before PATH, so an untrusted folder could supply its own binary.
    options: vscode.workspace.isTrusted ? undefined : { cwd: os.homedir() },
  };

  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ scheme: "file", language: "mhl" }],
  };

  client = new LanguageClient("mhl", "MHL Language Server", serverOptions, clientOptions);

  client.start().then(undefined, (err: unknown) => {
    const message = err instanceof Error ? err.message : String(err);
    vscode.window
      .showErrorMessage(
        `MHL: could not start the language server ("${serverPath} lsp"). Syntax highlighting still works; diagnostics, completion and navigation need the mhl CLI. Install it, then run "MHL: Restart Language Server", or set "mhl.serverPath". (${message})`,
        "Install mhl",
        "Open Settings"
      )
      .then((choice) => {
        if (choice === "Install mhl") {
          vscode.env.openExternal(vscode.Uri.parse(INSTALL_URL));
        } else if (choice === "Open Settings") {
          vscode.commands.executeCommand("workbench.action.openSettings", "mhl.serverPath");
        }
      });
  });
}

// stopClient only stops a running client: LanguageClient.stop() throws after a
// failed start, which would stop restart() from ever trying again.
async function stopClient(): Promise<void> {
  const current = client;
  client = undefined;
  if (current?.isRunning()) {
    await current.stop();
  }
}

async function restart() {
  await stopClient();
  start();
}

function configuredServerPath(): string {
  const configured = vscode.workspace
    .getConfiguration("mhl")
    .get<string>("serverPath", DEFAULT_SERVER_PATH);
  const folder = vscode.workspace.workspaceFolders?.[0];
  return resolveServerPath(configured, folder?.uri.fsPath, vscode.workspace.isTrusted);
}
