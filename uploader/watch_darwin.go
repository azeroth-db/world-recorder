//go:build darwin

package main

import (
	"path/filepath"
	"syscall"
)

const (
	wakeIdent    = 1
	vnodeChanges = syscall.NOTE_WRITE | syscall.NOTE_EXTEND | syscall.NOTE_ATTRIB | syscall.NOTE_DELETE | syscall.NOTE_RENAME | syscall.NOTE_REVOKE
)

// watcher waits on a kqueue: vnode events for each watched path and a user event that wake triggers.
type watcher struct{ kq int }

func newWatcher() (*watcher, error) {
	kq, err := syscall.Kqueue()
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(kq)
	user := syscall.Kevent_t{Ident: wakeIdent, Filter: syscall.EVFILT_USER, Flags: syscall.EV_ADD | syscall.EV_CLEAR}
	if _, err := syscall.Kevent(kq, []syscall.Kevent_t{user}, nil, nil); err != nil {
		syscall.Close(kq)
		return nil, err
	}
	return &watcher{kq}, nil
}

func (w *watcher) close() { syscall.Close(w.kq) }

func (w *watcher) wake() {
	trigger := syscall.Kevent_t{Ident: wakeIdent, Filter: syscall.EVFILT_USER, Fflags: syscall.NOTE_TRIGGER}
	syscall.Kevent(w.kq, []syscall.Kevent_t{trigger}, nil, nil)
}

// A folder reports its entries changing; each save file is watched itself, because the game can rewrite it in place,
// and so is each screenshot the game was still writing.
func watchTargets(roots []string, config string, unread []string) []watchTarget {
	targets := []watchTarget{{path: config}}
	for _, path := range unread {
		targets = append(targets, watchTarget{path: path})
	}
	for _, root := range roots {
		shots, _ := screenshotFolder(root)
		targets = append(targets, watchTarget{path: shots})
		dir, complete := accountFolder(root)
		if dir == "" {
			continue
		}
		targets = append(targets, watchTarget{path: dir})
		if !complete {
			continue
		}
		for _, account := range subdirectories(dir) {
			saved := filepath.Join(dir, account, "SavedVariables")
			targets = append(targets, watchTarget{path: filepath.Join(dir, account)}, watchTarget{path: saved}, watchTarget{path: filepath.Join(saved, saveName)})
		}
	}
	return targets
}

// wait arms the watches, calls armed, and blocks until a watched path changes or wake is called. Closing the
// descriptors removes their events.
func (w *watcher) wait(targets []watchTarget, armed func()) error {
	var changes []syscall.Kevent_t
	for _, t := range targets {
		fd, err := syscall.Open(t.path, syscall.O_EVTONLY|syscall.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		defer syscall.Close(fd)
		changes = append(changes, syscall.Kevent_t{Ident: uint64(fd), Filter: syscall.EVFILT_VNODE, Flags: syscall.EV_ADD | syscall.EV_CLEAR, Fflags: vnodeChanges})
	}
	if err := kevent(w.kq, changes, nil); err != nil {
		return err
	}
	armed()
	return kevent(w.kq, nil, make([]syscall.Kevent_t, 1))
}

func kevent(kq int, changes, events []syscall.Kevent_t) error {
	for {
		_, err := syscall.Kevent(kq, changes, events, nil)
		if err != syscall.EINTR {
			return err
		}
	}
}
