package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"mouse-battery-tray/logger"
)

// Version is injected at build time via -ldflags "-X main.Version=..."
var Version = "dev"

// DefaultGitHubRepo is the placeholder repository.
// Replace OWNER with your GitHub username or specify "github_repo" in devices.json.
const DefaultGitHubRepo = "OWNER/mouse-battery-tray"

var (
	modWininet              = syscall.NewLazyDLL("wininet.dll")
	procInternetOpenW       = modWininet.NewProc("InternetOpenW")
	procInternetOpenUrlW    = modWininet.NewProc("InternetOpenUrlW")
	procHttpQueryInfoW      = modWininet.NewProc("HttpQueryInfoW")
	procInternetReadFile    = modWininet.NewProc("InternetReadFile")
	procInternetCloseHandle = modWininet.NewProc("InternetCloseHandle")
)

const (
	INTERNET_OPEN_TYPE_PRECONFIG = 0
	INTERNET_FLAG_RELOAD         = 0x80000000
	INTERNET_FLAG_NO_CACHE_WRITE = 0x04000000
	INTERNET_FLAG_SECURE         = 0x00800000

	HTTP_QUERY_STATUS_CODE = 19
	HTTP_QUERY_FLAG_NUMBER = 0x20000000
)

type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type GitHubRelease struct {
	TagName    string        `json:"tag_name"`
	Name       string        `json:"name"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Assets     []GitHubAsset `json:"assets"`
}

type ConfigFile struct {
	GitHubRepo   string `json:"github_repo"`
	CheckUpdates bool   `json:"check_updates"`
}

// CleanOldBinary silently removes any leftover .old binary from a previous update.
func CleanOldBinary() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	exePath, _ = filepath.EvalSymlinks(exePath)
	dir := filepath.Dir(exePath)

	oldPath := filepath.Join(dir, "mouse-battery.exe.old")
	if _, err := os.Stat(oldPath); err == nil {
		if rmErr := os.Remove(oldPath); rmErr == nil {
			logger.Log("[UPDATER] Cleaned up previous binary: %s", oldPath)
		}
	}

	newPath := filepath.Join(dir, "mouse-battery.exe.new")
	if _, err := os.Stat(newPath); err == nil {
		_ = os.Remove(newPath)
	}
}

// GetConfiguredRepo reads the GitHub repository from devices.json or returns the default.
func GetConfiguredRepo() (string, bool) {
	exePath, err := os.Executable()
	if err == nil {
		exePath, _ = filepath.EvalSymlinks(exePath)
		configPath := filepath.Join(filepath.Dir(exePath), "devices.json")
		if data, err := os.ReadFile(configPath); err == nil {
			var cfg ConfigFile
			if jsonErr := json.Unmarshal(data, &cfg); jsonErr == nil {
				repo := strings.TrimSpace(cfg.GitHubRepo)
				if repo != "" {
					return repo, cfg.CheckUpdates
				}
			}
		}
	}
	return DefaultGitHubRepo, true
}

// StartPeriodicUpdateChecker runs update checks on startup (10s delay) and every 2 hours.
func StartPeriodicUpdateChecker(notifyFn func(title, msg string, isError bool)) {
	go func() {
		// Initial startup delay of 10 seconds
		time.Sleep(10 * time.Second)
		logger.Log("[UPDATER] Running initial background update check...")
		CheckForUpdates(false, notifyFn)

		// Periodic check every 2 hours
		ticker := time.NewTicker(2 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			logger.Log("[UPDATER] Running periodic 2-hour update check...")
			CheckForUpdates(false, notifyFn)
		}
	}()
}

// TriggerManualUpdateCheck initiates an update check requested by user in the menu.
func TriggerManualUpdateCheck(notifyFn func(title, msg string, isError bool)) {
	go CheckForUpdates(true, notifyFn)
}

// CheckForUpdates queries GitHub Releases API and triggers an update if a newer release exists.
func CheckForUpdates(manual bool, notifyFn func(title, msg string, isError bool)) {
	repo, enabled := GetConfiguredRepo()
	if !enabled && !manual {
		logger.Log("[UPDATER] Update checks disabled in devices.json.")
		return
	}

	if repo == "OWNER/mouse-battery-tray" || strings.HasPrefix(repo, "OWNER/") {
		logger.Log("[UPDATER] Repository is placeholder '%s'. Set your GitHub username in devices.json.", repo)
		if manual && notifyFn != nil {
			notifyFn(
				"MouseBatteryTray",
				fmt.Sprintf("Укажите ваш GitHub репозиторий в devices.json (сейчас: %s).", repo),
				true,
			)
		}
		return
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases", repo)
	logger.Log("[UPDATER] Querying GitHub releases: %s (current version: %s)", apiURL, Version)

	data, statusCode, err := httpGet(apiURL)
	if err != nil {
		logger.Log("[UPDATER] Network error checking updates: %v", err)
		if manual && notifyFn != nil {
			notifyFn("MouseBatteryTray", "Не удалось проверить обновления: проверьте интернет.", true)
		}
		return
	}

	if statusCode != 200 {
		logger.Log("[UPDATER] GitHub API returned HTTP %d: %s", statusCode, string(data))
		if manual && notifyFn != nil {
			notifyFn("MouseBatteryTray", fmt.Sprintf("GitHub API вернул статус: %d", statusCode), true)
		}
		return
	}

	var releases []GitHubRelease
	if err := json.Unmarshal(data, &releases); err != nil {
		logger.Log("[UPDATER] Failed to decode releases JSON: %v", err)
		if manual && notifyFn != nil {
			notifyFn("MouseBatteryTray", "Не удалось разобрать список релизов.", true)
		}
		return
	}

	// Find the latest non-draft release (including pre-releases/betas)
	var latest *GitHubRelease
	for i := range releases {
		rel := &releases[i]
		if !rel.Draft {
			latest = rel
			break
		}
	}

	if latest == nil {
		logger.Log("[UPDATER] No published releases found in %s.", repo)
		if manual && notifyFn != nil {
			notifyFn("MouseBatteryTray", "В репозитории пока нет опубликованных релизов.", false)
		}
		return
	}

	cmp := CompareVersions(Version, latest.TagName)
	logger.Log("[UPDATER] Latest release: %s (tag: %s, prerelease: %v). Comparison: %d",
		latest.Name, latest.TagName, latest.Prerelease, cmp)

	if cmp <= 0 {
		logger.Log("[UPDATER] Already up to date (current: %s, latest: %s).", Version, latest.TagName)
		if manual && notifyFn != nil {
			notifyFn(
				"MouseBatteryTray",
				fmt.Sprintf("У вас установлена актуальная версия: %s", Version),
				false,
			)
		}
		return
	}

	// Newer version found, look for executable asset
	var exeAsset *GitHubAsset
	for _, a := range latest.Assets {
		lowerName := strings.ToLower(a.Name)
		if lowerName == "mouse-battery.exe" || strings.HasSuffix(lowerName, ".exe") {
			exeAsset = &a
			break
		}
	}

	if exeAsset == nil {
		logger.Log("[UPDATER] New release %s found, but no .exe asset attached.", latest.TagName)
		if manual && notifyFn != nil {
			notifyFn("MouseBatteryTray", fmt.Sprintf("Версия %s найдена, но файл .exe отсутствует в релизе.", latest.TagName), true)
		}
		return
	}

	logger.Log("[UPDATER] Downloading update %s (%d bytes) from %s...", latest.TagName, exeAsset.Size, exeAsset.BrowserDownloadURL)
	if notifyFn != nil {
		notifyFn("MouseBatteryTray", fmt.Sprintf("Найдена новая версия %s! Скачивание обновления...", latest.TagName), false)
	}

	if err := downloadAndApplyUpdate(exeAsset.BrowserDownloadURL, latest.TagName, notifyFn); err != nil {
		logger.Log("[UPDATER] Update failed: %v", err)
		if notifyFn != nil {
			notifyFn("Ошибка обновления", fmt.Sprintf("Не удалось установить %s: %v", latest.TagName, err), true)
		}
	}
}

// downloadAndApplyUpdate downloads new binary, swaps via .old / .new, restarts detached, and exits.
func downloadAndApplyUpdate(downloadURL, newVersion string, notifyFn func(title, msg string, isError bool)) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, _ = filepath.EvalSymlinks(exePath)
	dir := filepath.Dir(exePath)

	newPath := filepath.Join(dir, "mouse-battery.exe.new")
	oldPath := filepath.Join(dir, "mouse-battery.exe.old")

	// 1. Download to mouse-battery.exe.new
	_ = os.Remove(newPath)
	if err := downloadFile(downloadURL, newPath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Verify reasonable binary size (> 500 KB)
	fi, err := os.Stat(newPath)
	if err != nil || fi.Size() < 500*1024 {
		_ = os.Remove(newPath)
		return fmt.Errorf("downloaded binary is invalid or too small, possible corruption")
	}

	logger.Log("[UPDATER] Downloaded %d bytes to %s", fi.Size(), newPath)

	// 2. Rename running binary to .old
	_ = os.Remove(oldPath) // Remove any preexisting .old
	if err := os.Rename(exePath, oldPath); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("failed to rename running binary to .old: %w", err)
	}

	// 3. Rename .new to running binary name
	if err := os.Rename(newPath, exePath); err != nil {
		// Rollback attempt
		_ = os.Rename(oldPath, exePath)
		return fmt.Errorf("failed to replace binary with .new: %w", err)
	}

	logger.Log("[UPDATER] Binary replaced successfully with version %s. Starting new instance...", newVersion)

	if notifyFn != nil {
		notifyFn("MouseBatteryTray", fmt.Sprintf("Версия %s успешно установлена! Перезапуск...", newVersion), false)
	}

	// 4. Detached restart of new binary
	cmd := exec.Command(exePath)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x00000008, // DETACHED_PROCESS
	}

	if err := cmd.Start(); err != nil {
		logger.Log("[UPDATER] Failed to restart new process: %v", err)
		return fmt.Errorf("failed to restart new binary: %w", err)
	}

	logger.Log("[UPDATER] New instance launched (PID: %d). Exiting current process.", cmd.Process.Pid)

	// Clean tray icon before termination
	if globalApp != nil {
		globalApp.Cleanup()
	}

	os.Exit(0)
	return nil
}

// httpGet executes an HTTP GET using Windows native wininet.dll with fallback to curl.exe.
func httpGet(urlStr string) ([]byte, int, error) {
	// Try native wininet.dll
	data, code, err := wininetGet(urlStr)
	if err == nil && code == 200 {
		return data, code, nil
	}

	// Fallback to curl.exe if wininet failed
	curlCmd := exec.Command("curl.exe", "-s", "-L", "-H", "User-Agent: MouseBatteryTray/"+Version, "-H", "Accept: application/vnd.github.v3+json", urlStr)
	curlOut, curlErr := curlCmd.Output()
	if curlErr == nil && len(curlOut) > 0 {
		return curlOut, 200, nil
	}

	if err != nil {
		return nil, code, err
	}
	return data, code, nil
}

// downloadFile downloads a URL to a local file using wininet.dll with fallback to curl.exe.
func downloadFile(urlStr, destPath string) error {
	// Try native wininet.dll
	err := wininetDownload(urlStr, destPath)
	if err == nil {
		if fi, statErr := os.Stat(destPath); statErr == nil && fi.Size() > 500*1024 {
			return nil
		}
	}

	// Fallback to curl.exe
	curlCmd := exec.Command("curl.exe", "-s", "-L", "-H", "User-Agent: MouseBatteryTray/"+Version, "-o", destPath, urlStr)
	if curlErr := curlCmd.Run(); curlErr == nil {
		if fi, statErr := os.Stat(destPath); statErr == nil && fi.Size() > 500*1024 {
			return nil
		}
	}

	if err != nil {
		return err
	}
	return fmt.Errorf("download incomplete or corrupt")
}

func wininetGet(urlStr string) ([]byte, int, error) {
	agentW, _ := syscall.UTF16PtrFromString("MouseBatteryTray/" + Version)
	hInternet, _, err := procInternetOpenW.Call(
		uintptr(unsafe.Pointer(agentW)),
		INTERNET_OPEN_TYPE_PRECONFIG,
		0, 0, 0,
	)
	if hInternet == 0 {
		return nil, 0, fmt.Errorf("InternetOpenW failed: %v", err)
	}
	defer procInternetCloseHandle.Call(hInternet)

	urlW, _ := syscall.UTF16PtrFromString(urlStr)
	headersW, _ := syscall.UTF16PtrFromString("Accept: application/vnd.github.v3+json\r\nUser-Agent: MouseBatteryTray/" + Version + "\r\n")

	flags := uintptr(INTERNET_FLAG_RELOAD | INTERNET_FLAG_NO_CACHE_WRITE | INTERNET_FLAG_SECURE)
	hUrl, _, err := procInternetOpenUrlW.Call(
		hInternet,
		uintptr(unsafe.Pointer(urlW)),
		uintptr(unsafe.Pointer(headersW)),
		^uintptr(0),
		flags,
		0,
	)
	if hUrl == 0 {
		return nil, 0, fmt.Errorf("InternetOpenUrlW failed: %v", err)
	}
	defer procInternetCloseHandle.Call(hUrl)

	var statusCode uint32
	var statusSize uint32 = 4
	var index uint32 = 0
	r, _, _ := procHttpQueryInfoW.Call(
		hUrl,
		uintptr(HTTP_QUERY_STATUS_CODE|HTTP_QUERY_FLAG_NUMBER),
		uintptr(unsafe.Pointer(&statusCode)),
		uintptr(unsafe.Pointer(&statusSize)),
		uintptr(unsafe.Pointer(&index)),
	)
	if r == 0 {
		statusCode = 0
	}

	var data []byte
	buf := make([]byte, 32768)
	for {
		var bytesRead uint32
		r, _, err := procInternetReadFile.Call(
			hUrl,
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(len(buf)),
			uintptr(unsafe.Pointer(&bytesRead)),
		)
		if r == 0 {
			return nil, int(statusCode), fmt.Errorf("InternetReadFile failed: %v", err)
		}
		if bytesRead == 0 {
			break
		}
		data = append(data, buf[:bytesRead]...)
	}

	return data, int(statusCode), nil
}

func wininetDownload(urlStr, destPath string) error {
	agentW, _ := syscall.UTF16PtrFromString("MouseBatteryTray/" + Version)
	hInternet, _, err := procInternetOpenW.Call(
		uintptr(unsafe.Pointer(agentW)),
		INTERNET_OPEN_TYPE_PRECONFIG,
		0, 0, 0,
	)
	if hInternet == 0 {
		return fmt.Errorf("InternetOpenW failed: %v", err)
	}
	defer procInternetCloseHandle.Call(hInternet)

	urlW, _ := syscall.UTF16PtrFromString(urlStr)
	headersW, _ := syscall.UTF16PtrFromString("Accept: application/octet-stream\r\nUser-Agent: MouseBatteryTray/" + Version + "\r\n")

	flags := uintptr(INTERNET_FLAG_RELOAD | INTERNET_FLAG_NO_CACHE_WRITE | INTERNET_FLAG_SECURE)
	hUrl, _, err := procInternetOpenUrlW.Call(
		hInternet,
		uintptr(unsafe.Pointer(urlW)),
		uintptr(unsafe.Pointer(headersW)),
		^uintptr(0),
		flags,
		0,
	)
	if hUrl == 0 {
		return fmt.Errorf("InternetOpenUrlW failed: %v", err)
	}
	defer procInternetCloseHandle.Call(hUrl)

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer f.Close()

	buf := make([]byte, 65536)
	for {
		var bytesRead uint32
		r, _, err := procInternetReadFile.Call(
			hUrl,
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(len(buf)),
			uintptr(unsafe.Pointer(&bytesRead)),
		)
		if r == 0 {
			return fmt.Errorf("InternetReadFile failed during download: %v", err)
		}
		if bytesRead == 0 {
			break
		}
		if _, writeErr := f.Write(buf[:bytesRead]); writeErr != nil {
			return fmt.Errorf("failed writing downloaded data: %w", writeErr)
		}
	}

	return nil
}

// CompareVersions compares two semver-like strings (e.g., "v0.1.0-beta", "v0.1.1").
// Returns:
//
//	 1 if target > current (newer version available)
//	 0 if target == current
//	-1 if target < current (target is older)
func CompareVersions(current, target string) int {
	cNorm := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(current, "v"), "V"))
	tNorm := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(target, "v"), "V"))

	if cNorm == tNorm {
		return 0
	}

	// If current is "dev", any non-empty release tag is considered newer
	if cNorm == "dev" {
		if tNorm != "" && tNorm != "dev" {
			return 1
		}
		return 0
	}
	if tNorm == "dev" {
		return -1
	}

	cCore, cPre := splitCoreAndPre(cNorm)
	tCore, tPre := splitCoreAndPre(tNorm)

	cParts := parseSemverParts(cCore)
	tParts := parseSemverParts(tCore)

	for i := 0; i < 3; i++ {
		if tParts[i] > cParts[i] {
			return 1
		}
		if tParts[i] < cParts[i] {
			return -1
		}
	}

	// Cores are equal: a version without prerelease is newer than one with prerelease (e.g. 0.1.0 > 0.1.0-beta)
	if cPre != "" && tPre == "" {
		return 1
	}
	if cPre == "" && tPre != "" {
		return -1
	}

	// Compare prerelease strings
	if tPre > cPre {
		return 1
	}
	if tPre < cPre {
		return -1
	}

	return 0
}

func splitCoreAndPre(v string) (string, string) {
	if idx := strings.Index(v, "-"); idx >= 0 {
		return v[:idx], v[idx+1:]
	}
	return v, ""
}

func parseSemverParts(core string) [3]int {
	var parts [3]int
	tokens := strings.Split(core, ".")
	for i := 0; i < len(tokens) && i < 3; i++ {
		val, _ := strconv.Atoi(tokens[i])
		parts[i] = val
	}
	return parts
}
