package drivers

import (
	"strings"
	"time"

	"mouse-battery-tray/hid"
	"mouse-battery-tray/logger"
)

type AjazzCompXDriver struct {
	lastPercent     int
	lastChargeState bool
}

func NewAjazzCompXDriver() *AjazzCompXDriver {
	return &AjazzCompXDriver{}
}

func (d *AjazzCompXDriver) Name() string {
	return "Ajazz / CompX 8K Wireless"
}

func (d *AjazzCompXDriver) Match(info hid.DeviceInfo) bool {
	// Exclude standard mouse and keyboard collections
	if info.UsagePage == 0x0001 && (info.Usage == 0x0002 || info.Usage == 0x0006) {
		return false
	}

	prod := strings.ToLower(info.Product)
	// Never match VXE, VGN, Dragonfly, or ATK devices (handled by dedicated VXENordicDriver)
	if info.VendorID == 0x3554 || info.VendorID == 0x373B ||
		strings.Contains(prod, "vxe") || strings.Contains(prod, "vgn") ||
		strings.Contains(prod, "dragonfly") || strings.Contains(prod, "atk") {
		return false
	}

	// Known Vendor IDs for Ajazz / CompX 8K
	knownVIDs := []uint16{
		0x3151, // Ajazz (AJ179 Pro, AJ159, AJ139)
		0x25A7, // Compx OEM
		0x1915, // Nordic / OEM 2.4G
		0x258A, // SinoWealth
	}

	vidMatch := false
	for _, vid := range knownVIDs {
		if info.VendorID == vid {
			vidMatch = true
			break
		}
	}

	nameMatch := strings.Contains(prod, "ajazz") ||
		strings.Contains(prod, "aj179") ||
		strings.Contains(prod, "aj159") ||
		strings.Contains(prod, "aj139")

	if !vidMatch && !nameMatch {
		return false
	}

	// Match vendor usage page
	return info.UsagePage >= 0xFF00 || info.UsagePage == 0x000C
}

