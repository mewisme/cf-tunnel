$ErrorActionPreference = 'Stop'

$Repo = 'mewisme/cf-tunnel'
$Binary = 'cf-tunnel'
$Uninstall = $args -contains '--uninstall' -or $args -contains '-Uninstall'
$UnknownArgs = @($args | Where-Object { $_ -notin @('--uninstall', '-Uninstall') })
if ($UnknownArgs.Count -gt 0) {
    throw "cf-tunnel installer: unknown option: $($UnknownArgs[0])"
}
$IsWindowsPlatform = $env:OS -eq 'Windows_NT'
if (Get-Variable -Name IsWindows -ErrorAction SilentlyContinue) {
    $IsWindowsPlatform = $IsWindows
}
if (-not $IsWindowsPlatform) {
    throw 'cf-tunnel installer: install.ps1 supports Windows only'
}

$InstallDir = if ($env:CF_TUNNEL_INSTALL_DIR) {
    $env:CF_TUNNEL_INSTALL_DIR
} else {
    Join-Path $env:LOCALAPPDATA 'Programs\cf-tunnel\bin'
}
$PathMarker = Join-Path $InstallDir '.cf-tunnel-path-managed'
$BinaryPath = Join-Path $InstallDir "$Binary.exe"

if ($Uninstall) {
    if (-not (Test-Path $BinaryPath)) {
        Write-Host "cf-tunnel is not installed at $BinaryPath"
        exit 0
    }

    $ManagedPath = Test-Path $PathMarker
    Remove-Item -Force $BinaryPath
    if ($ManagedPath) {
        $UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $PathEntries = @($UserPath -split ';' | Where-Object { $_ -and $_ -ne $InstallDir })
        [Environment]::SetEnvironmentVariable('Path', ($PathEntries -join ';'), 'User')
        $env:Path = (($env:Path -split ';' | Where-Object { $_ -and $_ -ne $InstallDir }) -join ';')
        Remove-Item -Force $PathMarker -ErrorAction SilentlyContinue
    }
    if ((Test-Path $InstallDir) -and -not (Get-ChildItem -Force $InstallDir | Select-Object -First 1)) {
        Remove-Item -Force $InstallDir
    }
    Write-Host "Uninstalled cf-tunnel from $BinaryPath"
    exit 0
}

if ($PSVersionTable.PSVersion.Major -lt 6) {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
}

$Architecture = try {
    [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
} catch {
    [string]$env:PROCESSOR_ARCHITECTURE.ToLowerInvariant()
}
$Arch = switch ($Architecture) {
    { $_ -in @('x64', 'amd64') } { 'amd64' }
    'arm64' { 'arm64' }
    default { throw "cf-tunnel installer: unsupported architecture: $Architecture" }
}

$Headers = @{ 'User-Agent' = 'cf-tunnel-installer' }
$Release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers $Headers
$Tag = [string]$Release.tag_name
if ([string]::IsNullOrWhiteSpace($Tag)) {
    throw 'cf-tunnel installer: could not determine the latest release tag'
}
$Version = $Tag.TrimStart('v')
$Asset = "${Binary}_${Version}_windows_${Arch}.zip"
$BaseUrl = "https://github.com/$Repo/releases/download/$Tag"
$TempDir = Join-Path ([IO.Path]::GetTempPath()) ("cf-tunnel-" + [guid]::NewGuid().ToString('N'))

try {
    New-Item -ItemType Directory -Path $TempDir | Out-Null
    $Archive = Join-Path $TempDir $Asset
    $Checksums = Join-Path $TempDir 'checksums.txt'

    Write-Host "Installing cf-tunnel $Version (windows/$Arch)"
    Invoke-WebRequest -Uri "$BaseUrl/$Asset" -Headers $Headers -OutFile $Archive
    Invoke-WebRequest -Uri "$BaseUrl/checksums.txt" -Headers $Headers -OutFile $Checksums

    $Expected = $null
    foreach ($Line in Get-Content $Checksums) {
        $Parts = $Line -split '\s+', 2
        if ($Parts.Count -eq 2 -and $Parts[1].TrimStart('*') -eq $Asset) {
            $Expected = $Parts[0].ToLowerInvariant()
            break
        }
    }
    if (-not $Expected) {
        throw "cf-tunnel installer: checksum for $Asset was not found"
    }

    $Actual = (Get-FileHash -Algorithm SHA256 -Path $Archive).Hash.ToLowerInvariant()
    if ($Actual -ne $Expected) {
        throw "cf-tunnel installer: checksum verification failed for $Asset"
    }

    Expand-Archive -Path $Archive -DestinationPath $TempDir -Force
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Copy-Item -Force -Path (Join-Path $TempDir "$Binary.exe") -Destination (Join-Path $InstallDir "$Binary.exe")

    $UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $PathEntries = @($UserPath -split ';' | Where-Object { $_ })
    if ($PathEntries -notcontains $InstallDir) {
        $NewUserPath = if ([string]::IsNullOrWhiteSpace($UserPath)) { $InstallDir } else { "$UserPath;$InstallDir" }
        [Environment]::SetEnvironmentVariable('Path', $NewUserPath, 'User')
        New-Item -ItemType File -Force -Path $PathMarker | Out-Null
    }
    if (($env:Path -split ';') -notcontains $InstallDir) {
        $env:Path = "$InstallDir;$env:Path"
    }

    Write-Host "Installed $Binary to $(Join-Path $InstallDir "$Binary.exe")"
} finally {
    if (Test-Path $TempDir) {
        Remove-Item -Recurse -Force $TempDir
    }
}