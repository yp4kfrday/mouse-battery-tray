package hid

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var (
	modHid      = syscall.NewLazyDLL("hid.dll")
	modSetupapi = syscall.NewLazyDLL("setupapi.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procHidD_GetHidGuid              = modHid.NewProc("HidD_GetHidGuid")
	procHidD_GetAttributes           = modHid.NewProc("HidD_GetAttributes")
	procHidD_GetPreparsedData        = modHid.NewProc("HidD_GetPreparsedData")
	procHidD_FreePreparsedData       = modHid.NewProc("HidD_FreePreparsedData")
	procHidP_GetCaps                 = modHid.NewProc("HidP_GetCaps")
	procHidD_GetProductString        = modHid.NewProc("HidD_GetProductString")
	procHidD_GetManufacturerString   = modHid.NewProc("HidD_GetManufacturerString")
	procHidD_SetOutputReport         = modHid.NewProc("HidD_SetOutputReport")
	procHidD_GetInputReport          = modHid.NewProc("HidD_GetInputReport")
	procHidD_SetFeature              = modHid.NewProc("HidD_SetFeature")
	procHidD_GetFeature              = modHid.NewProc("HidD_GetFeature")

	procSetupDiGetClassDevsW          = modSetupapi.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInterfaces   = modSetupapi.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiGetDeviceInterfaceDetailW = modSetupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procSetupDiDestroyDeviceInfoList  = modSetupapi.NewProc("SetupDiDestroyDeviceInfoList")

	procCreateEventW         = modKernel32.NewProc("CreateEventW")
	procWaitForSingleObject  = modKernel32.NewProc("WaitForSingleObject")
	procGetOverlappedResult  = modKernel32.NewProc("GetOverlappedResult")
	procCancelIoEx           = modKernel32.NewProc("CancelIoEx")
)

const (
	DIGCF_PRESENT         = 0x00000002
	DIGCF_DEVICEINTERFACE = 0x00000010

	GENERIC_READ  = 0x80000000
	GENERIC_WRITE = 0x40000000

	FILE_SHARE_READ  = 0x00000001
	FILE_SHARE_WRITE = 0x00000002

	OPEN_EXISTING = 3

	FILE_FLAG_OVERLAPPED = 0x40000000
	FILE_ATTRIBUTE_NORMAL = 0x00000080

	ERROR_IO_PENDING = 997
	WAIT_OBJECT_0    = 0
	WAIT_TIMEOUT     = 258
)

var (
	ErrTimeout       = errors.New("hid: read timeout")
	ErrDeviceClosed  = errors.New("hid: device closed")
	ErrNotSupported  = errors.New("hid: operation not supported")
)

type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type SP_DEVICE_INTERFACE_DATA struct {
	CbSize             uint32
	InterfaceClassGuid GUID
	Flags              uint32
	Reserved           uintptr
}

type HIDD_ATTRIBUTES struct {
	Size          uint32
	VendorID      uint16
	ProductID     uint16
	VersionNumber uint16
}

type HIDP_CAPS struct {
	Usage                     uint16
	UsagePage                 uint16
	InputReportByteLength     uint16
	OutputReportByteLength    uint16
	FeatureReportByteLength   uint16
	Reserved                  [17]uint16
	NumberLinkCollectionNodes uint16
	NumberInputButtonCaps     uint16
	NumberInputValueCaps      uint16
	NumberInputDataIndices    uint16
	NumberOutputButtonCaps    uint16
	NumberOutputValueCaps     uint16
	NumberOutputDataIndices   uint16
	NumberFeatureButtonCaps   uint16
	NumberFeatureValueCaps    uint16
	NumberFeatureDataIndices  uint16
}

type OVERLAPPED struct {
	Internal     uintptr
	InternalHigh uintptr
	Offset       uint32
	OffsetHigh   uint32
	HEvent       syscall.Handle
}

type DeviceInfo struct {
	Path                   string
	VendorID               uint16
	ProductID              uint16
	VersionNumber          uint16
	Manufacturer           string
	Product                string
	UsagePage              uint16
	Usage                  uint16
	InputReportByteLength  uint16
	OutputReportByteLength uint16
	FeatureReportByteLength uint16
}

type Device struct {
	Path   string
	Handle syscall.Handle
	Info   DeviceInfo
}

// GetHidGuid returns the GUID for HID class devices.
func GetHidGuid() GUID {
	var g GUID
	procHidD_GetHidGuid.Call(uintptr(unsafe.Pointer(&g)))
	return g
}

// Enumerate returns all present HID devices that match the optional filter.
func Enumerate(filter func(info DeviceInfo) bool) ([]DeviceInfo, error) {
	hidGuid := GetHidGuid()
	hDevInfo, _, err := procSetupDiGetClassDevsW.Call(
		uintptr(unsafe.Pointer(&hidGuid)),
		0,
		0,
		uintptr(DIGCF_PRESENT|DIGCF_DEVICEINTERFACE),
	)
	if hDevInfo == uintptr(syscall.InvalidHandle) {
		return nil, fmt.Errorf("SetupDiGetClassDevsW failed: %w", err)
	}
	defer procSetupDiDestroyDeviceInfoList.Call(hDevInfo)

	var devices []DeviceInfo
	var ifData SP_DEVICE_INTERFACE_DATA
	ifData.CbSize = uint32(unsafe.Sizeof(ifData))

	for idx := uint32(0); ; idx++ {
		r, _, _ := procSetupDiEnumDeviceInterfaces.Call(
			hDevInfo,
			0,
			uintptr(unsafe.Pointer(&hidGuid)),
			uintptr(idx),
			uintptr(unsafe.Pointer(&ifData)),
		)
		if r == 0 {
			break // No more devices
		}

		// Query required buffer size
		var requiredSize uint32
		procSetupDiGetDeviceInterfaceDetailW.Call(
			hDevInfo,
			uintptr(unsafe.Pointer(&ifData)),
			0,
			0,
			uintptr(unsafe.Pointer(&requiredSize)),
			0,
		)
		if requiredSize == 0 {
			continue
		}

		// Allocate buffer for detail data
		buf := make([]byte, requiredSize)
		// On 64-bit Windows, cbSize is 8 bytes
		*(*uint32)(unsafe.Pointer(&buf[0])) = 8

		r, _, _ = procSetupDiGetDeviceInterfaceDetailW.Call(
			hDevInfo,
			uintptr(unsafe.Pointer(&ifData)),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(requiredSize),
			0,
			0,
		)
		if r == 0 {
			continue
		}

		// Extract UTF-16 path starting at offset 4 (or offset 8 aligned)
		// In SP_DEVICE_INTERFACE_DETAIL_DATA_W:
		// DWORD cbSize (4 bytes)
		// WCHAR DevicePath[1]
		// In amd64 memory, DevicePath starts at offset 4
		pathPtr := (*uint16)(unsafe.Pointer(&buf[4]))
		devicePath := syscall.UTF16ToString((*[4096]uint16)(unsafe.Pointer(pathPtr))[:])
		if devicePath == "" {
			continue
		}

		// Query device attributes without exclusive access (0 access mask)
		devInfo, ok := queryDeviceAttributes(devicePath)
		if !ok {
			continue
		}

		if filter == nil || filter(devInfo) {
			devices = append(devices, devInfo)
		}
	}

	return devices, nil
}

func queryDeviceAttributes(path string) (DeviceInfo, bool) {
	pathW, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return DeviceInfo{}, false
	}

	// Open with 0 access mask to read attributes and capabilities without sharing locks
	h, err := syscall.CreateFile(
		pathW,
		0,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil || h == syscall.InvalidHandle {
		return DeviceInfo{}, false
	}
	defer syscall.CloseHandle(h)

	var attrs HIDD_ATTRIBUTES
	attrs.Size = uint32(unsafe.Sizeof(attrs))
	r, _, _ := procHidD_GetAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&attrs)))
	if r == 0 {
		return DeviceInfo{}, false
	}

	info := DeviceInfo{
		Path:          path,
		VendorID:      attrs.VendorID,
		ProductID:     attrs.ProductID,
		VersionNumber: attrs.VersionNumber,
	}

	// Read string descriptors (best effort)
	var strBuf [256]uint16
	r, _, _ = procHidD_GetManufacturerString.Call(uintptr(h), uintptr(unsafe.Pointer(&strBuf[0])), uintptr(len(strBuf)*2))
	if r != 0 {
		info.Manufacturer = syscall.UTF16ToString(strBuf[:])
	}

	r, _, _ = procHidD_GetProductString.Call(uintptr(h), uintptr(unsafe.Pointer(&strBuf[0])), uintptr(len(strBuf)*2))
	if r != 0 {
		info.Product = syscall.UTF16ToString(strBuf[:])
	}

	// Read capabilities
	var preparsed uintptr
	r, _, _ = procHidD_GetPreparsedData.Call(uintptr(h), uintptr(unsafe.Pointer(&preparsed)))
	if r != 0 && preparsed != 0 {
		var caps HIDP_CAPS
		r2, _, _ := procHidP_GetCaps.Call(preparsed, uintptr(unsafe.Pointer(&caps)))
		if r2 >= 0 {
			info.UsagePage = caps.UsagePage
			info.Usage = caps.Usage
			info.InputReportByteLength = caps.InputReportByteLength
			info.OutputReportByteLength = caps.OutputReportByteLength
			info.FeatureReportByteLength = caps.FeatureReportByteLength
		}
		procHidD_FreePreparsedData.Call(preparsed)
	}

	return info, true
}

