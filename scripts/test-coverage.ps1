$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$tempDir = Join-Path $env:TEMP "scriptorium-cover"
$mergedProfile = Join-Path $root "coverage.out"

Push-Location $root
try {
  Remove-Item -Recurse -Force $tempDir -ErrorAction SilentlyContinue
  New-Item -ItemType Directory -Path $tempDir | Out-Null
  "mode: atomic" | Set-Content $mergedProfile

  $packages = go list ./...
  foreach ($pkg in $packages) {
    $safeName = ($pkg -replace "[^A-Za-z0-9._-]", "_")
    $packageProfile = Join-Path $tempDir ($safeName + ".out")
    Remove-Item $packageProfile -ErrorAction SilentlyContinue
    go test $pkg -covermode=atomic "-coverprofile=$packageProfile" "-outputdir=$tempDir"
    if (Test-Path $packageProfile) {
      Get-Content $packageProfile | Select-Object -Skip 1 | Add-Content $mergedProfile
    }
  }

  cmd /c "go tool cover -func=$mergedProfile"
}
finally {
  Pop-Location
}
