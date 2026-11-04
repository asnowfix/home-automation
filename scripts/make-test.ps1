<#
.SYNOPSIS
Windows-native equivalent of `make test`, for machines without GNU Make.

.DESCRIPTION
Mirrors the `generate` and `test` targets in the root Makefile:
1. Runs the required `go generate` steps (UI assets, pool/garden defaults).
2. Runs `go test ./...` at the repo root.
3. Runs `go test ./...` inside every Go workspace module listed in go.work
   that has its own direct test files - mirroring the Makefile's
   `find ... -prune` logic: a module whose only tests live inside a nested
   sub-module (also listed separately in go.work) is skipped here, since
   that sub-module gets its own iteration.

Exits non-zero if any generate step or any module's tests fail.
#>

$ErrorActionPreference = "Stop"
$failed = $false

function Test-HasOwnTestFiles {
    param([string]$ModuleDir)

    $moduleRoot = (Resolve-Path $ModuleDir).Path
    $testFiles = Get-ChildItem -Path $ModuleDir -Filter "*_test.go" -File -Recurse -ErrorAction SilentlyContinue

    foreach ($f in $testFiles) {
        $dir = Split-Path $f.FullName -Parent
        $ownedByNestedModule = $false
        while ($dir -and ($dir -ne $moduleRoot)) {
            if (Test-Path (Join-Path $dir "go.mod")) {
                $ownedByNestedModule = $true
                break
            }
            $dir = Split-Path $dir -Parent
        }
        if (-not $ownedByNestedModule) {
            return $true
        }
    }
    return $false
}

Write-Host "=== go generate ===" -ForegroundColor Cyan
go generate ./internal/myhome/ui/...
if ($LASTEXITCODE -ne 0) { $failed = $true }
go generate ./myhome/ctl/pool
if ($LASTEXITCODE -ne 0) { $failed = $true }
go generate ./myhome/ctl/garden
if ($LASTEXITCODE -ne 0) { $failed = $true }
go generate ./...
if ($LASTEXITCODE -ne 0) { $failed = $true }

if ($failed) {
    Write-Host "`ngo generate failed - aborting before running tests." -ForegroundColor Red
    exit 1
}

Write-Host "`n=== Testing root module ===" -ForegroundColor Cyan
go test ./...
if ($LASTEXITCODE -ne 0) { $failed = $true }

# Workspace modules are listed in go.work as tab-indented "./path" lines.
$moduleDirs = Get-Content go.work | Where-Object { $_ -match '^\s+\.\/(.+)$' } | ForEach-Object { $matches[1] }

foreach ($dir in $moduleDirs) {
    if (Test-HasOwnTestFiles -ModuleDir $dir) {
        Write-Host "`n=== Testing $dir ===" -ForegroundColor Cyan
        Push-Location $dir
        go test ./...
        if ($LASTEXITCODE -ne 0) { $failed = $true }
        Pop-Location
    }
}

if ($failed) {
    Write-Host "`nFAILED: one or more modules had test failures." -ForegroundColor Red
    exit 1
} else {
    Write-Host "`nAll tests passed." -ForegroundColor Green
    exit 0
}
