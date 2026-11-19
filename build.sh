#!/usr/bin/env bash
set -e

VERSION="${1:-v0.1.0-beta}"

echo "=========================================="
echo " Building MouseBatteryTray $VERSION (Pure Go Win32)"
echo "=========================================="

export CGO_ENABLED=0
export GOOS=windows
export GOARCH=amd64

OUTPUT_PATH="mouse-battery.exe"

echo "Compiling with -H=windowsgui -s -w -X main.Version=$VERSION..."
go build -ldflags="-H=windowsgui -s -w -X main.Version=$VERSION" -o "$OUTPUT_PATH" .

if [ -f "$OUTPUT_PATH" ]; then
    SIZE=$(ls -lh "$OUTPUT_PATH" | awk '{print $5}')
    echo "[OK] Successfully built: $OUTPUT_PATH ($SIZE)"
    echo ""
    echo "Usage on Windows:"
    echo "  ./mouse-battery.exe          -> Run silently in system tray"
    echo "  ./mouse-battery.exe --probe  -> Live console HID packet sniffer & probe"
    echo "  ./mouse-battery.exe --help   -> Show help"
else
    echo "[ERROR] Build failed, output binary not found."
    exit 1
fi
