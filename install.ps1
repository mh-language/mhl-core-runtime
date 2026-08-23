# Installs the mhl runtime and, if VS Code is present, the mhl-language
# extension. Usage:
#   irm https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.ps1 | iex
#
# Env overrides:
#   $env:MHL_VERSION      release tag to install (default: latest)
#   $env:MHL_BASE_URL     base URL releases are downloaded from
#                         (default: https://github.com/mh-language/mhl-core-runtime/releases/download)
#   $env:MHL_INSTALL_DIR  where the binary is placed (default: $env:LOCALAPPDATA\mhl\bin)

$ErrorActionPreference = "Stop"

$Repo = "mh-language/mhl-core-runtime"
$BaseUrl = if ($env:MHL_BASE_URL) { $env:MHL_BASE_URL } else { "https://github.com/$Repo/releases/download" }
$InstallDir = if ($env:MHL_INSTALL_DIR) { $env:MHL_INSTALL_DIR } else { "$env:LOCALAPPDATA\mhl\bin" }

function Info($msg) { Write-Host "mhl-install: $msg" }
function Die($msg) { Write-Error "mhl-install: error: $msg"; exit 1 }

if ($env:PROCESSOR_ARCHITECTURE -ne "AMD64") {
  Die "unsupported architecture: $env:PROCESSOR_ARCHITECTURE (only windows-amd64 is published)"
}

$Version = $env:MHL_VERSION
if (-not $Version) {
  Info "resolving latest release..."
  $release = Invoke-RestMethod -UseBasicParsing "https://api.github.com/repos/$Repo/releases/latest"
  $Version = $release.tag_name
  if (-not $Version) { Die "could not resolve latest release version" }
}

$Archive = "mhl-$Version-windows-amd64.zip"
$WorkDir = Join-Path $env:TEMP ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $WorkDir | Out-Null

try {
  Info "downloading $Archive ($Version)..."
  Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/$Version/$Archive" -OutFile (Join-Path $WorkDir $Archive)
  Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/$Version/checksums.txt" -OutFile (Join-Path $WorkDir "checksums.txt")

  Info "verifying checksum..."
  $checksums = Get-Content (Join-Path $WorkDir "checksums.txt")
  $expectedLine = $checksums | Where-Object { $_ -match [regex]::Escape($Archive) + '$' }
  if (-not $expectedLine) { Die "no checksum entry found for $Archive" }
  $expected = ($expectedLine -split '\s+')[0]
  $actual = (Get-FileHash -Algorithm SHA256 (Join-Path $WorkDir $Archive)).Hash.ToLower()
  if ($expected -ne $actual) { Die "checksum mismatch for $Archive (expected $expected, got $actual)" }

  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  Expand-Archive -Path (Join-Path $WorkDir $Archive) -DestinationPath $WorkDir -Force
  Move-Item -Force (Join-Path $WorkDir "mhl.exe") (Join-Path $InstallDir "mhl.exe")
  Info "installed mhl to $InstallDir\mhl.exe"

  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  if (($userPath -split ";") -notcontains $InstallDir) {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$InstallDir", "User")
    $env:Path = "$env:Path;$InstallDir"
    Info "added $InstallDir to your User PATH (restart your terminal to pick it up in new windows)"
  }

  $vsixLine = $checksums | Where-Object { $_ -match 'mhl-language-\S+\.vsix$' } | Select-Object -First 1
  if ($vsixLine) {
    $vsix = ($vsixLine -split '\s+')[1]
    Info "downloading VS Code extension ($vsix)..."
    Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/$Version/$vsix" -OutFile (Join-Path $WorkDir $vsix)

    $vsixExpected = ($vsixLine -split '\s+')[0]
    $vsixActual = (Get-FileHash -Algorithm SHA256 (Join-Path $WorkDir $vsix)).Hash.ToLower()
    if ($vsixExpected -ne $vsixActual) {
      Info "warning: checksum mismatch for $vsix, skipping extension install"
    } elseif (Get-Command code -ErrorAction SilentlyContinue) {
      code --install-extension (Join-Path $WorkDir $vsix) --force
      Info "installed the mhl VS Code extension"
    } else {
      $dest = Join-Path "$env:USERPROFILE\Downloads" $vsix
      New-Item -ItemType Directory -Force -Path "$env:USERPROFILE\Downloads" | Out-Null
      Copy-Item (Join-Path $WorkDir $vsix) $dest
      Info "VS Code CLI ('code') not found on PATH; saved the extension to $dest"
      Info "install it manually via VS Code: Extensions -> ... -> Install from VSIX..."
    }
  } else {
    Info "no VS Code extension found in this release; skipping"
  }

  Info "done. verify with: $InstallDir\mhl.exe (prints usage)"
} finally {
  Remove-Item -Recurse -Force $WorkDir -ErrorAction SilentlyContinue
}
