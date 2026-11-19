package drivers

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	modSetupapi                      = syscall.NewLazyDLL("setupapi.dll")
	procSetupDiGetClassDevsW         = modSetupapi.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInfo        = modSetupapi.NewProc("SetupDiEnumDeviceInfo")
	procSetupDiGetDevicePropertyW    = modSetupapi.NewProc("SetupDiGetDevicePropertyW")
	procSetupDiDestroyDeviceInfoList = modSetupapi.NewProc("SetupDiDestroyDeviceInfoList")
)

type DEVPROPKEY struct {
	Fmtid syscall.GUID
	Pid   uint32
}

type SP_DEVINFO_DATA struct {
	CbSize    uint32
	ClassGuid syscall.GUID
	DevInst   uint32
	Reserved  uintptr
}

var (
	devpkeyBatteryLevel = DEVPROPKEY{
		Fmtid: syscall.GUID{
			Data1: 0x104EA319,
			Data2: 0x6EE2,
			Data3: 0x4701,
			Data4: [8]byte{0xBD, 0x47, 0x8D, 0xDB, 0xF4, 0x25, 0xBB, 0xE5},
		},
		Pid: 2,
	}

	devpkeyFriendlyName = DEVPROPKEY{
		Fmtid: syscall.GUID{
			Data1: 0xA45C254E,
			Data2: 0xDF1C,
			Data3: 0x4EFD,
			Data4: [8]byte{0x80, 0x20, 0x67, 0xD1, 0x46, 0xA8, 0x50, 0xE0},
		},
		Pid: 14,
	}

	devpkeyDeviceDesc = DEVPROPKEY{
		Fmtid: syscall.GUID{
			Data1: 0xA45C254E,
			Data2: 0xDF1C,
			Data3: 0x4EFD,
			Data4: [8]byte{0x80, 0x20, 0x67, 0xD1, 0x46, 0xA8, 0x50, 0xE0},
		},
		Pid: 2,
	}
)

const (
	DIGCF_ALLCLASSES = 0x00000004
	DIGCF_PRESENT    = 0x00000002
)

// PollWindowsPnP queries Windows Plug & Play for any Bluetooth LE or native device reporting battery level.
func PollWindowsPnP() (BatteryState, bool) {
	hDevInfo, _, _ := procSetupDiGetClassDevsW.Call(
		0,
		0,
		0,
		uintptr(DIGCF_ALLCLASSES|DIGCF_PRESENT),
	)
	if hDevInfo == uintptr(syscall.InvalidHandle) {
		return BatteryState{}, false
	}
	defer procSetupDiDestroyDeviceInfoList.Call(hDevInfo)

	var devInfo SP_DEVINFO_DATA
	devInfo.CbSize = uint32(unsafe.Sizeof(devInfo))

	for idx := uintptr(0); ; idx++ {
		r, _, _ := procSetupDiEnumDeviceInfo.Call(hDevInfo, idx, uintptr(unsafe.Pointer(&devInfo)))
		if r == 0 {
			break
		}

		var propType uint32
		var propBuf [16]byte
		var reqSize uint32

		r, _, _ = procSetupDiGetDevicePropertyW.Call(
			hDevInfo,
			uintptr(unsafe.Pointer(&devInfo)),
			uintptr(unsafe.Pointer(&devpkeyBatteryLevel)),
			uintptr(unsafe.Pointer(&propType)),
			uintptr(unsafe.Pointer(&propBuf[0])),
			uintptr(len(propBuf)),
			uintptr(unsafe.Pointer(&reqSize)),
			0,
		)
		if r != 0 && reqSize > 0 {
			// Battery percentage byte
			percent := int(propBuf[0])
			if percent >= 0 && percent <= 100 {
				// Query device friendly name
				name := queryDevicePropString(hDevInfo, &devInfo, &devpkeyFriendlyName)
				if name == "" {
					name = queryDevicePropString(hDevInfo, &devInfo, &devpkeyDeviceDesc)
				}
				if name == "" {
					name = "Bluetooth Wireless Mouse"
				}

				return BatteryState{
					Status:      StatusDischarging,
					DeviceName:  name,
					Percent:     percent,
					LastUpdated: time.Now(),
				}, true
			}
		}
	}

	return BatteryState{}, false
}

func queryDevicePropString(hDevInfo uintptr, devInfo *SP_DEVINFO_DATA, key *DEVPROPKEY) string {
	var propType uint32
	var propBuf [512]byte
	var reqSize uint32

	r, _, _ := procSetupDiGetDevicePropertyW.Call(
		hDevInfo,
		uintptr(unsafe.Pointer(devInfo)),
		uintptr(unsafe.Pointer(key)),
		uintptr(unsafe.Pointer(&propType)),
		uintptr(unsafe.Pointer(&propBuf[0])),
		uintptr(len(propBuf)),
		uintptr(unsafe.Pointer(&reqSize)),
		0,
	)
	if r != 0 && reqSize > 0 {
		return syscall.UTF16ToString((*[256]uint16)(unsafe.Pointer(&propBuf[0]))[:reqSize/2])
	}
	return ""
}
