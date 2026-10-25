//go:build windows

package main

import (
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
	agent := filepath.Join(os.Getenv("ProgramData"), "Battle.net", "Agent", "product.db")
	for _, candidate := range append(battleNetInstalls(agent), registryInstalls()...) {
		if candidate != "" && !slices.Contains(roots, candidate) {
			roots = append(roots, candidate)
		}
	}
	for _, variable := range []string{"ProgramFiles(x86)", "ProgramW6432", "ProgramFiles"} {
		if dir := os.Getenv(variable); dir != "" {
			roots = append(roots, filepath.Join(dir, "World of Warcraft"))
		}
	}
	return roots
}

func registryInstalls() []string {
	var roots []string
	for _, hive := range []syscall.Handle{syscall.HKEY_LOCAL_MACHINE, syscall.HKEY_CURRENT_USER} {
		for _, prefix := range []string{`SOFTWARE\WOW6432Node\Blizzard Entertainment\`, `SOFTWARE\Blizzard Entertainment\`} {
			for _, product := range []string{"World of Warcraft", "World of Warcraft Beta"} {
				for _, value := range []string{"InstallPath", "InstallLocation"} {
					path, err := readRegistryString(hive, prefix+product, value)
					if err != nil || path == "" {
						continue
					}
					path = filepath.Clean(path)
					if flavorDir.MatchString(filepath.Base(path)) {
						path = filepath.Dir(path)
					}
					roots = append(roots, path)
				}
			}
		}
	}
	return roots
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
	if kind != syscall.REG_SZ && kind != syscall.REG_EXPAND_SZ {
		return "", errors.New("registry install path is not a string")
	}
	pathValue := syscall.UTF16ToString(buffer)
	if kind == syscall.REG_EXPAND_SZ {
		expand := kernel32.NewProc("ExpandEnvironmentStringsW")
		input := utf16(pathValue)
		length, _, err := expand.Call(uintptr(unsafe.Pointer(input)), 0, 0)
		if length == 0 {
			return "", err
		}
		output := make([]uint16, length)
		written, _, err := expand.Call(uintptr(unsafe.Pointer(input)), uintptr(unsafe.Pointer(&output[0])), length)
		if written == 0 || written > length {
			return "", err
		}
		pathValue = syscall.UTF16ToString(output)
	}
	return pathValue, nil
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
	if err := setRunValue(runCommand(exe, args)); err != nil {
		return err
	}
	start := exec.Command(exe, args...)
	start.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP}
	return start.Start()
}

// setLoginItem writes or removes the Run value; the running program stays.
func setLoginItem(on bool, exe string, args []string) error {
	if !on {
		return unregister()
	}
	return setRunValue(runCommand(exe, args))
}

func runCommand(exe string, args []string) string {
	var quoted []string
	for _, arg := range append([]string{exe}, args...) {
		quoted = append(quoted, syscall.EscapeArg(arg))
	}
	return strings.Join(quoted, " ")
}

// swapProgram renames the running program aside, which Windows allows, moves the new one into its place and
// returns where the previous one is kept.
func swapProgram(file, exe string) (string, error) { return swapIn(file, exe) }

// restartProgram starts the new program with the same arguments and ends this one.
func restartProgram(exe string) error {
	start := exec.Command(exe, os.Args[1:]...)
	start.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP}
	if err := start.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

func loginItemExists() bool {
	var key syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, utf16(runKey), 0, syscall.KEY_READ, &key) != nil {
		return false
	}
	defer syscall.RegCloseKey(key)
	var size uint32
	return syscall.RegQueryValueEx(key, utf16(runValue), nil, nil, nil, &size) == nil
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
