# Exercises the Windows installer against a locally-built, checksummed release.
# The user's original PATH is restored even if the smoke test fails.

$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
$SmokeRoot = Join-Path $env:RUNNER_TEMP "mhl-installer-smoke-$PID"
$SmokeTag = "v0.0.0-ci"
$SmokeVersion = $SmokeTag -replace '^v', ''
$AssetRoot = Join-Path $SmokeRoot "releases"
$ReleaseDir = Join-Path $AssetRoot $SmokeTag
$StageDir = Join-Path $SmokeRoot "stage"
$InstallDir = Join-Path $SmokeRoot "install\bin"
$ArchiveName = "mhl-$SmokeVersion-windows-amd64.zip"
$ArchivePath = Join-Path $ReleaseDir $ArchiveName
$OriginalUserPath = [Environment]::GetEnvironmentVariable("Path", "User")
$Server = $null

try {
  New-Item -ItemType Directory -Force -Path $ReleaseDir, $StageDir, $InstallDir | Out-Null

  foreach ($ScriptPath in @((Join-Path $RepoRoot "install.ps1"), (Join-Path $RepoRoot "uninstall.ps1"))) {
    $Tokens = $null
    $ParseErrors = $null
    [System.Management.Automation.Language.Parser]::ParseFile($ScriptPath, [ref]$Tokens, [ref]$ParseErrors) | Out-Null
    if ($ParseErrors.Count -gt 0) {
      throw "PowerShell parse error in ${ScriptPath}: $($ParseErrors -join '; ')"
    }
  }

  Push-Location (Join-Path $RepoRoot "src/mhl-runtime")
  try {
    & go build -trimpath `
      -ldflags "-s -w -X github.com/mh-language/mhl-core-runtime/internal/cli.Version=$SmokeVersion" `
      -o (Join-Path $StageDir "mhl.exe") ./cmd/mhl
    if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
  } finally {
    Pop-Location
  }

  Compress-Archive -Path (Join-Path $StageDir "mhl.exe") -DestinationPath $ArchivePath
  $ArchiveHash = (Get-FileHash -Algorithm SHA256 $ArchivePath).Hash.ToLower()
  "$ArchiveHash  $ArchiveName" | Set-Content -Encoding ascii -LiteralPath (Join-Path $ReleaseDir "checksums.txt")

  $Server = Start-Process -FilePath "python" `
    -ArgumentList @("-m", "http.server", "8765", "--bind", "127.0.0.1", "--directory", $AssetRoot) `
    -RedirectStandardOutput (Join-Path $SmokeRoot "http.out.log") `
    -RedirectStandardError (Join-Path $SmokeRoot "http.err.log") `
    -PassThru

  $ServerReady = $false
  foreach ($Attempt in 1..30) {
    try {
      Invoke-WebRequest -UseBasicParsing -TimeoutSec 1 `
        -Uri "http://127.0.0.1:8765/$SmokeTag/checksums.txt" | Out-Null
      $ServerReady = $true
      break
    } catch {
      Start-Sleep -Milliseconds 200
    }
  }
  if (-not $ServerReady) { throw "local artifact server did not start" }

  $env:MHL_VERSION = $SmokeTag
  $env:MHL_BASE_URL = "http://127.0.0.1:8765"
  $env:MHL_INSTALL_DIR = $InstallDir
  & (Join-Path $RepoRoot "install.ps1")

  $InstalledBinary = Join-Path $InstallDir "mhl.exe"
  $InstalledVersion = & $InstalledBinary version
  if ($LASTEXITCODE -ne 0 -or $InstalledVersion -notmatch [regex]::Escape($SmokeVersion)) {
    throw "installed binary reported unexpected version: $InstalledVersion"
  }

  & (Join-Path $RepoRoot "uninstall.ps1") -KeepVSCode
  if (Test-Path -LiteralPath $InstalledBinary) {
    throw "uninstaller left the runtime behind"
  }

  Write-Host "installer smoke passed for windows-amd64"
} finally {
  if ($Server -and -not $Server.HasExited) {
    Stop-Process -Id $Server.Id -Force -ErrorAction SilentlyContinue
  }
  [Environment]::SetEnvironmentVariable("Path", $OriginalUserPath, "User")
  if (Test-Path -LiteralPath $SmokeRoot) {
    Remove-Item -Recurse -Force -LiteralPath $SmokeRoot
  }
}
