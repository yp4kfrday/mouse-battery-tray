package main

import (
	"flag"
	"fmt"
	"os"

	"mouse-battery-tray/drivers"
	"mouse-battery-tray/logger"
)

func main() {
	// Silently remove any leftover .old binary from a previous update
	CleanOldBinary()

	probeFlag := flag.Bool("probe", false, "Scan connected mice, send probe packets, and stream live HID packets to console")
	scanFlag := flag.Bool("scan", false, "Alias for --probe")
	onceFlag := flag.Bool("once", false, "Probe once and exit immediately without streaming")
	versionFlag := flag.Bool("version", false, "Print application version and exit")
	helpFlag := flag.Bool("help", false, "Show help and command-line usage")

	flag.Parse()

	if *versionFlag {
		InitConsoleOutput()
		fmt.Printf("MouseBatteryTray %s\n", Version)
		os.Exit(0)
	}

	logger.Log("==================================================")
	logger.Log("MouseBatteryTray %s invoked (PID: %d, args: %v)", Version, os.Getpid(), os.Args[1:])
	logger.Log("Log file location: %s", logger.LogFilePath())

	if *helpFlag {
		InitConsoleOutput()
		fmt.Printf("MouseBatteryTray %s - Wireless Mouse Battery Monitor for Windows Tray\n", Version)
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Println("  mouse-battery.exe          Run silently in Windows system tray")
		fmt.Println("  mouse-battery.exe --probe  Scan devices and monitor live raw HID packets")
		fmt.Println("  mouse-battery.exe --probe --once  Probe once and exit")
		fmt.Println("  mouse-battery.exe --scan   Alias for --probe")
		fmt.Println("  mouse-battery.exe --version Print version")
		fmt.Println("  mouse-battery.exe --help   Show this help message")
		os.Exit(0)
	}

	mgr := drivers.NewManager()

	if *probeFlag || *scanFlag {
		RunProbe(mgr, *onceFlag)
		return
	}

	// Normal System Tray mode
	logger.Log("==================================================")
	logger.Log("MouseBatteryTray %s starting in system tray mode (PID: %d)", Version, os.Getpid())
	logger.Log("Log file location: %s", logger.LogFilePath())

	app := NewTrayApp(mgr)
	if err := app.Run(); err != nil {
		InitConsoleOutput()
		fmt.Fprintf(os.Stderr, "Fatal error running system tray app: %v\n", err)
		os.Exit(1)
	}
}
