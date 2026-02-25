param(
    [switch]$Stress
)

$ErrorActionPreference = "Stop"
$root = Join-Path $PSScriptRoot ".."
Set-Location $root

$coverageFile = Join-Path $root "coverage_internal.out"
$coverageMin = 80.0

Write-Host "[1/4] Running default test suite"
go test ./...
if ($LASTEXITCODE -ne 0) {
    throw "default test suite failed"
}

Write-Host "[2/4] Enforcing internal package coverage >= $coverageMin%"
go test ./internal/... -covermode=atomic -coverprofile $coverageFile
if ($LASTEXITCODE -ne 0) {
    throw "coverage run failed (check Go toolchain coverage support in this environment)"
}
$coverOutput = go tool cover -func $coverageFile
if ($LASTEXITCODE -ne 0) {
    throw "go tool cover failed for $coverageFile"
}
$totalLine = $coverOutput | Select-String '^total:'
if (-not $totalLine) {
    throw "failed to parse total coverage from $coverageFile"
}
if ($totalLine -notmatch '([0-9]+(?:\.[0-9]+)?)%') {
    throw "unexpected total coverage format: $totalLine"
}
$totalCoverage = [double]$matches[1]
if ($totalCoverage -lt $coverageMin) {
    throw "coverage threshold failure: got $totalCoverage% want >= $coverageMin% (internal/* only)"
}
Write-Host ("coverage check passed: {0:N2}%" -f $totalCoverage)

Write-Host "[3/4] Running race tests for all packages"
$env:CGO_ENABLED = "1"
go test -race ./...
if ($LASTEXITCODE -ne 0) {
    throw "race test suite failed (ensure C toolchain, e.g., gcc, is installed)"
}

if ($Stress) {
    Write-Host "[4/4] Running stress-tagged tests"
    go test -tags=stress ./...
    if ($LASTEXITCODE -ne 0) {
        throw "stress-tagged tests failed"
    }
} else {
    Write-Host "[4/4] Stress-tagged tests skipped (use -Stress to include)"
}
