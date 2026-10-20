# Build Windows / Linux binaries on Windows.  Usage: .\build.ps1
# (ASCII only: Windows PowerShell 5.1 misreads BOM-less UTF-8 scripts)
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$go = Join-Path $root 'tools\go\bin\go.exe'
if (-not (Test-Path $go)) { $go = 'go' }
$dist = Join-Path $root 'dist'
New-Item -ItemType Directory -Force $dist | Out-Null

$targets = @(
  @{ os = 'windows'; arch = 'amd64'; out = 'aprsigate-windows-x64.exe' },
  @{ os = 'linux';   arch = 'amd64'; out = 'aprsigate-linux-x64' },
  @{ os = 'linux';   arch = 'arm64'; out = 'aprsigate-linux-arm64' },
  @{ os = 'linux';   arch = 'arm';   out = 'aprsigate-linux-armv7'; arm = '7' },
  @{ os = 'linux';   arch = 'arm';   out = 'aprsigate-linux-armv6'; arm = '6' }
)

Push-Location (Join-Path $root 'src')
try {
  & $go test ./...
  if ($LASTEXITCODE -ne 0) { throw 'tests failed' }
  foreach ($t in $targets) {
    $env:GOOS = $t.os; $env:GOARCH = $t.arch; $env:CGO_ENABLED = '0'
    if ($t.arm) { $env:GOARM = $t.arm } else { Remove-Item Env:GOARM -ErrorAction SilentlyContinue }
    Write-Host "build $($t.out)"
    & $go build -trimpath -ldflags '-s -w' -o (Join-Path $dist $t.out) .
    if ($LASTEXITCODE -ne 0) { throw "build failed: $($t.out)" }
  }
} finally {
  Remove-Item Env:GOOS, Env:GOARCH, Env:GOARM, Env:CGO_ENABLED -ErrorAction SilentlyContinue
  Pop-Location
}
Get-ChildItem $dist | Select-Object Name, Length
