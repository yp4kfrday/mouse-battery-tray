package logger

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	modShell32        = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW = modShell32.NewProc("ShellExecuteW")

	logMu       sync.Mutex
	logFilePath string
)

func init() {
	exe, err := os.Executable()
	if err == nil {
		logFilePath = filepath.Join(filepath.Dir(exe), "mouse-battery.log")
	} else {
		logFilePath = "mouse-battery.log"
	}
}

// LogFilePath returns the resolved absolute path to mouse-battery.log.
func LogFilePath() string {
	return logFilePath
}

// Log writes a timestamped line to mouse-battery.log.
// If the file exceeds 500 KB, it auto-rotates/truncates.
func Log(format string, args ...any) {
	logMu.Lock()
	defer logMu.Unlock()

	msg := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	line := fmt.Sprintf("[%s] %s\r\n", timestamp, msg)

	// Check if file exceeds 500 KB (500 * 1024 = 512,000 bytes)
	fi, err := os.Stat(logFilePath)
	if err == nil && fi.Size() > 500*1024 {
		f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err == nil {
			header := fmt.Sprintf("[%s] --- Log rotated (file size exceeded 500 KB) ---\r\n", timestamp)
			_, _ = f.WriteString(header)
			_, _ = f.WriteString(line)
			_ = f.Close()
			return
		}
	}

	f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// OpenLog launches the default text editor for mouse-battery.log.
func OpenLog() error {
	logMu.Lock()
	path := logFilePath
	if _, err := os.Stat(path); os.IsNotExist(err) {
		timestamp := time.Now().Format("2006-01-02 15:04:05.000")
		_ = os.WriteFile(path, []byte(fmt.Sprintf("[%s] --- MouseBatteryTray Log Initialized ---\r\n", timestamp)), 0644)
	}
	logMu.Unlock()

	pathW, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	opW, _ := syscall.UTF16PtrFromString("open")

	// Call ShellExecuteW(0, "open", path, nil, nil, SW_SHOWNORMAL=1)
	r, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(opW)),
		uintptr(unsafe.Pointer(pathW)),
		0,
		0,
		1,
	)
	if r <= 32 {
		// Fallback to notepad
		return exec.Command("notepad.exe", path).Start()
	}
	return nil
}
