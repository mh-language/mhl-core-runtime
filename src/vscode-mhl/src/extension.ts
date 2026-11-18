import * as fs from "fs";
import * as os from "os";
import * as path from "path";
import * as vscode from "vscode";
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
  TransportKind,
} from "vscode-languageclient/node";
import {
  BundledManifest,
  bundledMatchesHost,
  resolveServerPath,
} from "./serverPath";

const INSTALL_URL = "https://github.com/mh-language/mhl-core-runtime#install";

let client: LanguageClient | undefined;
// The runtime vendored into a platform-specific VSIX (bin/mhl), if this
// install carries one built for this host.
let bundled: string | undefined;

export function activate(context: vscode.ExtensionContext) {
  bundled = findBundledServer(context.extensionPath);
  // Integrated terminals get the bundled `mhl` too. Appended, so an mhl the
  // user installed on PATH still wins there. The collection persists across
  // sessions, so it is rebuilt every activation (an update may drop bin/).
  const env = context.environmentVariableCollection;
  env.clear();
  if (bundled !== undefined) {
    env.description = "Makes the mhl CLI bundled with the MHL extension available as a fallback on PATH.";
    env.append("PATH", path.delimiter + path.dirname(bundled));
  }

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
  const configured = vscode.workspace.getConfiguration("mhl").get<string>("serverPath", "");
  const folder = vscode.workspace.workspaceFolders?.[0];
  return resolveServerPath(configured, folder?.uri.fsPath, vscode.workspace.isTrusted, bundled);
}

// findBundledServer returns bin/mhl[.exe] when bin/mhl.json says it was built
// for this host, else undefined (a universal VSIX has no bin/ at all).
function findBundledServer(extensionPath: string): string | undefined {
  const binDir = path.join(extensionPath, "bin");
  let manifest: BundledManifest;
  try {
    manifest = JSON.parse(fs.readFileSync(path.join(binDir, "mhl.json"), "utf8"));
  } catch {
    return undefined;
  }
  if (!bundledMatchesHost(String(manifest.target), process.platform, process.arch)) {
    return undefined;
  }
  const binary = path.join(binDir, process.platform === "win32" ? "mhl.exe" : "mhl");
  if (!fs.existsSync(binary)) {
    return undefined;
  }
  if (process.platform !== "win32") {
    // Defensive: should the VSIX extraction drop the executable bit, restore
    // it. The extension directory may be read-only; then the spawn error says why.
    try {
      if ((fs.statSync(binary).mode & 0o111) === 0) {
        fs.chmodSync(binary, 0o755);
      }
    } catch {
      // fall through
    }
  }
  return binary;
}
