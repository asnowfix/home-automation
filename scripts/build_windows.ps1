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

# Build Installer
$iscc = "C:\Program Files (x86)\Inno Setup 6\ISCC.exe"
if (Test-Path $iscc) {
    Write-Host "Building Installer..."
    & $iscc "/DMyAppVersion=$version" myhome.iss
} else {
    Write-Host "Error: ISCC.exe not found at $iscc"
    exit 1
}

Write-Host "Done. Installer created in dist/"
