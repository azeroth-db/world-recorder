//go:build windows

package main

import (
	"encoding/csv"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	blizzardKey      = `SOFTWARE\WOW6432Node\Blizzard Entertainment\World of Warcraft`
	runKey           = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue         = "WowdbRecorderUploader"
	createNoWindow   = 0x08000000
	detachedProcess  = 0x00000008
	errorFileMissing = syscall.Errno(2)
	mbIconError      = 0x10
	mbIconInfo       = 0x40
	mbSetForeground  = 0x10000
)

var (
	flavorDir             = regexp.MustCompile(`^_.+_$`)
	advapi32              = syscall.NewLazyDLL("advapi32.dll")
	regSetValue           = advapi32.NewProc("RegSetValueExW")
	regDeleteValue        = advapi32.NewProc("RegDeleteValueW")
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	getConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	freeConsole           = kernel32.NewProc("FreeConsole")
	getConsoleWindow      = kernel32.NewProc("GetConsoleWindow")
	messageBox            = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
)

func defaultInstalls() []string {
	var roots []string
	for _, candidate := range []string{registryInstall(), `C:\Program Files (x86)\World of Warcraft`} {
		if candidate != "" && !slices.Contains(roots, candidate) {
			roots = append(roots, candidate)
		}
	}
	return roots
}

func registryInstall() string {
	path, err := readRegistryString(syscall.HKEY_LOCAL_MACHINE, blizzardKey, "InstallPath")
	if err != nil || path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if flavorDir.MatchString(filepath.Base(path)) {
		return filepath.Dir(path)
	}
	return path
}

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func readRegistryString(root syscall.Handle, path, name string) (string, error) {
	var key syscall.Handle
	if err := syscall.RegOpenKeyEx(root, utf16(path), 0, syscall.KEY_READ, &key); err != nil {
		return "", err
	}
	defer syscall.RegCloseKey(key)
	var kind, size uint32
	if err := syscall.RegQueryValueEx(key, utf16(name), nil, &kind, nil, &size); err != nil {
		return "", err
	}
	buffer := make([]uint16, size/2+1)
	if err := syscall.RegQueryValueEx(key, utf16(name), nil, &kind, (*byte)(unsafe.Pointer(&buffer[0])), &size); err != nil {
		return "", err
	}
	return syscall.UTF16ToString(buffer), nil
}

func withRunKey(action func(syscall.Handle) error) error {
	var key syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, utf16(runKey), 0, syscall.KEY_SET_VALUE, &key); err != nil {
		return err
	}
	defer syscall.RegCloseKey(key)
	return action(key)
}

func errno(code uintptr) error {
	if code != 0 {
		return syscall.Errno(code)
	}
	return nil
}

func hidden(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}

func gameRunning() (bool, error) {
	out, err := hidden(exec.Command("tasklist", "/FO", "CSV", "/NH")).Output()
	if err != nil {
		return false, err
	}
	rows, err := csv.NewReader(strings.NewReader(string(out))).ReadAll()
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(rows, func(row []string) bool {
		name := strings.ToLower(row[0])
		return strings.HasPrefix(name, "wow") && strings.HasSuffix(name, ".exe")
	}), nil
}

// consoleShared reports whether another process, such as a shell, shares this console.
func consoleShared() bool {
	var ids [2]uint32
	count, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids)))
	return count > 1
}

func detachConsole() {
	if !consoleShared() {
		freeConsole.Call()
	}
}

// Explorer gives a double-clicked console program a console of its own.
func hasTerminal() bool {
	window, _, _ := getConsoleWindow.Call()
	return window != 0 && consoleShared()
}

func bundle(exe string) (source, name, inner string) { return exe, "wowdb-recorder.exe", "" }

// stopOthers ends running copies of the installed uploader so its file can be replaced.
func stopOthers(installed string) {
	hidden(exec.Command("taskkill", "/F", "/FI", "IMAGENAME eq "+filepath.Base(installed),
		"/FI", "PID ne "+strconv.Itoa(os.Getpid()))).Run()
}

func openBrowser(address string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", address).Run()
}

func showDialog(text string, failed bool) {
	icon := uintptr(mbIconInfo)
	if failed {
		icon = mbIconError
	}
	messageBox.Call(0, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16("World Recorder"))), icon|mbSetForeground)
}

func register(exe string, args []string) error {
	var quoted []string
	for _, arg := range append([]string{exe}, args...) {
		quoted = append(quoted, syscall.EscapeArg(arg))
	}
	if err := setRunValue(strings.Join(quoted, " ")); err != nil {
		return err
	}
	start := exec.Command(exe, args...)
	start.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP}
	return start.Start()
}

func setRunValue(command string) error {
	data, err := syscall.UTF16FromString(command)
	if err != nil {
		return err
	}
	return withRunKey(func(key syscall.Handle) error {
		code, _, _ := regSetValue.Call(uintptr(key), uintptr(unsafe.Pointer(utf16(runValue))), 0, syscall.REG_SZ,
			uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2))
		return errno(code)
	})
}

func unregister() error {
	return withRunKey(func(key syscall.Handle) error {
		code, _, _ := regDeleteValue.Call(uintptr(key), uintptr(unsafe.Pointer(utf16(runValue))))
		if err := errno(code); err != nil && !errors.Is(err, errorFileMissing) {
			return err
		}
		return nil
	})
}

func computerName() string {
	name, _ := os.Hostname()
	return name
}