func (d *AjazzCompXDriver) Poll(dev *hid.Device) (BatteryState, error) {
	name := dev.Info.Product
	if name == "" {
		if dev.Info.VendorID == 0x3151 && dev.Info.ProductID == 0x402D {
			name = "Ajazz AJ179 Pro"
		} else {
			name = "Ajazz / CompX Wireless"
		}
	}

	state := BatteryState{
		Status:      StatusAsleep,
		DeviceName:  name,
		Percent:     -1,
		LastUpdated: time.Now(),
	}

	// 1. Primary AJAZZ AJ179 / AJ159 / 8K protocol:
	// Send 0xF7 Enabler poll via SET_FEATURE on Report ID 0x00 across UsagePage 0xFFFF
	if dev.Info.FeatureReportByteLength >= 64 {
		payload := make([]byte, dev.Info.FeatureReportByteLength-1)
		payload[0] = 0xF7
		_ = dev.SendFeatureReport(0x00, payload)
		logger.Log("[Ajazz] [TX] SET_FEATURE (Report 0x00, %d bytes): F7 00 ... -> VID:0x%04X PID:0x%04X",
			len(payload)+1, dev.Info.VendorID, dev.Info.ProductID)

		// Brief settle delay (~40ms) for 2.4G base to mirror mouse status
		time.Sleep(40 * time.Millisecond)

		// Read status via GET_FEATURE on Report ID 0x05
		res5, err := dev.GetFeatureReport(0x05, int(dev.Info.FeatureReportByteLength))
		if err == nil && len(res5) >= 5 {
			dumpLen := 16
			if len(res5) < dumpLen {
				dumpLen = len(res5)
			}
			logger.Log("[Ajazz] [RX] GET_FEATURE (Report 0x05, %d bytes): % X", len(res5), res5[:dumpLen])

			// Expected frame: [05 [dockFlag] 00 [Charge] [linkFlag] ...]
			// dockFlag is 0x01 on dock/charge, 0x00 on wireless
			if res5[0] == 0x05 && res5[2] == 0x00 && (res5[1] == 0x00 || res5[1] == 0x01) {
				val := int(res5[3])
				if val >= 1 && val <= 100 {
					dockFlag := res5[1]
					statusFlag := res5[4]

					// Check hardware charging indicators
					isCharging := (dockFlag == 0x01) || (statusFlag == 0x02) || (statusFlag&0x02 != 0)

					// Trend-based charging detection:
					// When mouse is placed on charging dock/cable, battery percentage rises
					if d.lastPercent > 0 {
						if val > d.lastPercent {
							d.lastChargeState = true
						} else if val < d.lastPercent {
							d.lastChargeState = false
						}
					}
					d.lastPercent = val

					if d.lastChargeState {
						isCharging = true
					}

					status := StatusDischarging
					if isCharging {
						status = StatusCharging
					}

					logger.Log("[Ajazz] [PARSED] %s: Battery=%d%%, Charging=%v (DockFlag=0x%02X, StatusFlag=0x%02X) -> Status: %s",
						name, val, isCharging, dockFlag, statusFlag, status)

					return BatteryState{
						Status:      status,
						DeviceName:  name,
						Percent:     val,
						IsCharging:  isCharging,
						RawPacket:   res5,
						LastUpdated: time.Now(),
					}, nil
				} else if val == 0x00 || val == 0xFF {
					logger.Log("[Ajazz] Mouse sleeping / unlinked from receiver (val=0x%02X)", val)
					return state, nil
				}
			}
		} else if err != nil {
			logger.Log("[Ajazz] GetFeatureReport 0x05 error: %v", err)
		}

		// Also check Report 0x00 response as fallback
		res0, err := dev.GetFeatureReport(0x00, int(dev.Info.FeatureReportByteLength))
		if err == nil && len(res0) >= 5 {
			if res0[0] == 0x00 && res0[2] == 0x00 && (res0[1] == 0x00 || res0[1] == 0x01) {
				val := int(res0[3])
				if val >= 1 && val <= 100 {
					dumpLen := 16
					if len(res0) < dumpLen {
						dumpLen = len(res0)
					}
					logger.Log("[Ajazz] [RX] GET_FEATURE Fallback (Report 0x00): % X", res0[:dumpLen])

					dockFlag := res0[1]
					statusFlag := res0[4]
					isCharging := (dockFlag == 0x01) || (statusFlag == 0x02) || (statusFlag&0x02 != 0)
					if d.lastPercent > 0 {
						if val > d.lastPercent {
							d.lastChargeState = true
						} else if val < d.lastPercent {
							d.lastChargeState = false
						}
					}
					d.lastPercent = val
					if d.lastChargeState {
						isCharging = true
					}
					status := StatusDischarging
					if isCharging {
						status = StatusCharging
					}
					logger.Log("[Ajazz] [PARSED] %s (Fallback 0x00): Battery=%d%%, Charging=%v", name, val, isCharging)
					return BatteryState{
						Status:      status,
						DeviceName:  name,
						Percent:     val,
						IsCharging:  isCharging,
						RawPacket:   res0,
						LastUpdated: time.Now(),
					}, nil
				}
			}
		}
	}

	// 2. Secondary fallback: Check Input Report ID 0x05 directly
	inRep, err := dev.GetInputReport(0x05, 5)
	if err == nil && len(inRep) >= 2 {
		if parsed, ok := parseAjazzPacket(inRep, name); ok {
			return parsed, nil
		}
	}

	// 3. Timed Read fallback (200ms timeout)
	if dev.Info.InputReportByteLength > 0 {
		buf, err := dev.ReadWithTimeout(200*time.Millisecond, int(dev.Info.InputReportByteLength))
		if err == nil && len(buf) > 0 {
			if parsed, ok := parseAjazzPacket(buf, name); ok {
				return parsed, nil
			}
		}
	}

	return state, nil
}

func parseAjazzPacket(data []byte, name string) (BatteryState, bool) {
	if len(data) == 0 {
		return BatteryState{}, false
	}

	reportID := data[0]
	// Check offsets for battery percentage (1..100)
	checkOffsets := []int{3, 1, 2, 4}
	if reportID > 0 && reportID <= 100 {
		checkOffsets = []int{0, 1, 2, 3}
	}

	for _, off := range checkOffsets {
		if off < len(data) {
			val := int(data[off])
			if val >= 1 && val <= 100 {
				flag := byte(0)
				if off+1 < len(data) {
					flag = data[off+1]
				} else if off > 0 {
					flag = data[off-1]
				}

				isCharging := (flag&0x02 != 0) || (flag == 0x02)
				status := StatusDischarging
				if isCharging {
					status = StatusCharging
				}

				return BatteryState{
					Status:      status,
					DeviceName:  name,
					Percent:     val,
					IsCharging:  isCharging,
					RawPacket:   data,
					LastUpdated: time.Now(),
				}, true
			}
		}
	}

	return BatteryState{}, false
}
