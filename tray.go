package main

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"mouse-battery-tray/drivers"
	"mouse-battery-tray/hid"
	"mouse-battery-tray/logger"
)

var (
	modShell32 = syscall.NewLazyDLL("shell32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procShell_NotifyIconW = modShell32.NewProc("Shell_NotifyIconW")

	procRegisterClassExW    = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW     = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW      = modUser32.NewProc("DefWindowProcW")
	procDestroyWindow       = modUser32.NewProc("DestroyWindow")
	procPostQuitMessage     = modUser32.NewProc("PostQuitMessage")
	procGetMessageW         = modUser32.NewProc("GetMessageW")
	procTranslateMessage    = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW    = modUser32.NewProc("DispatchMessageW")
	procPostMessageW        = modUser32.NewProc("PostMessageW")
	procCreatePopupMenu     = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW         = modUser32.NewProc("AppendMenuW")
	procTrackPopupMenu      = modUser32.NewProc("TrackPopupMenu")
	procDestroyMenu         = modUser32.NewProc("DestroyMenu")
	procSetForegroundWindow = modUser32.NewProc("SetForegroundWindow")
	procGetCursorPos        = modUser32.NewProc("GetCursorPos")

	procGetModuleHandleW = modKernel32.NewProc("GetModuleHandleW")
)

const (
	NIM_ADD        = 0x00000000
	NIM_MODIFY     = 0x00000001
	NIM_DELETE     = 0x00000002
	NIM_SETVERSION = 0x00000004

	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004
	NIF_INFO    = 0x00000010

	NIIF_INFO    = 0x00000001
	NIIF_WARNING = 0x00000002

	WM_DESTROY     = 0x0002
	WM_COMMAND     = 0x0111
	WM_USER        = 0x0400
	WM_APP         = 0x8000
	WM_TRAY_NOTIFY = WM_APP + 100
	WM_TRAY_REFRESH = WM_APP + 101

	WM_LBUTTONUP    = 0x0202
	WM_RBUTTONUP    = 0x0205
	WM_LBUTTONDBLCLK = 0x0203
	WM_CONTEXTMENU  = 0x007B

	MF_STRING    = 0x00000000
	MF_GRAYED    = 0x00000001
	MF_DISABLED  = 0x00000002
	MF_CHECKED   = 0x00000008
	MF_SEPARATOR = 0x00000800

	TPM_RIGHTBUTTON = 0x0002
	TPM_BOTTOMALIGN = 0x0020

	CMD_REFRESH_NOW   = 2001
	CMD_AUTOSTART     = 2002
	CMD_EXIT          = 2003
	CMD_OPEN_LOG      = 2004
	CMD_CHECK_UPDATE  = 2005
)

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     syscall.Handle
	HIcon         syscall.Handle
	HCursor       syscall.Handle
	HbrBackground syscall.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       syscall.Handle
}

type POINT struct {
	X int32
	Y int32
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	_pad0            uint32
	HWnd             syscall.Handle
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	_pad1            uint32
	HIcon            syscall.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	TimeoutOrVersion uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         hid.GUID
	HBalloonIcon     syscall.Handle
}

type TrayApp struct {
	hwnd              syscall.Handle
	nid               NOTIFYICONDATAW
	currentIcon       syscall.Handle
	manager           *drivers.Manager
	currentState      drivers.BatteryState
	lowBatteryAlerted bool
}

var globalApp *TrayApp

func NewTrayApp(manager *drivers.Manager) *TrayApp {
	app := &TrayApp{
		manager: manager,
	}
	globalApp = app
	return app
}

