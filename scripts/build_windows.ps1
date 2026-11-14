$ErrorActionPreference = "Stop"

# Get version from git
try {
    $gitVersion = git describe --tags --always
    # Strip leading 'v'
    $version = $gitVersion -replace "^v", ""
} catch {
    Write-Host "Warning: Could not get version from git, using default 0.0.0"
    $version = "0.0.0"
}

Write-Host "Building version: $version"

# Create dist and assets directories
New-Item -ItemType Directory -Force -Path dist | Out-Null
New-Item -ItemType Directory -Force -Path assets | Out-Null

# Convert icon (requires ImageMagick)
if (Get-Command magick -ErrorAction SilentlyContinue) {
    Write-Host "Converting icon..."
    magick convert "internal/myhome/ui/static/penates.svg" -define icon:auto-resize=256,128,64,48,32,16 "assets/penates.ico"
} else {
    Write-Host "Warning: ImageMagick (magick) not found. Skipping icon conversion."
}

# Build Go binary
Write-Host "Building Windows executable..."
$ldflags = "-X github.com/asnowfix/home-automation/pkg/version.Version=$version"
go build -ldflags $ldflags -o myhome.exe ./myhome

# Build Installer (WiX)
# MSI Version must be strictly numeric (max 4 dot-separated fields) — strip any
# git-describe suffix (e.g. "0.11.3-2-gabcdef" -> "0.11.3").
if ($version -match '^(\d+\.\d+\.\d+)') {
    $msiVersion = $matches[1]
} else {
    $msiVersion = "0.0.0"
}

if (-not (Get-Command wix -ErrorAction SilentlyContinue)) {
    # dotnet tool installs (wix, and dotnet.exe itself on a fresh install) may not be
    # on PATH yet in the current session even though the tool is installed.
    $env:Path = "C:\Program Files\dotnet;$env:USERPROFILE\.dotnet\tools;$env:Path"
}

if (Get-Command wix -ErrorAction SilentlyContinue) {
    Write-Host "Building Installer (MSI version $msiVersion)..."
    wix build myhome.wxs -arch x64 -ext WixToolset.Util.wixext -d "Version=$msiVersion" -o "dist\myhome-$msiVersion.msi"
} else {
    Write-Host "Error: 'wix' not found. Install with:"
    Write-Host "  dotnet tool install --global wix --version 5.0.2"
    Write-Host "  wix extension add WixToolset.Util.wixext/5.0.2"
    exit 1
}

Write-Host "Done. Installer created in dist/"
