# Build script for Windows PowerShell / PowerShell 7.
#
#   .\build.ps1            build for this machine -> build\resume-generator.exe
#   .\build.ps1 all        cross-compile linux, windows and macOS binaries into build\
#   .\build.ps1 test       run gofmt check, go vet and go test
param([string]$Target = "")
$ErrorActionPreference = "Stop"

$Root = $PSScriptRoot
$Code = Join-Path $Root "code"
$Out  = Join-Path $Root "build"
$Name = "resume-generator"
$Version = (git -C $Root describe --tags --always --dirty 2>$null)
if (-not $Version) { $Version = "dev" }
$LdFlags = "-s -w -X main.version=$Version"

New-Item -ItemType Directory -Force $Out | Out-Null
$env:CGO_ENABLED = "0"

function Build-One($goos, $goarch, $output) {
    Write-Host "building $output"
    $env:GOOS = $goos
    $env:GOARCH = $goarch
    Push-Location $Code
    try {
        go build -trimpath -ldflags $LdFlags -o $output ./cmd/server
        if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    } finally {
        Pop-Location
        Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
    }
}

switch ($Target) {
    "" {
        Push-Location $Code
        $goos = go env GOOS; $goarch = go env GOARCH
        Pop-Location
        $ext = if ($goos -eq "windows") { ".exe" } else { "" }
        Build-One $goos $goarch (Join-Path $Out "$Name$ext")
    }
    "all" {
        Build-One linux   amd64 (Join-Path $Out "$Name-linux-amd64")
        Build-One linux   arm64 (Join-Path $Out "$Name-linux-arm64")
        Build-One windows amd64 (Join-Path $Out "$Name-windows-amd64.exe")
        Build-One darwin  amd64 (Join-Path $Out "$Name-darwin-amd64")
        Build-One darwin  arm64 (Join-Path $Out "$Name-darwin-arm64")
    }
    "test" {
        Push-Location $Code
        try {
            $unformatted = gofmt -l .
            if ($unformatted) { Write-Host "gofmt needed:"; $unformatted; exit 1 }
            go vet ./...
            if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
            go test ./...
            if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        } finally { Pop-Location }
    }
    default { Write-Error "usage: .\build.ps1 [all|test]"; exit 2 }
}