// Open opens a HID device with read/write access and overlapped I/O flag.
func Open(info DeviceInfo) (*Device, error) {
	pathW, err := syscall.UTF16PtrFromString(info.Path)
	if err != nil {
		return nil, err
	}

	// Try opening with GENERIC_READ | GENERIC_WRITE first
	h, err := syscall.CreateFile(
		pathW,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.OPEN_EXISTING,
		FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		// Try opening with GENERIC_READ only if read/write is denied
		h, err = syscall.CreateFile(
			pathW,
			syscall.GENERIC_READ,
			syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
			nil,
			syscall.OPEN_EXISTING,
			FILE_FLAG_OVERLAPPED,
			0,
		)
		if err != nil {
			return nil, fmt.Errorf("open HID device %s failed: %w", info.Path, err)
		}
	}

	return &Device{
		Path:   info.Path,
		Handle: h,
		Info:   info,
	}, nil
}

// Close closes the device handle.
func (d *Device) Close() error {
	if d.Handle != 0 && d.Handle != syscall.InvalidHandle {
		procCancelIoEx.Call(uintptr(d.Handle), 0)
		err := syscall.CloseHandle(d.Handle)
		d.Handle = syscall.InvalidHandle
		return err
	}
	return nil
}

// SendOutputReport sends an output report via HidD_SetOutputReport or WriteFile.
func (d *Device) SendOutputReport(reportID byte, data []byte) error {
	sz := len(data) + 1
	if int(d.Info.OutputReportByteLength) > sz {
		sz = int(d.Info.OutputReportByteLength)
	}
	buf := make([]byte, sz)
	buf[0] = reportID
	copy(buf[1:], data)

	// Try HidD_SetOutputReport first
	r, _, _ := procHidD_SetOutputReport.Call(
		uintptr(d.Handle),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if r != 0 {
		return nil
	}

	// Fallback to WriteFile with overlapped
	hEvent, _, _ := procCreateEventW.Call(0, 1, 0, 0)
	if hEvent == 0 {
		return errors.New("failed to create event for write")
	}
	defer syscall.CloseHandle(syscall.Handle(hEvent))

	var ov OVERLAPPED
	ov.HEvent = syscall.Handle(hEvent)

	var bytesWritten uint32
	err := syscall.WriteFile(d.Handle, buf, &bytesWritten, (*syscall.Overlapped)(unsafe.Pointer(&ov)))
	if err == nil {
		return nil
	}
	if err == syscall.Errno(ERROR_IO_PENDING) {
		waitRes, _, _ := procWaitForSingleObject.Call(hEvent, 1000)
		if waitRes == WAIT_OBJECT_0 {
			return nil
		}
		procCancelIoEx.Call(uintptr(d.Handle), uintptr(unsafe.Pointer(&ov)))
		return ErrTimeout
	}
	return err
}

// SendFeatureReport sends a feature report via HidD_SetFeature.
func (d *Device) SendFeatureReport(reportID byte, data []byte) error {
	sz := len(data) + 1
	if int(d.Info.FeatureReportByteLength) > sz {
		sz = int(d.Info.FeatureReportByteLength)
	}
	buf := make([]byte, sz)
	buf[0] = reportID
	copy(buf[1:], data)

	r, _, err := procHidD_SetFeature.Call(
		uintptr(d.Handle),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if r == 0 {
		return fmt.Errorf("HidD_SetFeature failed: %w", err)
	}
	return nil
}

// GetFeatureReport retrieves a feature report via HidD_GetFeature.
func (d *Device) GetFeatureReport(reportID byte, size int) ([]byte, error) {
	if int(d.Info.FeatureReportByteLength) > size {
		size = int(d.Info.FeatureReportByteLength)
	}
	if size <= 0 {
		size = 65
	}
	buf := make([]byte, size)
	buf[0] = reportID

	r, _, err := procHidD_GetFeature.Call(
		uintptr(d.Handle),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if r == 0 {
		return nil, fmt.Errorf("HidD_GetFeature failed: %w", err)
	}
	return buf, nil
}

// GetInputReport retrieves an input report via HidD_GetInputReport.
func (d *Device) GetInputReport(reportID byte, size int) ([]byte, error) {
	if int(d.Info.InputReportByteLength) > size {
		size = int(d.Info.InputReportByteLength)
	}
	if size <= 0 {
		size = 64
	}
	buf := make([]byte, size)
	buf[0] = reportID

	r, _, err := procHidD_GetInputReport.Call(
		uintptr(d.Handle),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if r == 0 {
		return nil, fmt.Errorf("HidD_GetInputReport failed: %w", err)
	}
	return buf, nil
}

// ReadWithTimeout reads an input report within the specified timeout using overlapped I/O.
func (d *Device) ReadWithTimeout(timeout time.Duration, size int) ([]byte, error) {
	if int(d.Info.InputReportByteLength) > size {
		size = int(d.Info.InputReportByteLength)
	}
	if size <= 0 {
		size = 64
	}

	hEvent, _, _ := procCreateEventW.Call(0, 1, 0, 0)
	if hEvent == 0 {
		return nil, errors.New("failed to create event for read")
	}
	defer syscall.CloseHandle(syscall.Handle(hEvent))

	var ov OVERLAPPED
	ov.HEvent = syscall.Handle(hEvent)

	buf := make([]byte, size)
	var bytesRead uint32

	err := syscall.ReadFile(d.Handle, buf, &bytesRead, (*syscall.Overlapped)(unsafe.Pointer(&ov)))
	if err == nil {
		return buf[:bytesRead], nil
	}

	if err == syscall.Errno(ERROR_IO_PENDING) {
		timeoutMs := uint32(timeout.Milliseconds())
		waitRes, _, _ := procWaitForSingleObject.Call(hEvent, uintptr(timeoutMs))
		if waitRes == WAIT_OBJECT_0 {
			var transferred uint32
			r, _, _ := procGetOverlappedResult.Call(
				uintptr(d.Handle),
				uintptr(unsafe.Pointer(&ov)),
				uintptr(unsafe.Pointer(&transferred)),
				0,
			)
			if r != 0 {
				return buf[:transferred], nil
			}
			return nil, errors.New("GetOverlappedResult failed")
		}
		// Cancel pending I/O on timeout
		procCancelIoEx.Call(uintptr(d.Handle), uintptr(unsafe.Pointer(&ov)))
		procWaitForSingleObject.Call(hEvent, 100) // allow cancellation to complete
		return nil, ErrTimeout
	}

	return nil, err
}
