package drivers

import (
	"time"

	"mouse-battery-tray/hid"
)

type RazerDriver struct{}

func NewRazerDriver() *RazerDriver {
	return &RazerDriver{}
}

func (d *RazerDriver) Name() string {
	return "Razer Wireless"
}

func (d *RazerDriver) Match(info hid.DeviceInfo) bool {
	if info.VendorID != 0x1532 {
		return false
	}
	if info.UsagePage == 0x0001 && info.Usage == 0x0002 {
		return false
	}
	return info.UsagePage >= 0xFF00 || info.UsagePage == 0x000C || info.FeatureReportByteLength >= 90
}

func (d *RazerDriver) Poll(dev *hid.Device) (BatteryState, error) {
	name := dev.Info.Product
	if name == "" {
		name = "Razer Wireless Mouse"
	}

	state := BatteryState{
		Status:      StatusAsleep,
		DeviceName:  name,
		Percent:     -1,
		LastUpdated: time.Now(),
	}

	// 90-byte Razer report
	report := make([]byte, 90)
	report[0] = 0x00 // Report ID
	report[1] = 0x00 // Status
	report[2] = 0x1F // Transaction ID
	report[3] = 0x00 // Remaining packets MSB
	report[4] = 0x00 // Remaining packets LSB
	report[5] = 0x00 // Protocol type
	report[6] = 0x02 // Data size
	report[7] = 0x07 // Command Class: Battery
	report[8] = 0x80 // Command ID: Get Battery Level

	// Calculate XOR checksum over bytes 2..88
	var crc byte = 0
	for i := 2; i < 89; i++ {
		crc ^= report[i]
	}
	report[89] = crc

	err := dev.SendFeatureReport(0x00, report[1:])
	if err != nil {
		return state, nil
	}

	time.Sleep(30 * time.Millisecond)

	res, err := dev.GetFeatureReport(0x00, 91)
	if err != nil || len(res) < 12 {
		return state, nil
	}

	// In response, byte 9 is battery level, byte 10 is charging state
	rawLevel := int(res[9])
	// Some models return 0..255 (scale to 100), others return 0..100 directly
	percent := rawLevel
	if percent > 100 && percent <= 255 {
		percent = int(float64(rawLevel) / 255.0 * 100.0)
	}

	if percent >= 1 && percent <= 100 {
		charging := res[10] != 0
		status := StatusDischarging
		if charging {
			status = StatusCharging
		}
		return BatteryState{
			Status:      status,
			DeviceName:  name,
			Percent:     percent,
			IsCharging:  charging,
			RawPacket:   res,
			LastUpdated: time.Now(),
		}, nil
	}

	return state, nil
}
