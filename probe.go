package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mouse-battery-tray/drivers"
	"mouse-battery-tray/hid"
)

var (
	procAttachConsole = modKernel32.NewProc("AttachConsole")
)

func InitConsoleOutput() {
	hOut, _ := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if hOut == 0 || hOut == syscall.InvalidHandle {
		r, _, _ := procAttachConsole.Call(^uintptr(0))
		if r != 0 {
			hOut, _ = syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
			if hOut != 0 && hOut != syscall.InvalidHandle {
				os.Stdout = os.NewFile(uintptr(hOut), "/dev/stdout")
			}
			hErr, _ := syscall.GetStdHandle(syscall.STD_ERROR_HANDLE)
			if hErr != 0 && hErr != syscall.InvalidHandle {
				os.Stderr = os.NewFile(uintptr(hErr), "/dev/stderr")
			}
		}
	} else {
		os.Stdout = os.NewFile(uintptr(hOut), "/dev/stdout")
	}
}

func RunProbe(mgr *drivers.Manager, once bool) {
	InitConsoleOutput()

	fmt.Println("===============================================================================")
	fmt.Println("           MouseBatteryTray Hardware Probe & Live Packet Monitor               ")
	fmt.Println("===============================================================================")
	fmt.Println("Strict Honesty: Zero dummy data. Streaming live hardware responses...")
	fmt.Println()

	// 1. Check Windows Native PnP
	fmt.Println("[1] Checking Windows PnP / Bluetooth LE Battery...")
	if pnpState, ok := drivers.PollWindowsPnP(); ok {
		fmt.Printf("    >>> Found Native Device: %s | Battery: %d%% | Status: %s\n",
			pnpState.DeviceName, pnpState.Percent, pnpState.TooltipText())
	} else {
		fmt.Println("    No Bluetooth LE / native Windows PnP battery devices reported.")
	}
	fmt.Println()

	// 2. Enumerate HID interfaces
	fmt.Println("[2] Enumerating connected HID interfaces...")
	os.Stdout.Sync()
	devs, err := hid.Enumerate(nil)
	if err != nil {
		fmt.Printf("    Enumeration failed: %v\n", err)
		os.Stdout.Sync()
		return
	}

	type ProbeTarget struct {
		info   hid.DeviceInfo
		driver drivers.Driver
	}

	var targets []ProbeTarget
	for _, d := range devs {
		for _, drv := range mgr.Drivers() {
			if drv.Match(d) {
				targets = append(targets, ProbeTarget{info: d, driver: drv})
				break
			}
		}
	}

	if len(targets) == 0 {
		fmt.Println("    No supported wireless mice detected.")
		fmt.Println("    Make sure the 2.4G dongle or USB cable is plugged in.")
		os.Stdout.Sync()
		return
	}

	fmt.Printf("    Found %d matching interface(s):\n", len(targets))
	for i, t := range targets {
		fmt.Printf("    [%d] Driver: %-25s | VID: 0x%04X PID: 0x%04X | Page: 0x%04X Usage: 0x%04X | InLen: %-2d OutLen: %-2d FeatLen: %-2d | '%s'\n",
			i+1, t.driver.Name(), t.info.VendorID, t.info.ProductID, t.info.UsagePage, t.info.Usage,
			t.info.InputReportByteLength, t.info.OutputReportByteLength, t.info.FeatureReportByteLength, t.info.Product)
	}
	fmt.Println()
	os.Stdout.Sync()

	// 3. Open interfaces and send active probes
	fmt.Println("[3] Opening vendor interfaces and sending probe queries...")
	os.Stdout.Sync()
	var activeDevs []*hid.Device
	for _, t := range targets {
		dev, err := hid.Open(t.info)
		if err != nil {
			fmt.Printf("    Could not open [%s]: %v\n", t.info.Path, err)
			continue
		}
		activeDevs = append(activeDevs, dev)

		// Send initial probe queries depending on interface
		if t.info.FeatureReportByteLength > 0 {
			cmd := make([]byte, t.info.FeatureReportByteLength-1)
			cmd[0] = 0xF7 // AJAZZ 0xF7 telemetry heartbeat
			_ = dev.SendFeatureReport(0x00, cmd)
		}
		if t.info.OutputReportByteLength > 0 {
			cmd := make([]byte, t.info.OutputReportByteLength-1)
			cmd[0] = 0x11
			cmd[1] = 0x02
			_ = dev.SendOutputReport(0x05, cmd)
		}
	}

	if len(activeDevs) == 0 {
		fmt.Println("    Could not open any interfaces.")
		return
	}
	defer func() {
		for _, dev := range activeDevs {
			dev.Close()
		}
	}()

	// Query live status from manager
	initialState := mgr.PollAll()
	fmt.Printf("    Current Hardware Status: %s\n", initialState.TooltipText())
	if initialState.Percent > 0 {
		fmt.Printf("    Battery: %d%% | Charging: %v\n", initialState.Percent, initialState.IsCharging)
	}

	if once {
		fmt.Println("\n(--once flag specified, exiting probe)")
		return
	}

	fmt.Println()
	fmt.Println("[4] Streaming live HID packets (Press Ctrl+C to stop)...")
	fmt.Println("    TIP: Plug in the charging cable/dock or move the mouse to trigger status packets.")
	fmt.Println("-------------------------------------------------------------------------------")

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	packetChan := make(chan struct {
		dev  *hid.Device
		data []byte
	}, 100)

	// Launch reader goroutines for devices with input capabilities
	for _, dev := range activeDevs {
		d := dev
		if d.Info.InputReportByteLength > 0 {
			go func() {
				for {
					buf, err := d.ReadWithTimeout(1*time.Second, int(d.Info.InputReportByteLength))
					if err == nil && len(buf) > 0 {
						packetChan <- struct {
							dev  *hid.Device
							data []byte
						}{dev: d, data: buf}
					}
					// Check if device is closed
					if err == hid.ErrDeviceClosed {
						break
					}
				}
			}()
		}
	}

	// Poll timer to re-send probes every 3 seconds while in probe mode
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			fmt.Println("\nProbe stopped.")
			return

		case <-ticker.C:
			// Re-send probe queries to prompt dongle
			for _, dev := range activeDevs {
				if dev.Info.FeatureReportByteLength > 0 {
					cmd := make([]byte, dev.Info.FeatureReportByteLength-1)
					cmd[0] = 0xF7
					_ = dev.SendFeatureReport(0x00, cmd)
				}
				if dev.Info.OutputReportByteLength > 0 {
					cmd := make([]byte, dev.Info.OutputReportByteLength-1)
					cmd[0] = 0x11
					cmd[1] = 0x02
					_ = dev.SendOutputReport(0x05, cmd)
				}
			}

		case pkt := <-packetChan:
			timestamp := time.Now().Format("15:04:05.000")
			hexStr := fmt.Sprintf("% X", pkt.data)

			// Try to parse packet
			state := mgr.PollAll()
			parsedInfo := "[Raw Packet]"
			if state.Status == drivers.StatusCharging {
				parsedInfo = fmt.Sprintf("[BATTERY: %d%% | CHARGING ⚡]", state.Percent)
			} else if state.Status == drivers.StatusDischarging {
				parsedInfo = fmt.Sprintf("[BATTERY: %d%% | DISCHARGING]", state.Percent)
			} else if state.Status == drivers.StatusAsleep {
				parsedInfo = "[DONGLE IDLE / MOUSE ASLEEP]"
			} else if state.Status == drivers.StatusDisconnected {
				parsedInfo = "[DEVICE DISCONNECTED]"
			}

			fmt.Printf("[%s] Page: 0x%04X Usage: 0x%04X | Len: %-2d | %-28s | Raw: %s\n",
				timestamp, pkt.dev.Info.UsagePage, pkt.dev.Info.Usage, len(pkt.data), parsedInfo, hexStr)
		}
	}
}
