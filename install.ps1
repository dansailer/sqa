# Install sqa from GitHub releases (windows/amd64). Windows PowerShell 5.1+ or pwsh.
#
#   irm https://raw.githubusercontent.com/dansailer/sqa/main/install.ps1 | iex
#   $env:SQA_VERSION = 'v0.3.0'; irm ... | iex
#   .\install.ps1 -Version v0.3.0 -NoSkill
#
# Environment:
#   SQA_VERSION, SQA_INSTALL_DIR (default %LOCALAPPDATA%\Programs\sqa), SQA_NO_SKILL=1
#   SQA_DOWNLOAD_BASE, SQA_GITHUB — test hooks

#Requires -Version 5.1
[CmdletBinding()]
param(
    [string]$Version = $env:SQA_VERSION,
    [switch]$NoSkill,
    [switch]$Help
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Repo = 'dansailer/sqa'
$GitHubWeb = $(if ($env:SQA_GITHUB) { $env:SQA_GITHUB } else { 'https://github.com' })

function Show-Usage {
    @'
Install sqa from GitHub releases (windows/amd64).

Usage:
  irm https://raw.githubusercontent.com/dansailer/sqa/main/install.ps1 | iex
  $env:SQA_VERSION = 'v0.3.0'; irm ... | iex
  .\install.ps1 -Version v0.3.0 [-NoSkill]

Installs to $env:SQA_INSTALL_DIR (default %LOCALAPPDATA%\Programs\sqa) and
verifies the archive against checksums.txt. No GitHub login or token is
needed: the latest release is read from the
github.com/dansailer/sqa/releases/latest redirect.

Skip skill install with -NoSkill or SQA_NO_SKILL=1.
'@ | Write-Host
}

function Die {
    param([string]$Message)
    Write-Error "install.ps1: $Message"
    exit 1
}

function Invoke-Download {
    param(
        [Parameter(Mandatory = $true)][string]$Uri,
        [Parameter(Mandatory = $true)][string]$OutFile
    )
    if ($Uri -notmatch '^https?://') {
        Copy-Item -LiteralPath $Uri -Destination $OutFile -Force
        return
    }
    try {
        Invoke-WebRequest -Uri $Uri -OutFile $OutFile -UserAgent 'sqa-install' -UseBasicParsing | Out-Null
    }
    catch {
        throw "install.ps1: download failed for $(Split-Path -Leaf $Uri): $($_.Exception.Message)"
    }
}

function ConvertTo-Tag {
    param([string]$Raw)
    if ($Raw.StartsWith('v')) { return $Raw }
    return "v$Raw"
}

function Get-DownloadBaseUrl {
    param([string]$Base, [string]$Name)
    $Base = $Base.TrimEnd('/', '\')
    if ($Base -match '^[A-Za-z]:[\\/]' -or $Base.StartsWith('\\') -or $Base.StartsWith('/')) {
        return (Join-Path $Base $Name)
    }
    return "$Base/$Name"
}

function Get-LatestTag {
    if ($Version) { return (ConvertTo-Tag $Version) }
    if ($env:SQA_DOWNLOAD_BASE) {
        Die 'set SQA_VERSION or pass -Version when using SQA_DOWNLOAD_BASE'
    }
    # github.com/<repo>/releases/latest redirects to .../releases/tag/<tag>;
    # no API call, so no API rate limit and no token.
    $url = "$GitHubWeb/$Repo/releases/latest"
    try {
        $resp = Invoke-WebRequest -Uri $url -UserAgent 'sqa-install' -UseBasicParsing
    }
    catch {
        Die "could not determine the latest release from $url (pass -Version): $($_.Exception.Message)"
    }
    # Windows PowerShell: HttpWebResponse.ResponseUri; pwsh: HttpResponseMessage.RequestMessage.RequestUri.
    $base = $resp.BaseResponse
    $final = if ($base.PSObject.Properties['ResponseUri']) { $base.ResponseUri } else { $base.RequestMessage.RequestUri }
    if ([string]$final -notmatch '/releases/tag/([^/?#]+)$') {
        Die "could not determine the latest release from $url (pass -Version)"
    }
    return $Matches[1]
}

function Get-ReleaseAssets {
    param([string]$Tag, [string]$Tmp)
    $ver = $Tag.TrimStart('v')
    $archive = "sqa_${ver}_windows_amd64.zip"
    if ($env:SQA_DOWNLOAD_BASE) {
        Invoke-Download -Uri (Get-DownloadBaseUrl $env:SQA_DOWNLOAD_BASE $archive) -OutFile (Join-Path $Tmp $archive)
        Invoke-Download -Uri (Get-DownloadBaseUrl $env:SQA_DOWNLOAD_BASE 'checksums.txt') -OutFile (Join-Path $Tmp 'checksums.txt')
        return $archive
    }
    Invoke-Download -Uri "$GitHubWeb/$Repo/releases/download/$Tag/$archive" -OutFile (Join-Path $Tmp $archive)
    Invoke-Download -Uri "$GitHubWeb/$Repo/releases/download/$Tag/checksums.txt" -OutFile (Join-Path $Tmp 'checksums.txt')
    return $archive
}

function Get-ExpectedSha256 {
    param([string]$SumsPath, [string]$Archive)
    foreach ($line in Get-Content -LiteralPath $SumsPath) {
        if ($line -match '^\s*([0-9A-Fa-f]{64})\s+\*?(\S+)\s*$' -and $Matches[2] -eq $Archive) {
            return $Matches[1].ToLowerInvariant()
        }
    }
    return $null
}

function Assert-Checksum {
    param([string]$Archive, [string]$Tmp)
    $expected = Get-ExpectedSha256 (Join-Path $Tmp 'checksums.txt') $Archive
    if (-not $expected) { Die "checksums.txt has no entry for $Archive" }
    $got = (Get-FileHash -LiteralPath (Join-Path $Tmp $Archive) -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($expected -ne $got) {
        Die "checksum mismatch for $Archive (expected $expected, got $got)"
    }
}

function Install-Binary {
    param([string]$Archive, [string]$Ver, [string]$Tmp, [string]$InstallDir)
    $extract = Join-Path $Tmp 'extract'
    New-Item -ItemType Directory -Path $extract | Out-Null
    Expand-Archive -LiteralPath (Join-Path $Tmp $Archive) -DestinationPath $extract -Force
    $preferred = Join-Path $extract "sqa_${Ver}_windows_amd64\sqa.exe"
    $bin = $null
    if (Test-Path -LiteralPath $preferred) {
        $bin = $preferred
    }
    else {
        $found = @(Get-ChildItem -Path $extract -Recurse -Filter 'sqa.exe' -File)
        if ($found.Count -ne 1) { Die 'archive did not contain exactly one sqa.exe binary' }
        $bin = $found[0].FullName
    }
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Copy-Item -LiteralPath $bin -Destination (Join-Path $InstallDir 'sqa.exe') -Force
}

function Test-OnPath {
    param([string]$Dir)
    $parts = @()
    if ($env:Path) { $parts += $env:Path.Split(';') }
    foreach ($p in $parts) {
        if ($p.TrimEnd('\') -eq $Dir.TrimEnd('\')) { return $true }
    }
    return $false
}

# --- main ---

if ($Help) {
    Show-Usage
    exit 0
}

try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
}
catch {
    # Platform may not expose ServicePointManager (rare on pwsh); continue.
}

$skipSkill = $NoSkill
if ($env:SQA_NO_SKILL -in @('1', 'true', 'yes')) { $skipSkill = $true }

$installDir = $env:SQA_INSTALL_DIR
if (-not $installDir) {
    if (-not $env:LOCALAPPDATA) { Die 'LOCALAPPDATA is not set; set SQA_INSTALL_DIR' }
    $installDir = Join-Path $env:LOCALAPPDATA 'Programs\sqa'
}

$tmp = $null
try {
    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("sqa-install-" + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    $tag = Get-LatestTag
    $ver = $tag.TrimStart('v')
    $archive = Get-ReleaseAssets $tag $tmp
    Assert-Checksum $archive $tmp
    Install-Binary $archive $ver $tmp $installDir
}
finally {
    if ($tmp -and (Test-Path -LiteralPath $tmp)) {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Write-Host "installed sqa $tag to $(Join-Path $installDir 'sqa.exe')"

if (-not (Test-OnPath $installDir)) {
    Write-Host "warning: $installDir is not on PATH. Add it, for example:" -ForegroundColor Yellow
    Write-Host "  [Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User') + ';$installDir', 'User')"
}

if (-not $skipSkill) {
    & (Join-Path $installDir 'sqa.exe') skill install
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
