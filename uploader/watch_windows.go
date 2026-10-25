//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

const (
	notifyNames    = 0x1 | 0x2  // FILE_NOTIFY_CHANGE_FILE_NAME | DIR_NAME
	notifyContents = 0x8 | 0x10 // FILE_NOTIFY_CHANGE_SIZE | LAST_WRITE
	waitFailed     = 0xFFFFFFFF
	invalidHandle  = ^uintptr(0)
)

var (
	createEvent     = kernel32.NewProc("CreateEventW")
	setEvent        = kernel32.NewProc("SetEvent")
	findFirstChange = kernel32.NewProc("FindFirstChangeNotificationW")
	findCloseChange = kernel32.NewProc("FindCloseChangeNotification")
	waitForMultiple = kernel32.NewProc("WaitForMultipleObjects")
)

// watcher waits on change notification handles and an auto-reset event that wake sets.
type watcher struct{ event uintptr }

func newWatcher() (*watcher, error) {
	event, _, err := createEvent.Call(0, 0, 0, 0)
	if event == 0 {
		return nil, err
	}
	return &watcher{event}, nil
}

func (w *watcher) wake() { setEvent.Call(w.event) }

func (w *watcher) close() { syscall.CloseHandle(syscall.Handle(w.event)) }

// The Account folder is watched with every folder below it, so a new account folder or save wakes the program; the
// Screenshots folder reports its files' writes, so a screenshot finishing wakes it too.
func watchTargets(roots []string, config string, _ []string) []watchTarget {
	targets := []watchTarget{{path: config}}
	for _, root := range roots {
		if shots, complete := screenshotFolder(root); complete {
			targets = append(targets, watchTarget{path: shots, tree: true})
		} else {
			targets = append(targets, watchTarget{path: shots})
		}
		if dir, complete := accountFolder(root); dir != "" {
			targets = append(targets, watchTarget{path: dir, tree: complete})
		}
	}
	return targets
}

// wait arms the watches, calls armed, and blocks until a watched folder changes or wake is called. A folder watched without its tree reports only
// entries added, removed or renamed, as on macOS, so the program's own log appends never wake it.
func (w *watcher) wait(targets []watchTarget, armed func()) error {
	handles := []uintptr{w.event}
	for _, t := range targets {
		filter, tree := uintptr(notifyNames), uintptr(0)
		if t.tree {
			filter, tree = notifyNames|notifyContents, 1
		}
		handle, _, _ := findFirstChange.Call(uintptr(unsafe.Pointer(utf16(t.path))), tree, filter)
		if handle == invalidHandle {
			continue
		}
		defer findCloseChange.Call(handle)
		handles = append(handles, handle)
	}
	armed()
	result, _, err := waitForMultiple.Call(uintptr(len(handles)), uintptr(unsafe.Pointer(&handles[0])), 0, syscall.INFINITE)
	if result == waitFailed {
		return err
	}
	return nil
}
