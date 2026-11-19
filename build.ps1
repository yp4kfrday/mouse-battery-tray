# Build script for MouseBatteryTray (Pure Go, 0 CGO/MinGW)
param (
    [string]$Version = "v0.1.0-beta"
)

$ErrorActionPreference = "Stop"

Write-Host "==========================================" -ForegroundColor Cyan
Write-Host " Building MouseBatteryTray $Version (Pure Go Win32)" -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan

$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"

$outputPath = "mouse-battery.exe"

Write-Host "Compiling with -H=windowsgui -s -w -X main.Version=$Version..." -ForegroundColor Yellow
go build -ldflags="-H=windowsgui -s -w -X main.Version=$Version" -o $outputPath .
if ($LASTEXITCODE -ne 0) {
    Write-Host "[ERROR] go build failed with exit code $LASTEXITCODE" -ForegroundColor Red
    exit 1
}

if (Test-Path $outputPath) {
    $sizeBytes = (Get-Item $outputPath).Length
    $sizeMB = [math]::Round($sizeBytes / 1MB, 2)
    Write-Host "[OK] Successfully built: $outputPath ($sizeMB MB / $sizeBytes bytes)" -ForegroundColor Green
    Write-Host ""
    Write-Host "Usage:" -ForegroundColor White
    Write-Host "  .\mouse-battery.exe          -> Run silently in system tray" -ForegroundColor Gray
    Write-Host "  .\mouse-battery.exe --probe  -> Live console HID packet sniffer & probe" -ForegroundColor Gray
    Write-Host "  .\mouse-battery.exe --help   -> Show help" -ForegroundColor Gray
} else {
    Write-Host "[ERROR] Build failed, output binary not found." -ForegroundColor Red
    exit 1
}
