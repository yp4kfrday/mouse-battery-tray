package drivers

import (
	"fmt"
	"time"

	"mouse-battery-tray/hid"
)

type BatteryStatus int

const (
	StatusDisconnected BatteryStatus = iota // No USB dongle / device plugged in
	StatusAsleep                            // Dongle plugged in, but mouse sleeping / unlinked
	StatusDischarging                       // Active on battery
	StatusCharging                          // Cable or dock connected
)

func (s BatteryStatus) String() string {
	switch s {
	case StatusDisconnected:
		return "Disconnected"
	case StatusAsleep:
		return "Asleep"
	case StatusDischarging:
		return "Discharging"
	case StatusCharging:
		return "Charging"
	default:
		return "Unknown"
	}
}

const StatusNoDevice = StatusDisconnected // Compatibility alias

type BatteryState struct {
	Status      BatteryStatus
	DeviceName  string
	Percent     int // -1 if not available
	IsCharging  bool
	RawPacket   []byte
	LastUpdated time.Time
	Error       string
}

func (s BatteryState) TooltipText() string {
	switch s.Status {
	case StatusDisconnected:
		return "Мышь не подключена (USB-адаптер не найден)"
	case StatusAsleep:
		name := s.DeviceName
		if name == "" {
			name = "Мышь"
		}
		return fmt.Sprintf("%s: режим ожидания (спит)", name)
	case StatusCharging:
		name := s.DeviceName
		if name == "" {
			name = "Мышь"
		}
		return fmt.Sprintf("%s: %d%% (Заряжается ⚡)", name, s.Percent)
	case StatusDischarging:
		name := s.DeviceName
		if name == "" {
			name = "Мышь"
		}
		return fmt.Sprintf("%s: %d%% (Разряжается)", name, s.Percent)
	default:
		return "Статус неизвестен"
	}
}

func (s BatteryState) MenuStatusText() string {
	switch s.Status {
	case StatusDisconnected:
		return "Устройство: не подключено"
	case StatusAsleep:
		name := s.DeviceName
		if name == "" {
			name = "Беспроводная мышь"
		}
		return fmt.Sprintf("%s: режим ожидания (спит)", name)
	case StatusCharging:
		return fmt.Sprintf("%s: %d%% [Заряжается ⚡]", s.DeviceName, s.Percent)
	case StatusDischarging:
		return fmt.Sprintf("%s: %d%% [Разряжается]", s.DeviceName, s.Percent)
	default:
		return "Состояние: неизвестно"
	}
}

type Driver interface {
	Name() string
	Match(info hid.DeviceInfo) bool
	Poll(dev *hid.Device) (BatteryState, error)
}
