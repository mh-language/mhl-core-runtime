# Uninstalls the mhl runtime and the VS Code extension installed by install.ps1.
# Usage:
#   irm https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/uninstall.ps1 | iex
#   .\uninstall.ps1 -Purge

param(
  [switch]$Purge,
  [switch]$KeepVSCode
)

$ErrorActionPreference = "Stop"

$InstallDir = if ($env:MHL_INSTALL_DIR) { $env:MHL_INSTALL_DIR } else { "$env:LOCALAPPDATA\mhl\bin" }
$InstallRoot = Join-Path $env:LOCALAPPDATA "mhl"
$UserDataDir = Join-Path $env:USERPROFILE ".mhl"

function Info($msg) { Write-Host "mhl-uninstall: $msg" }
function Same-Path($left, $right) {
  if (-not $left -or -not $right) { return $false }
  return $left.TrimEnd([char[]]"\/") -ieq $right.TrimEnd([char[]]"\/")
}
function Remove-EmptyDirectory($path) {
  if ((Test-Path -LiteralPath $path -PathType Container) -and
      -not (Get-ChildItem -Force -LiteralPath $path | Select-Object -First 1)) {
    Remove-Item -LiteralPath $path
  }
}

$Binary = Join-Path $InstallDir "mhl.exe"
if (Test-Path -LiteralPath $Binary) {
  Remove-Item -Force -LiteralPath $Binary
  Info "removed $Binary"
} else {
  Info "runtime not found at $Binary; nothing to remove"
}

$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath) {
  $PathEntries = @($UserPath -split ';' | Where-Object { $_ -and -not (Same-Path $_ $InstallDir) })
  $NewUserPath = $PathEntries -join ';'
  if ($NewUserPath -ne $UserPath) {
    [Environment]::SetEnvironmentVariable("Path", $NewUserPath, "User")
    $env:Path = (@($env:Path -split ';' | Where-Object { $_ -and -not (Same-Path $_ $InstallDir) })) -join ';'
    Info "removed $InstallDir from your User PATH"
  }
}

if (-not $KeepVSCode) {
  if (Get-Command code -ErrorAction SilentlyContinue) {
    & code --uninstall-extension mhl-language.mhl-language *> $null
    if ($LASTEXITCODE -eq 0) {
      Info "uninstalled the MHL VS Code extension"
    } else {
      Info "MHL VS Code extension was not installed (or VS Code could not remove it)"
    }
  } else {
    Info "VS Code CLI ('code') not found; skipping extension removal"
  }
}

if ($Purge) {
  foreach ($Path in @($InstallRoot, $UserDataDir)) {
    if (-not $Path -or (Same-Path $Path $env:LOCALAPPDATA) -or (Same-Path $Path $env:USERPROFILE)) {
      throw "mhl-uninstall: refusing to purge unsafe path: $Path"
    }
    if (Test-Path -LiteralPath $Path) {
      Remove-Item -Recurse -Force -LiteralPath $Path
      Info "purged user-wide MHL data from $Path"
    }
  }
} else {
  Remove-EmptyDirectory $InstallDir
  Remove-EmptyDirectory $InstallRoot
}

Info "done. Open a new terminal for the PATH change to take effect."
