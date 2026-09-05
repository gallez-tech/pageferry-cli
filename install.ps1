$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$Repository = "gallez-tech/pageferry-cli"
$ArchitectureName = if ($env:PROCESSOR_ARCHITEW6432) {
    $env:PROCESSOR_ARCHITEW6432
} else {
    $env:PROCESSOR_ARCHITECTURE
}

$Architecture = switch ($ArchitectureName.ToUpperInvariant()) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    default { throw "pageferry: unsupported Windows architecture: $ArchitectureName" }
}

$Version = $env:PAGEFERRY_VERSION
if ([string]::IsNullOrWhiteSpace($Version)) {
    $Release = Invoke-RestMethod `
        -Headers @{ Accept = "application/vnd.github+json" } `
        -Uri "https://api.github.com/repos/$Repository/releases/latest"
    $Version = $Release.tag_name
}
if (-not $Version.StartsWith("v")) {
    $Version = "v$Version"
}

$Asset = "pageferry-windows-$Architecture.msi"
$BaseUrl = "https://github.com/$Repository/releases/download/$Version"
$TemporaryDirectory = Join-Path ([System.IO.Path]::GetTempPath()) ("pageferry-" + [guid]::NewGuid())
$MsiPath = Join-Path $TemporaryDirectory $Asset
$ChecksumsPath = Join-Path $TemporaryDirectory "checksums.txt"

New-Item -ItemType Directory -Path $TemporaryDirectory | Out-Null
try {
    Invoke-WebRequest -Uri "$BaseUrl/$Asset" -OutFile $MsiPath
    Invoke-WebRequest -Uri "$BaseUrl/checksums.txt" -OutFile $ChecksumsPath

    $EscapedAsset = [regex]::Escape($Asset)
    $ChecksumLine = Get-Content $ChecksumsPath | Where-Object {
        $_ -match "^([a-fA-F0-9]{64})\s+\*?$EscapedAsset$"
    } | Select-Object -First 1
    if (-not $ChecksumLine) {
        throw "pageferry: release checksum is missing for $Asset"
    }
    $ExpectedHash = ([regex]::Match($ChecksumLine, "^[a-fA-F0-9]{64}")).Value.ToLowerInvariant()
    $ActualHash = (Get-FileHash -Algorithm SHA256 -Path $MsiPath).Hash.ToLowerInvariant()
    if ($ActualHash -ne $ExpectedHash) {
        throw "pageferry: checksum verification failed"
    }

    Write-Host "Installing PageFerry $($Version.TrimStart('v'))..."
    $Process = Start-Process -FilePath "msiexec.exe" `
        -ArgumentList "/i `"$MsiPath`"" `
        -Verb RunAs `
        -Wait `
        -PassThru
    if ($Process.ExitCode -notin @(0, 1641, 3010)) {
        throw "pageferry: Windows Installer exited with code $($Process.ExitCode)"
    }
    Write-Host "PageFerry $($Version.TrimStart('v')) installed successfully."
    if ($Process.ExitCode -in @(1641, 3010)) {
        Write-Host "Windows reports that a restart is required."
    }
} finally {
    Remove-Item -LiteralPath $TemporaryDirectory -Recurse -Force -ErrorAction SilentlyContinue
}
