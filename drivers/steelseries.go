package drivers

import (
	"time"

	"mouse-battery-tray/hid"
)

type SteelSeriesDriver struct{}

func NewSteelSeriesDriver() *SteelSeriesDriver {
	return &SteelSeriesDriver{}
}

func (d *SteelSeriesDriver) Name() string {
	return "SteelSeries Wireless"
}

func (d *SteelSeriesDriver) Match(info hid.DeviceInfo) bool {
	if info.VendorID != 0x1038 {
		return false
	}
	if info.UsagePage == 0x0001 && info.Usage == 0x0002 {
		return false
	}
	return info.UsagePage >= 0xFF00 || info.UsagePage == 0x000C
}

func (d *SteelSeriesDriver) Poll(dev *hid.Device) (BatteryState, error) {
	name := dev.Info.Product
	if name == "" {
		name = "SteelSeries Wireless Mouse"
	}

	state := BatteryState{
		Status:      StatusAsleep,
		DeviceName:  name,
		Percent:     -1,
		LastUpdated: time.Now(),
	}

	// SteelSeries battery query: 0x90 or 0xB0
	cmd := []byte{0x90}
	_ = dev.SendOutputReport(0x00, cmd)

	buf, err := dev.ReadWithTimeout(1200*time.Millisecond, 32)
	if err != nil || len(buf) < 3 {
		return state, nil
	}

	for _, off := range []int{1, 2, 3} {
		if off < len(buf) {
			val := int(buf[off])
			if val >= 1 && val <= 100 {
				charging := false
				if off+1 < len(buf) {
					charging = buf[off+1] == 1
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

	return state, nil
}
