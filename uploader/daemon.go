package main

import (
	"context"
	"errors"
	"maps"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// daemon is the resident program: one worker runs passes and commands in turn, so an update never
// replaces the program while a send is in flight. Linking waits for the browser outside the worker.
type daemon struct {
	s      *state
	opts   options
	origin string
	exe    string
	token  string
	stop   context.Context
	links  sync.WaitGroup
	wake   chan struct{}
	jobs   chan job
	force  atomic.Bool
	due    atomic.Bool
	seen   mark

	lookups *lookupRequests

	mu          sync.Mutex
	watcher     *watcher
	unread      []string
	wow         string
	results     map[string]saveResult
	busy        string
	queued      map[string]bool
	linking     bool
	update      *release
	failed      string
	server      *http.Server
	listenError string
	subscribers map[chan struct{}]bool
}

type job struct {
	busy string
	run  func() error
	done chan error
}

var errBusy = errors.New("already queued or running")

// mark is what decides whether a pass is due: the saves' sizes and times, the key and the game folder.
type mark struct {
	saves map[string]fileState
	key   string
	wow   string
}

func (m mark) equal(other mark) bool {
	return m.key == other.key && m.wow == other.wow && maps.EqualFunc(m.saves, other.saves, fileState.equal)
}

func (s *state) serve(opts options) error {
	origin, err := siteBase(opts.endpoint)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return err
	}
	d := &daemon{s: s, opts: opts, origin: origin, exe: exe, wow: opts.wow, wake: make(chan struct{}, 1), jobs: make(chan job),
		results: map[string]saveResult{}, queued: map[string]bool{}, subscribers: map[chan struct{}]bool{}, lookups: newLookupRequests()}
	if d.token, err = s.writeControlToken(); err != nil {
		s.log("control result=failed: %v", pathless(err))
	}
	d.stop, _ = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go d.exitOnSignal()
	if w, err := newWatcher(); err != nil {
		s.log("watch result=failed: %v", err)
	} else {
		d.watcher = w
		go d.watch()
	}
	go d.checkEvery()
	// The first pass sends only when sending is not paused; it also checks for a release.
	d.force.Store(!s.readSettings().Paused)
	d.signal()
	d.work()
	return nil
}

// exitOnSignal lets a link in progress withdraw its request before the program ends.
func (d *daemon) exitOnSignal() {
	<-d.stop.Done()
	d.links.Wait()
	os.Exit(0)
}

// contractCheck is how often the program asks wowdb.wiki which clients it reads and whether a release is out while the
// game writes nothing (owner-set), so a client the contract adds, such as WoW Forever's launch client, gets the addon
// and a release installs without a game session.
const contractCheck = 15 * time.Minute

func (d *daemon) checkEvery() {
	for range time.Tick(contractCheck) {
		d.due.Store(true)
		d.signal()
	}
}

func (d *daemon) signal() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *daemon) work() {
	for {
		select {
		case <-d.wake:
			d.bind()
			d.wakeUp()
		case j := <-d.jobs:
			d.bind()
			d.setBusy(j.busy)
			err := j.run()
			d.mu.Lock()
			delete(d.queued, j.busy)
			d.mu.Unlock()
			d.setBusy("")
			j.done <- err
		}
	}
}

// bind serves the API port and, the first time it succeeds, completes an update that restarted this program and
// starts the desktop app in the tray.
func (d *daemon) bind() {
	if d.listen() {
		d.finishUpdate()
		d.startControl(false)
	}
}

// listen binds the API port and reports whether it did now; while another program holds the port, each later
// wake tries again and uploads go on.
func (d *daemon) listen() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.server != nil {
		return false
	}
	listener, err := net.Listen("tcp", apiAddress)
	if err != nil {
		if err.Error() != d.listenError {
			d.s.log("listen result=failed: %v", err)
			d.listenError = err.Error()
		}
		return false
	}
	d.listenError = ""
	d.server = &http.Server{Handler: d.routes()}
	go d.server.Serve(listener)
	return true
}

func (d *daemon) closeListener() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.server != nil {
		d.server.Close()
		d.server = nil
	}
}

func (d *daemon) current() mark {
	wow := d.gameFolder()
	return mark{saves: snapshot(findSaves(installs(wow))), key: d.s.readKey(), wow: wow}
}

// wakeUp sends what changed, then looks for a release on every wake, so entering the world updates a program whose
// last session crashed before writing a save.
func (d *daemon) wakeUp() {
	defer d.checkRelease()
	d.openLookups()
	if d.due.Swap(false) {
		d.s.readClients(d.opts.endpoint)
		d.s.refreshAddons(d.opts.endpoint, installs(d.gameFolder()))
	}
	forced := d.force.Swap(false)
	now := d.current()
	if !forced && now.equal(d.seen) {
		return
	}
	if !forced && d.s.readSettings().Paused {
		d.broadcast()
		return
	}
	d.seen = now
	d.runPass()
}

// openLookups opens the pages the addon's View on website clicks asked for; a screenshot still being written is
// watched until it is complete.
func (d *daemon) openLookups() {
	d.lookups.open(d.s, d.origin, screenshotFolders(installs(d.gameFolder())))
	d.mu.Lock()
	changed := !slices.Equal(d.unread, d.lookups.unread)
	d.unread = d.lookups.unread
	d.mu.Unlock()
	if changed {
		d.rewatch()
	}
}

func (d *daemon) runPass() {
	d.setBusy("pass")
	defer d.setBusy("")
	results, _ := d.s.pass(d.gameFolder(), d.opts.endpoint)
	byPath := map[string]saveResult{}
	for _, r := range results {
		byPath[r.path] = r
	}
	d.mu.Lock()
	d.results = byPath
	d.mu.Unlock()
}

// submit hands a command to the worker; a second command of the same kind while one is queued or running is refused.
func (d *daemon) submit(busy string, run func() error) (chan error, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if busy != "" && d.queued[busy] {
		return nil, errBusy
	}
	if busy != "" {
		d.queued[busy] = true
	}
	j := job{busy: busy, run: run, done: make(chan error, 1)}
	go func() { d.jobs <- j }()
	return j.done, nil
}

func (d *daemon) watch() {
	for {
		d.mu.Lock()
		w, unread := d.watcher, d.unread
		d.mu.Unlock()
		// After the watches are armed, one check catches a write that landed before them.
		err := w.wait(watchTargets(watchRoots(d.gameFolder()), d.s.dir, unread), d.signal)
		if err == nil {
			d.signal()
			continue
		}
		d.s.log("watch result=failed: %v", err)
		w.close()
		fresh, err := newWatcher()
		d.mu.Lock()
		d.watcher = fresh
		d.mu.Unlock()
		if err != nil {
			d.s.log("watch result=failed: %v", err)
			return
		}
	}
}

// rewatch rebuilds the watched folders, after the game folder changed.
func (d *daemon) rewatch() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.watcher != nil {
		d.watcher.wake()
	}
}

func (d *daemon) gameFolder() string {
	d.mu.Lock()
	wow := d.wow
	d.mu.Unlock()
	if wow == "" {
		wow = savedGameFolder()
	}
	return wow
}

func (d *daemon) setBusy(busy string) {
	d.mu.Lock()
	d.busy = busy
	d.mu.Unlock()
	d.broadcast()
}

func (d *daemon) subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	d.mu.Lock()
	d.subscribers[ch] = true
	d.mu.Unlock()
	return ch
}

func (d *daemon) unsubscribe(ch chan struct{}) {
	d.mu.Lock()
	delete(d.subscribers, ch)
	d.mu.Unlock()
}

func (d *daemon) broadcast() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for ch := range d.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