func (app *TrayApp) Run() error {
	className, _ := syscall.UTF16PtrFromString("MouseBatteryTrayWndClass")
	windowName, _ := syscall.UTF16PtrFromString("MouseBatteryTrayWindow")

	hInst, _, _ := procGetModuleHandleW.Call(0)

	wndClass := WNDCLASSEXW{
		CbSize:      uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc: syscall.NewCallback(wndProc),
		HInstance:   syscall.Handle(hInst),
		LpszClassName: className,
	}

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wndClass)))

	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0, 0, 0, 0,
		0, 0,
		hInst,
		0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW failed: %w", err)
	}
	app.hwnd = syscall.Handle(hwnd)

	// Initialize NOTIFYICONDATAW
	app.nid.CbSize = uint32(unsafe.Sizeof(app.nid))
	app.nid.HWnd = app.hwnd
	app.nid.UID = 1
	app.nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	app.nid.UCallbackMessage = WM_TRAY_NOTIFY

	// Initial poll & icon
	app.Refresh()

	// Add icon to system tray
	r, _, err := procShell_NotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&app.nid)))
	if r == 0 {
		return fmt.Errorf("Shell_NotifyIconW ADD failed: %w", err)
	}

	// Start background poll timer (instant 1-second polling)
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if app.hwnd != 0 {
				procPostMessageW.Call(uintptr(app.hwnd), WM_TRAY_REFRESH, 0, 0)
			}
		}
	}()

	// Start background periodic update checker
	StartPeriodicUpdateChecker(func(title, msg string, isError bool) {
		flag := uint32(NIIF_INFO)
		if isError {
			flag = NIIF_WARNING
		}
		if globalApp != nil {
			globalApp.showNotification(title, msg, flag)
		}
	})

	// Win32 Message Pump
	var msg struct {
		HWnd    syscall.Handle
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		Pt      POINT
	}

	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	// Cleanup on exit
	app.Cleanup()

	return nil
}

// Cleanup removes tray icon and frees icon handle.
func (app *TrayApp) Cleanup() {
	if app.nid.HWnd != 0 {
		procShell_NotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&app.nid)))
		app.nid.HWnd = 0
	}
	if app.currentIcon != 0 {
		DestroyIconHandle(app.currentIcon)
		app.currentIcon = 0
	}
}

func (app *TrayApp) Refresh() {
	state := app.manager.PollAll()

	// Only update icon and tray if state actually changed
	changed := (state.Status != app.currentState.Status) ||
		(state.Percent != app.currentState.Percent) ||
		(state.IsCharging != app.currentState.IsCharging) ||
		(state.DeviceName != app.currentState.DeviceName) ||
		(app.currentIcon == 0)

	app.currentState = state

	if !changed {
		return
	}

	logger.Log("[TRAY] Status updated: %s", state.TooltipText())

	// Render dynamic icon
	newIcon, err := RenderBatteryIcon(state)
	if err == nil && newIcon != 0 {
		if app.currentIcon != 0 {
			DestroyIconHandle(app.currentIcon)
		}
		app.currentIcon = newIcon
		app.nid.HIcon = newIcon
	}

	// Format tooltip
	tip := state.TooltipText()
	tipW, _ := syscall.UTF16FromString(tip)
	var tipArr [128]uint16
	copy(tipArr[:], tipW)
	app.nid.SzTip = tipArr

	// Check for low battery notification (<20%)
	if state.Status == drivers.StatusDischarging && state.Percent < 20 && state.Percent >= 0 {
		if !app.lowBatteryAlerted {
			app.lowBatteryAlerted = true
			app.showNotification(
				"Низкий заряд батареи",
				fmt.Sprintf("%s: осталось всего %d%%. Подключите кабель или док-станцию.", state.DeviceName, state.Percent),
				NIIF_WARNING,
			)
		}
	} else if state.IsCharging || state.Percent >= 20 {
		// Reset alert when charging or recharged
		app.lowBatteryAlerted = false
	}

	// Update tray icon in taskbar
	if app.nid.HWnd != 0 {
		procShell_NotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&app.nid)))
	}
}

func (app *TrayApp) showNotification(title, text string, infoFlag uint32) {
	app.nid.UFlags |= NIF_INFO
	app.nid.DwInfoFlags = infoFlag

	titleW, _ := syscall.UTF16FromString(title)
	var titleArr [64]uint16
	copy(titleArr[:], titleW)
	app.nid.SzInfoTitle = titleArr

	textW, _ := syscall.UTF16FromString(text)
	var textArr [256]uint16
	copy(textArr[:], textW)
	app.nid.SzInfo = textArr

	procShell_NotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&app.nid)))

	// Clear info flag so subsequent icon refreshes don't re-trigger notification
	app.nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
}

