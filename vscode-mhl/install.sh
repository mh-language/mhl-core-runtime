#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

VERSION=$(node -p "require('./package.json').version")
VSIX_FILE="mhl-language-${VERSION}.vsix"

echo "Removing old ${VSIX_FILE}..."
rm -f *.vsix

echo "Installing extension dependencies..."
npm install

echo "Packaging ${VSIX_FILE}..."
npx --yes @vscode/vsce package

echo "Installing ${VSIX_FILE} in VS Code..."
# Some VS Code CLI builds emit Node DEP0169 from their internal url.parse()
# usage. This affects only the CLI child process, not the packaged extension.
NODE_OPTIONS="${NODE_OPTIONS:+$NODE_OPTIONS }--no-deprecation" \
  code --install-extension "${VSIX_FILE}" --force

echo "Done."
