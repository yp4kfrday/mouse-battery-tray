package main

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	modAdvapi32 = syscall.NewLazyDLL("advapi32.dll")

	procRegOpenKeyExW    = modAdvapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW = modAdvapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW   = modAdvapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW  = modAdvapi32.NewProc("RegDeleteValueW")
	procRegCloseKey      = modAdvapi32.NewProc("RegCloseKey")
)

const (
	HKEY_CURRENT_USER = 0x80000001
	KEY_READ          = 0x20019
	KEY_WRITE         = 0x20006
	REG_SZ            = 1

	runSubKey   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValName  = `MouseBatteryTray`
)

func IsAutoStartEnabled() bool {
	subKeyW, _ := syscall.UTF16PtrFromString(runSubKey)
	valNameW, _ := syscall.UTF16PtrFromString(runValName)

	var hKey syscall.Handle
	r, _, _ := procRegOpenKeyExW.Call(
		HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(subKeyW)),
		0,
		KEY_READ,
		uintptr(unsafe.Pointer(&hKey)),
	)
	if r != 0 {
		return false
	}
	defer procRegCloseKey.Call(uintptr(hKey))

	var valType uint32
	var buf [512]uint16
	bufLen := uint32(len(buf) * 2)

	r, _, _ = procRegQueryValueExW.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(valNameW)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufLen)),
	)

	return r == 0
}

func EnableAutoStart() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	quotedPath := `"` + exePath + `"`

	subKeyW, _ := syscall.UTF16PtrFromString(runSubKey)
	valNameW, _ := syscall.UTF16PtrFromString(runValName)
	valDataW, _ := syscall.UTF16FromString(quotedPath)

	var hKey syscall.Handle
	r, _, err := procRegOpenKeyExW.Call(
		HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(subKeyW)),
		0,
		KEY_WRITE,
		uintptr(unsafe.Pointer(&hKey)),
	)
	if r != 0 {
		return err
	}
	defer procRegCloseKey.Call(uintptr(hKey))

	r, _, err = procRegSetValueExW.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(valNameW)),
		0,
		REG_SZ,
		uintptr(unsafe.Pointer(&valDataW[0])),
		uintptr(len(valDataW)*2),
	)
	if r != 0 {
		return err
	}

	return nil
}

func DisableAutoStart() error {
	subKeyW, _ := syscall.UTF16PtrFromString(runSubKey)
	valNameW, _ := syscall.UTF16PtrFromString(runValName)

	var hKey syscall.Handle
	r, _, err := procRegOpenKeyExW.Call(
		HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(subKeyW)),
		0,
		KEY_WRITE,
		uintptr(unsafe.Pointer(&hKey)),
	)
	if r != 0 {
		return err
	}
	defer procRegCloseKey.Call(uintptr(hKey))

	r, _, err = procRegDeleteValueW.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(valNameW)),
	)
	if r != 0 && r != 2 { // 2 = ERROR_FILE_NOT_FOUND (already disabled)
		return err
	}

	return nil
}

func ToggleAutoStart() (bool, error) {
	if IsAutoStartEnabled() {
		err := DisableAutoStart()
		return false, err
	}
	err := EnableAutoStart()
	return true, err
}
