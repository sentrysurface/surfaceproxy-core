# SurfaceProxy Installer Script for Windows
# Downloads the latest release zip and installs it to LocalAppData

$ErrorActionPreference = "Stop"

$Owner = "sentrysurface"
$Repo = "surfaceproxy-core"

# 1. Detect CPU Architecture
$Arch = $env:PROCESSOR_ARCHITECTURE
switch ($Arch) {
    "AMD64" { $ArchName = "amd64" }
    "ARM64" { $ArchName = "arm64" }
    default {
        Write-Error "Unsupported CPU architecture: $Arch"
    }
}

# 2. Get latest release version from GitHub API
Write-Host "Finding latest version of SurfaceProxy..."
$Version = $null
try {
    $ReleaseUrl = "https://api.github.com/repos/$Owner/$Repo/releases/latest"
    $Response = Invoke-RestMethod -Uri $ReleaseUrl -UseBasicParsing
    $Version = $Response.tag_name
} catch {
    Write-Warning "Could not fetch latest release tag from GitHub API. Defaulting to v0.1.0-alpha."
    $Version = "v0.1.0-alpha"
}

if (-not $Version.StartsWith("v")) {
    $Version = "v$Version"
}

Write-Host "Target version: $Version"
Write-Host "Architecture:   $ArchName"

# 3. Download and Extract Binary
$ZipName = "surface-proxy-$Version-windows-$ArchName.zip"
$DownloadUrl = "https://github.com/$Owner/$Repo/releases/download/$Version/$ZipName"
$InstallDir = Join-Path $env:USERPROFILE "AppData\Local\Programs\SurfaceProxy"
$TempDir = Join-Path $env:TEMP "SurfaceProxyInstall-$((Get-Date).Ticks)"

New-Item -ItemType Directory -Path $TempDir -Force | Out-Null
$ZipPath = Join-Path $TempDir $ZipName

Write-Host "Downloading from $DownloadUrl..."
try {
    Invoke-WebRequest -Uri $DownloadUrl -OutFile $ZipPath -UseBasicParsing
} catch {
    Write-Error "Failed to download $ZipName. Please check your internet connection or if the release version exists."
}

Write-Host "Extracting to $InstallDir..."
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
Expand-Archive -Path $ZipPath -DestinationPath $InstallDir -Force

# Clean up temp files
Remove-Item -Recurse -Force $TempDir

# 4. Add to User Path Environment Variable
Write-Host "Updating PATH environment variable..."
$UserPath = [Environment]::GetEnvironmentVariable("Path", [EnvironmentVariableTarget]::User)
if ($UserPath -split ";" -notcontains $InstallDir) {
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", [EnvironmentVariableTarget]::User)
    # Update current session path
    $env:Path = "$env:Path;$InstallDir"
    Write-Host "✓ Added SurfaceProxy to User PATH."
} else {
    Write-Host "✓ SurfaceProxy is already in PATH."
}

# 5. Run configuration initialization
Write-Host "Initializing configuration..."
$ExePath = Join-Path $InstallDir "surface-proxy.exe"
if (Test-Path $ExePath) {
    # Run the init command to register the MCP server with Cursor
    Start-Process -FilePath $ExePath -ArgumentList "init", "--cursor" -NoNewWindow -Wait
} else {
    Write-Warning "Could not locate surface-proxy.exe in the installation directory."
}

Write-Host "`n================================================================="
Write-Host " 🎉 SurfaceProxy has been successfully installed to:"
Write-Host "    $InstallDir"
Write-Host "================================================================="
Write-Host "`nPlease restart your terminal or editor (Cursor/VS Code) to apply the path changes."
Write-Host "You can now run:"
Write-Host "  - 'surface-proxy' to start the CLI proxy daemon."
Write-Host "  - 'surface-proxy-tray' to start the system tray daemon."
Write-Host ""
