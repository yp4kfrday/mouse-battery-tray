package drivers

import (
	"time"

	"mouse-battery-tray/hid"
)

type LogitechDriver struct{}

func NewLogitechDriver() *LogitechDriver {
	return &LogitechDriver{}
}

func (d *LogitechDriver) Name() string {
	return "Logitech HID++ 2.0"
}

func (d *LogitechDriver) Match(info hid.DeviceInfo) bool {
	if info.VendorID != 0x046D {
		return false
	}
	if info.UsagePage == 0x0001 && info.Usage == 0x0002 {
		return false
	}
	return info.UsagePage >= 0xFF00 || info.UsagePage == 0xFF43 || info.Usage == 0x0201 || info.Usage == 0x0202
}

func (d *LogitechDriver) Poll(dev *hid.Device) (BatteryState, error) {
	name := dev.Info.Product
	if name == "" {
		name = "Logitech Wireless Mouse"
	}

	state := BatteryState{
		Status:      StatusAsleep,
		DeviceName:  name,
		Percent:     -1,
		LastUpdated: time.Now(),
	}

	// HID++ 2.0 Short Report (ID 0x10):
	// [0x10, DeviceIndex (0xFF), FeatureIndex, FunctionID, Param1, Param2, Param3]
	// Feature 0x1000 (Unified Battery) query: [0x10, 0xFF, 0x08, 0x00, 0x00, 0x00, 0x00]
	// Feature 0x1004 (Battery Voltage/Status): [0x10, 0xFF, 0x09, 0x00, 0x00, 0x00, 0x00]
	queries := [][]byte{
		{0x10, 0xFF, 0x08, 0x00, 0x00, 0x00, 0x00},
		{0x10, 0xFF, 0x09, 0x00, 0x00, 0x00, 0x00},
		{0x10, 0xFF, 0x00, 0x11, 0x00, 0x00, 0x00}, // Ping
	}

	for _, q := range queries {
		_ = dev.SendOutputReport(q[0], q[1:])
	}

	// Read response
	buf, err := dev.ReadWithTimeout(1200*time.Millisecond, 20)
	if err != nil || len(buf) < 5 {
		return state, nil
	}

	// Parse HID++ report
	// Short report 0x10 or Long report 0x11
	if buf[0] == 0x10 || buf[0] == 0x11 {
		// buf[1] is device index, buf[2] is feature
		// If error response (0x8F): device asleep or offline
		if buf[2] == 0x8F {
			return state, nil
		}

		// Battery percentage typically in byte 4 or 5
		for _, off := range []int{4, 5, 3} {
			if off < len(buf) {
				val := int(buf[off])
				if val >= 1 && val <= 100 {
					charging := false
					if off+1 < len(buf) {
						// 1 = Recharging, 2 = Almost full, 3 = Full
						chargingStatus := buf[off+1]
						charging = chargingStatus == 1 || chargingStatus == 2
					}
					status := StatusDischarging
					if charging {
						status = StatusCharging
					}
					return BatteryState{
						Status:      status,
						DeviceName:  name,
						Percent:     val,
						IsCharging:  charging,
						RawPacket:   buf,
						LastUpdated: time.Now(),
					}, nil
				}
			}
		}
	}

	return state, nil
}