func (app *TrayApp) showContextMenu() {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	// 1. Status header item (disabled / grayed)
	statusText := app.currentState.MenuStatusText()
	statusW, _ := syscall.UTF16PtrFromString(statusText)
	procAppendMenuW.Call(hMenu, uintptr(MF_DISABLED|MF_GRAYED|MF_STRING), 0, uintptr(unsafe.Pointer(statusW)))

	// Separator
	procAppendMenuW.Call(hMenu, uintptr(MF_SEPARATOR), 0, 0)

	// 2. "Обновить сейчас"
	refreshW, _ := syscall.UTF16PtrFromString("Обновить сейчас")
	procAppendMenuW.Call(hMenu, uintptr(MF_STRING), uintptr(CMD_REFRESH_NOW), uintptr(unsafe.Pointer(refreshW)))

	// 3. "Проверить обновления"
	updateW, _ := syscall.UTF16PtrFromString("Проверить обновления")
	procAppendMenuW.Call(hMenu, uintptr(MF_STRING), uintptr(CMD_CHECK_UPDATE), uintptr(unsafe.Pointer(updateW)))

	// 4. "Открыть лог"
	openLogW, _ := syscall.UTF16PtrFromString("Открыть лог")
	procAppendMenuW.Call(hMenu, uintptr(MF_STRING), uintptr(CMD_OPEN_LOG), uintptr(unsafe.Pointer(openLogW)))

	// 5. "Запускать вместе с Windows"
	autostartFlags := uintptr(MF_STRING)
	if IsAutoStartEnabled() {
		autostartFlags |= uintptr(MF_CHECKED)
	}
	autostartW, _ := syscall.UTF16PtrFromString("Запускать вместе с Windows")
	procAppendMenuW.Call(hMenu, autostartFlags, uintptr(CMD_AUTOSTART), uintptr(unsafe.Pointer(autostartW)))

	// Separator
	procAppendMenuW.Call(hMenu, uintptr(MF_SEPARATOR), 0, 0)

	// 6. "Выход"
	exitW, _ := syscall.UTF16PtrFromString("Выход")
	procAppendMenuW.Call(hMenu, uintptr(MF_STRING), uintptr(CMD_EXIT), uintptr(unsafe.Pointer(exitW)))

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	procSetForegroundWindow.Call(uintptr(app.hwnd))
	procTrackPopupMenu.Call(
		hMenu,
		uintptr(TPM_RIGHTBUTTON|TPM_BOTTOMALIGN),
		uintptr(pt.X),
		uintptr(pt.Y),
		0,
		uintptr(app.hwnd),
		0,
	)
}

func wndProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	if globalApp == nil {
		r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
		return r
	}

	switch msg {
	case WM_TRAY_NOTIFY:
		switch lParam {
		case WM_RBUTTONUP, WM_CONTEXTMENU:
			globalApp.showContextMenu()
		case WM_LBUTTONUP, WM_LBUTTONDBLCLK:
			globalApp.Refresh()
		}
		return 0

	case WM_TRAY_REFRESH:
		globalApp.Refresh()
		return 0

	case WM_COMMAND:
		cmdID := uint32(wParam & 0xFFFF)
		switch cmdID {
		case CMD_REFRESH_NOW:
			logger.Log("[USER] Context menu: 'Обновить сейчас'")
			globalApp.Refresh()
		case CMD_CHECK_UPDATE:
			logger.Log("[USER] Context menu: 'Проверить обновления'")
			TriggerManualUpdateCheck(func(title, msg string, isError bool) {
				flag := uint32(NIIF_INFO)
				if isError {
					flag = NIIF_WARNING
				}
				if globalApp != nil {
					globalApp.showNotification(title, msg, flag)
				}
			})
		case CMD_OPEN_LOG:
			logger.Log("[USER] Context menu: 'Открыть лог'")
			if err := logger.OpenLog(); err != nil {
				logger.Log("[ERROR] OpenLog failed: %v", err)
			}
		case CMD_AUTOSTART:
			en, err := ToggleAutoStart()
			logger.Log("[USER] Context menu: 'Запускать вместе с Windows' -> enabled=%v (err: %v)", en, err)
		case CMD_EXIT:
			logger.Log("[APP] Exit requested via context menu.")
			procDestroyWindow.Call(uintptr(hwnd))
			procPostQuitMessage.Call(0)
		}
		return 0

	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}

	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}
