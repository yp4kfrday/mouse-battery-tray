package drivers

import (
	"strings"
	"time"

	"mouse-battery-tray/hid"
	"mouse-battery-tray/logger"
)

// VXENordicDriver supports VXE R1 SE / SE+, VXE R1 Pro, VGN Dragonfly, ATK, and Compx Nordic nRF52840 mice.
type VXENordicDriver struct{}

func NewVXENordicDriver() *VXENordicDriver {
	return &VXENordicDriver{}
}

func (d *VXENordicDriver) Name() string {
	return "VXE / VGN / Nordic 52"
}

func (d *VXENordicDriver) Match(info hid.DeviceInfo) bool {
	// Exclude standard mouse and keyboard report collections
	if info.UsagePage == 0x0001 && (info.Usage == 0x0002 || info.Usage == 0x0006) {
		return false
	}

	// Supported VIDs:
	// 0x3554: VGN / VXE (R1 SE, R1 SE+, R1 Pro, Dragonfly F1)
	// 0x373B: ATK (F1, X1)
	// 0x25A7: Compx
	knownVIDs := []uint16{0x3554, 0x373B, 0x25A7}
	vidMatch := false
	for _, vid := range knownVIDs {
		if info.VendorID == vid {
			vidMatch = true
			break
		}
	}

	prod := strings.ToLower(info.Product)
	nameMatch := strings.Contains(prod, "vxe") ||
		strings.Contains(prod, "vgn") ||
		strings.Contains(prod, "dragonfly") ||
		strings.Contains(prod, "atk")

	if !vidMatch && !nameMatch {
		return false
	}

	// Target vendor telemetry page 0xFF02 or 0xFF00
	return info.UsagePage == 0xFF02 || info.UsagePage >= 0xFF00 || info.UsagePage == 0x000C
}

func (d *VXENordicDriver) Poll(dev *hid.Device) (BatteryState, error) {
	name := dev.Info.Product
	if name == "" || strings.Contains(strings.ToLower(name), "dongle") || strings.Contains(strings.ToLower(name), "receiver") {
		if dev.Info.VendorID == 0x3554 {
			switch dev.Info.ProductID {
			case 0xF58E, 0xF58F:
				name = "VXE R1 SE+"
			case 0xF58C, 0xF58D:
				name = "VXE R1 Pro"
			case 0xF55B, 0xF55C:
				name = "VGN Dragonfly F1"
			default:
				name = "VXE Wireless Mouse"
			}
		} else {
			name = "VXE / Nordic Wireless"
		}
	}

	state := BatteryState{
		Status:      StatusAsleep,
		DeviceName:  name,
		Percent:     -1,
		LastUpdated: time.Now(),
	}

	// 17-byte Nordic 52 battery query command variants:
	// Variant A: Report ID 0x08, checksum sum(0..16) == 0x55 (0x08 + 0x04 + 0x49 = 0x55)
	req08 := make([]byte, 16)
	req08[0] = 0x04 // Sub-command 0x04 = Battery query
	req08[15] = 0x49

	// Variant B: Report ID 0x00, checksum sum(0..16) == 0x55 (0x00 + 0x04 + 0x51 = 0x55)
	req00 := make([]byte, 16)
	req00[0] = 0x04
	req00[15] = 0x51

	logger.Log("[VXE] Querying %s (VID: 0x%04X, PID: 0x%04X, Page: 0x%04X, Usage: 0x%04X, InLen: %d, OutLen: %d, FeatLen: %d)",
		name, dev.Info.VendorID, dev.Info.ProductID, dev.Info.UsagePage, dev.Info.Usage,
		dev.Info.InputReportByteLength, dev.Info.OutputReportByteLength, dev.Info.FeatureReportByteLength)

	// Send query via Output and Feature reports
	_ = dev.SendOutputReport(0x08, req08)
	_ = dev.SendFeatureReport(0x08, req08)
	_ = dev.SendOutputReport(0x00, req00)
	_ = dev.SendFeatureReport(0x00, req00)

	time.Sleep(30 * time.Millisecond)

	// Read response (17 bytes or full report length)
	var res []byte
	var err error

	// 1. Try Feature Reports
	res, err = dev.GetFeatureReport(0x08, 17)
	if err != nil || len(res) < 8 || (res[0] != 0x08 && res[0] != 0x04) {
		res0, err0 := dev.GetFeatureReport(0x00, 17)
		if err0 == nil && len(res0) >= 8 && (res0[0] == 0x00 || res0[1] == 0x04) {
			res = res0
			err = nil
		}
	}

	// 2. Fallback to Input Report via ReadWithTimeout
	if err != nil || len(res) < 8 {
		res, err = dev.ReadWithTimeout(120*time.Millisecond, 17)
	}

	// 3. Fallback to GetInputReport
	if err != nil || len(res) < 8 {
		resIn, errIn := dev.GetInputReport(0x08, 17)
		if errIn == nil && len(resIn) >= 8 {
			res = resIn
			err = nil
		}
	}

	if err != nil || len(res) < 8 {
		logger.Log("[VXE] No response packet from %s (err: %v)", name, err)
		return state, err
	}

	dumpLen := len(res)
	if dumpLen > 17 {
		dumpLen = 17
	}
	logger.Log("[VXE] [RX] Raw response (%d bytes): % X", len(res), res[:dumpLen])

	// Parse response packet:
	// Standard Compx / Nordic layout:
	// res[0]: Report ID (0x08, 0x00, or 0x04)
	// res[1]: Sub-command (0x04)
	// res[5]: RF link state (0x01 = linked, 0x00 = unlinked/sleeping)
	// res[6]: Battery percentage (1..100)
	// res[7]: Wired / Charging flag (0=wireless discharging, 1=wired charging)
	// res[8..9]: Voltage in mV (big endian)
	pct := -1
	wiredFlag := byte(0)
	voltage := 0

	if (res[0] == 0x08 || res[0] == 0x00) && res[1] == 0x04 {
		pct = int(res[6])
		wiredFlag = res[7]
		if len(res) >= 10 {
			voltage = (int(res[8]) << 8) | int(res[9])
		}
	} else if res[0] == 0x04 {
		if len(res) >= 7 {
			pct = int(res[5])
			wiredFlag = res[6]
		}
	}

	if pct >= 1 && pct <= 100 {
		isCharging := (wiredFlag == 1)
		status := StatusDischarging
		if isCharging {
			status = StatusCharging
		}

		logger.Log("[VXE] [PARSED] %s: Battery=%d%%, Voltage=%dmV, WiredFlag=%d => Status=%s",
			name, pct, voltage, wiredFlag, status)

		return BatteryState{
			Status:      status,
			DeviceName:  name,
			Percent:     pct,
			IsCharging:  isCharging,
			RawPacket:   res,
			LastUpdated: time.Now(),
		}, nil
	} else if pct == 0 || pct == 0xFF {
		logger.Log("[VXE] %s is in sleep mode / unlinked (pct=0x%02X)", name, pct)
		return state, nil
	}

	return state, nil
}
