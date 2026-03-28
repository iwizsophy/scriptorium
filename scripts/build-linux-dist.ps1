param(
    [Parameter(Mandatory = $true)]
    [string]$Version,

    [Parameter(Mandatory = $true)]
    [ValidateSet("amd64", "arm64")]
    [string]$Arch,

    [string]$OutputDir = "dist"
)

$ErrorActionPreference = "Stop"

function Invoke-NativeCommand {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,

        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$FilePath failed with exit code $LASTEXITCODE"
    }
}

$repoRoot = Split-Path -Parent $PSScriptRoot
$distRoot = Join-Path $repoRoot $OutputDir
$archiveName = "scriptorium_${Version}_linux_${Arch}"
$stagingDir = Join-Path $distRoot $archiveName
$archivePath = Join-Path $distRoot "${archiveName}.tar.gz"
$docsDir = Join-Path $stagingDir "docs"

if (Test-Path -LiteralPath $stagingDir) {
    Remove-Item -LiteralPath $stagingDir -Recurse -Force
}
if (Test-Path -LiteralPath $archivePath) {
    Remove-Item -LiteralPath $archivePath -Force
}

New-Item -ItemType Directory -Path $docsDir -Force | Out-Null

$ldflags = "-s -w -X github.com/silvekt/scriptorium/internal/app.ServerVersion=$Version"
$savedEnv = @{
    CGO_ENABLED = $env:CGO_ENABLED
    GOOS        = $env:GOOS
    GOARCH      = $env:GOARCH
}

try {
    $env:CGO_ENABLED = "0"
    $env:GOOS = "linux"
    $env:GOARCH = $Arch

    Invoke-NativeCommand -FilePath "go" -Arguments @("build", "-trimpath", "-ldflags", $ldflags, "-o", (Join-Path $stagingDir "scriptorium"), "./cmd/scriptorium")
    Invoke-NativeCommand -FilePath "go" -Arguments @("build", "-trimpath", "-ldflags", $ldflags, "-o", (Join-Path $stagingDir "scriptorium-index"), "./cmd/build-docs-index")
    Invoke-NativeCommand -FilePath "go" -Arguments @("build", "-trimpath", "-ldflags", $ldflags, "-o", (Join-Path $stagingDir "scriptorium-snapshot"), "./cmd/build-git-snapshot")
}
finally {
    $env:CGO_ENABLED = $savedEnv.CGO_ENABLED
    $env:GOOS = $savedEnv.GOOS
    $env:GOARCH = $savedEnv.GOARCH
}

Copy-Item -LiteralPath (Join-Path $repoRoot "LICENSE") -Destination $stagingDir
Copy-Item -LiteralPath (Join-Path (Join-Path $repoRoot "docs") "linux-setup.md") -Destination (Join-Path $docsDir "linux-setup.md")
Copy-Item -LiteralPath (Join-Path (Join-Path $repoRoot "docs") "linux-setup.ja.md") -Destination (Join-Path $docsDir "linux-setup.ja.md")

Invoke-NativeCommand -FilePath "tar" -Arguments @("-C", $distRoot, "-czf", $archivePath, $archiveName)
Remove-Item -LiteralPath $stagingDir -Recurse -Force

Write-Host "Created $archivePath"
