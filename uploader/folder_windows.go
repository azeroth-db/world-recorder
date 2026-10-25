//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// IFileOpenDialog and IShellItem layouts/constants are the Windows SDK contracts.
type shellObject struct{ vtable *[29]uintptr }

//go:uintptrescapes
func (o *shellObject) call(method int, args ...uintptr) uintptr {
	values := append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)
	hr, _, _ := syscall.SyscallN(o.vtable[method], values...)
	runtime.KeepAlive(o)
	return hr
}
func comError(hr uintptr) error {
	if int32(hr) < 0 {
		return fmt.Errorf("Windows folder dialog failed (0x%08x)", uint32(hr))
	}
	return nil
}

func selectGameFolder(initial, reason string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	showDialog(reason+"\n\n"+pickerHint(), false)
	ole := syscall.NewLazyDLL("ole32.dll")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 2) // COINIT_APARTMENTTHREADED
	if err := comError(hr); err != nil {
		return "", err
	}
	defer ole.NewProc("CoUninitialize").Call()
	class := syscall.GUID{Data1: 0xdc1c5a9c, Data2: 0xe88a, Data3: 0x4dde, Data4: [8]byte{0xa5, 0xa1, 0x60, 0xf8, 0x2a, 0x20, 0xae, 0xf7}}
	iid := syscall.GUID{Data1: 0xd57c7288, Data2: 0xd4ad, Data3: 0x4768, Data4: [8]byte{0xbe, 0x02, 0x9d, 0x96, 0x95, 0x32, 0xd9, 0x60}}
	var dialog *shellObject
	hr, _, _ = ole.NewProc("CoCreateInstance").Call(uintptr(unsafe.Pointer(&class)), 0, 1, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&dialog)))
	if err := comError(hr); err != nil {
		return "", err
	}
	defer dialog.call(2)
	// FOS_PICKFOLDERS | FORCEFILESYSTEM | PATHMUSTEXIST | DONTADDTORECENT.
	if err := comError(dialog.call(9, 0x20|0x40|0x800|0x02000000)); err != nil {
		return "", err
	}
	if err := comError(dialog.call(17, uintptr(unsafe.Pointer(utf16("World Recorder — Select WoW Forever folder"))))); err != nil {
		return "", err
	}
	if err := comError(dialog.call(18, uintptr(unsafe.Pointer(utf16("Use this folder"))))); err != nil {
		return "", err
	}
	if info, err := os.Stat(initial); err == nil && info.IsDir() {
		itemID := syscall.GUID{Data1: 0x43826d1e, Data2: 0xe718, Data3: 0x42ee, Data4: [8]byte{0xbc, 0x55, 0xa1, 0xe2, 0x61, 0xc3, 0x7b, 0xfe}}
		var folder *shellObject
		hr, _, _ = syscall.NewLazyDLL("shell32.dll").NewProc("SHCreateItemFromParsingName").Call(uintptr(unsafe.Pointer(utf16(initial))), 0, uintptr(unsafe.Pointer(&itemID)), uintptr(unsafe.Pointer(&folder)))
		if comError(hr) == nil {
			dialog.call(12, uintptr(unsafe.Pointer(folder)))
			folder.call(2)
		}
	}
	hr = dialog.call(3, 0)
	if uint32(hr) == 0x800704c7 {
		return "", errSelectionCancelled
	} // ERROR_CANCELLED
	if err := comError(hr); err != nil {
		return "", err
	}
	var result *shellObject
	if err := comError(dialog.call(20, uintptr(unsafe.Pointer(&result)))); err != nil {
		return "", err
	}
	defer result.call(2)
	var path *uint16
	if err := comError(result.call(5, 0x80058000, uintptr(unsafe.Pointer(&path)))); err != nil {
		return "", err
	} // SIGDN_FILESYSPATH
	defer ole.NewProc("CoTaskMemFree").Call(uintptr(unsafe.Pointer(path)))
	var units []uint16
	for offset := uintptr(0); ; offset += 2 {
		value := *(*uint16)(unsafe.Add(unsafe.Pointer(path), offset))
		if value == 0 {
			break
		}
		units = append(units, value)
	}
	return syscall.UTF16ToString(units), nil
}

func validateClientExecutable(client, version string) error {
	// Each client names its executable after itself (WowB.exe, WowClassicB.exe, WowClassic.exe).
	paths, _ := filepath.Glob(filepath.Join(client, "Wow*.exe"))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			continue
		}
		found, err := executableVersion(path)
		if err == nil && found == version {
			return nil
		}
	}
	return fmt.Errorf("The WoW Forever executable is missing or does not match build %s. Finish or repair this installation in Battle.net.", version)
}

func executableVersion(path string) (string, error) {
	dll := syscall.NewLazyDLL("version.dll")
	name := utf16(path)
	size, _, err := dll.NewProc("GetFileVersionInfoSizeW").Call(uintptr(unsafe.Pointer(name)), 0)
	if size == 0 {
		return "", err
	}
	data := make([]byte, size)
	ok, _, err := dll.NewProc("GetFileVersionInfoW").Call(uintptr(unsafe.Pointer(name)), 0, size, uintptr(unsafe.Pointer(&data[0])))
	if ok == 0 {
		return "", err
	}
	query := dll.NewProc("VerQueryValueW")
	var translations *uint16
	var length uint32
	ok, _, err = query.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(utf16(`\VarFileInfo\Translation`))), uintptr(unsafe.Pointer(&translations)), uintptr(unsafe.Pointer(&length)))
	if ok == 0 || translations == nil {
		return "", fmt.Errorf("missing executable version translations: %v", err)
	}
	for offset := uintptr(0); offset+4 <= uintptr(length); offset += 4 {
		language := *(*uint16)(unsafe.Add(unsafe.Pointer(translations), offset))
		codepage := *(*uint16)(unsafe.Add(unsafe.Pointer(translations), offset+2))
		key := fmt.Sprintf(`\StringFileInfo\%04x%04x\FileVersion`, language, codepage)
		var text *uint16
		var count uint32
		ok, _, _ = query.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(utf16(key))), uintptr(unsafe.Pointer(&text)), uintptr(unsafe.Pointer(&count)))
		if ok == 0 || text == nil || count == 0 {
			continue
		}
		units := unsafe.Slice(text, count)
		value := syscall.UTF16ToString(units)
		runtime.KeepAlive(data)
		return strings.NewReplacer(",", ".", " ", "").Replace(strings.TrimSpace(value)), nil
	}
	return "", fmt.Errorf("missing executable FileVersion")
}
