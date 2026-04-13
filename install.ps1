<#
.SYNOPSIS
    Install pgcompare from GitHub Releases.

.PARAMETER BinDir
    Installation directory (default: $HOME\.local\bin).

.PARAMETER Version
    Release tag to install (default: latest, e.g. v1.0.1).

.EXAMPLE
    irm https://raw.githubusercontent.com/pg-tools/pgcompare/main/install.ps1 | iex

.EXAMPLE
    .\install.ps1 -Version v1.0.1

.EXAMPLE
    .\install.ps1 -BinDir C:\tools
#>
param(
    [string]$BinDir = "$HOME\.local\bin",
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"

$ProjectName = "pgcompare"
$GithubRepo  = "pg-tools/pgcompare"

function Err($msg) {
    Write-Error "error: $msg"
    exit 1
}

function Fetch-LatestVersion {
    $url = "https://api.github.com/repos/$GithubRepo/releases/latest"
    $headers = @{}
    if ($env:GITHUB_TOKEN) {
        $headers["Authorization"] = "Bearer $env:GITHUB_TOKEN"
    }
    $release = Invoke-RestMethod -Uri $url -Headers $headers
    return $release.tag_name
}

if (-not $Version) {
    $Version = Fetch-LatestVersion
    if (-not $Version) { Err "failed to resolve latest release tag" }
}

$Asset   = "${ProjectName}_windows_amd64.zip"
$BaseUrl = "https://github.com/$GithubRepo/releases/download/$Version"

$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $TmpDir -Force | Out-Null

try {
    $ArchivePath   = Join-Path $TmpDir $Asset
    $ChecksumsPath = Join-Path $TmpDir "checksums.txt"

    Write-Host "Installing $ProjectName $Version for windows/amd64..."

    Invoke-WebRequest -Uri "$BaseUrl/checksums.txt" -OutFile $ChecksumsPath -UseBasicParsing
    Invoke-WebRequest -Uri "$BaseUrl/$Asset" -OutFile $ArchivePath -UseBasicParsing

    $expectedSha = (Get-Content $ChecksumsPath | Where-Object { $_ -match $Asset } | ForEach-Object { ($_ -split '\s+')[0] }) | Select-Object -First 1
    if (-not $expectedSha) { Err "checksum for $Asset not found in checksums.txt" }

    $actualSha = (Get-FileHash -Path $ArchivePath -Algorithm SHA256).Hash.ToLower()
    if ($actualSha -ne $expectedSha) { Err "checksum mismatch for $Asset" }

    Expand-Archive -Path $ArchivePath -DestinationPath $TmpDir -Force

    $Binary = Get-ChildItem -Path $TmpDir -Recurse -Filter "$ProjectName.exe" | Select-Object -First 1
    if (-not $Binary) { Err "binary $ProjectName.exe not found in archive" }

    if (-not (Test-Path $BinDir)) {
        New-Item -ItemType Directory -Path $BinDir -Force | Out-Null
    }

    Copy-Item -Path $Binary.FullName -Destination (Join-Path $BinDir "$ProjectName.exe") -Force

    Write-Host "Installed $ProjectName to $BinDir\$ProjectName.exe"

    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($userPath -notlike "*$BinDir*") {
        Write-Host ""
        Write-Host "Add $BinDir to your PATH:"
        Write-Host "  [Environment]::SetEnvironmentVariable('Path', `"$BinDir;`$env:Path`", 'User')"
    }
}
finally {
    Remove-Item -Path $TmpDir -Recurse -Force -ErrorAction SilentlyContinue
}
