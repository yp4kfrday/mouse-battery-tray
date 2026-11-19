package drivers

import (
	"sort"
	"time"

	"mouse-battery-tray/hid"
	"mouse-battery-tray/logger"
)

type Manager struct {
	drivers []Driver
}

func NewManager() *Manager {
	return &Manager{
		drivers: []Driver{
			NewVXENordicDriver(),
			NewAjazzCompXDriver(),
			NewLogitechDriver(),
			NewRazerDriver(),
			NewSteelSeriesDriver(),
		},
	}
}

func (m *Manager) Drivers() []Driver {
	return m.drivers
}

// PollAll performs a single poll across all connected devices and drivers.
func (m *Manager) PollAll() BatteryState {
	// 1. Check Windows native PnP (Bluetooth LE / Windows-supported mice)
	if pnpState, ok := PollWindowsPnP(); ok {
		logger.Log("[POLL] Windows PnP mouse active: %s (%d%%)", pnpState.DeviceName, pnpState.Percent)
		return pnpState
	}

	// 2. Enumerate all HID devices
	devs, err := hid.Enumerate(nil)
	if err != nil || len(devs) == 0 {
		logger.Log("[POLL] No HID devices found (err: %v)", err)
		return BatteryState{
			Status:      StatusDisconnected,
			Percent:     -1,
			LastUpdated: time.Now(),
		}
	}

	var matchedDevices []struct {
		info   hid.DeviceInfo
		driver Driver
	}

	for _, d := range devs {
		for _, drv := range m.drivers {
			if drv.Match(d) {
				matchedDevices = append(matchedDevices, struct {
					info   hid.DeviceInfo
					driver Driver
				}{info: d, driver: drv})
				break
			}
		}
	}

	if len(matchedDevices) == 0 {
		logger.Log("[POLL] %d HID interfaces present, but none matched known mouse drivers.", len(devs))
		return BatteryState{
			Status:      StatusDisconnected,
			Percent:     -1,
			LastUpdated: time.Now(),
		}
	}

	// Prioritize interfaces with FeatureReportByteLength (command/control endpoints)
	sort.SliceStable(matchedDevices, func(i, j int) bool {
		return matchedDevices[i].info.FeatureReportByteLength > matchedDevices[j].info.FeatureReportByteLength
	})

	var lastAsleepState *BatteryState

	// 3. Poll each matching device
	for _, item := range matchedDevices {
		dev, err := hid.Open(item.info)
		if err != nil {
			logger.Log("[POLL] Could not open matched device %s: %v", item.info.Path, err)
			continue
		}

		state, err := item.driver.Poll(dev)
		dev.Close()

		if err == nil {
			if state.Status == StatusCharging || state.Status == StatusDischarging {
				return state
			}
			if state.Status == StatusAsleep {
				lastAsleepState = &state
			}
		}
	}

	if lastAsleepState != nil {
		logger.Log("[POLL] Active device is in standby/sleep: %s", lastAsleepState.TooltipText())
		return *lastAsleepState
	}

	logger.Log("[POLL] All matched interfaces returned no active mouse telemetry.")
	return BatteryState{
		Status:      StatusDisconnected,
		Percent:     -1,
		LastUpdated: time.Now(),
	}
}
