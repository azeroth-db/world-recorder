package main

import (
	"errors"
	"os"
	"runtime"
)

type status struct {
	App          string       `json:"app"`
	API          int          `json:"api"`
	Version      string       `json:"version"`
	Platform     string       `json:"platform"`
	Computer     string       `json:"computer"`
	Key          *string      `json:"key"`
	Paused       bool         `json:"paused"`
	AutoUpdate   bool         `json:"autoUpdate"`
	StartAtLogin bool         `json:"startAtLogin"`
	Folder       folderStatus `json:"folder"`
	Addon        addonStatus  `json:"addon"`
	Saves        []saveStatus `json:"saves"`
	Sends        []sendStatus `json:"sends"`
	Busy         *string      `json:"busy"`
	Update       *newRelease  `json:"update"`
}

type folderStatus struct {
	Root    *string      `json:"root"`
	Error   *string      `json:"error"`
	Unknown []seenClient `json:"unknown"`
}

type addonStatus struct {
	Bundled   string  `json:"bundled"`
	Installed *string `json:"installed"`
}

type saveStatus struct {
	ID        string `json:"id"`
	Flavor    string `json:"flavor"`
	WrittenAt int64  `json:"writtenAt"`
	State     string `json:"state"`
	SentAt    *int64 `json:"sentAt"`
	Bytes     int64  `json:"bytes"`
}

type sendStatus struct {
	At     int64  `json:"at"`
	Bytes  int    `json:"bytes"`
	Result string `json:"result"`
}

type newRelease struct {
	Version string `json:"version"`
}

// status reads the disk without the lock, then takes the worker's last results under it.
func (d *daemon) status() status {
	v := d.s.readSettings()
	key, wow, sends := d.s.readKey(), d.gameFolder(), d.s.readSends()
	saves := findSaves(installs(wow))
	st := status{App: "world-recorder", API: 1, Version: programVersion(), Platform: runtime.GOOS, Computer: computerName(),
		Paused: v.Paused, AutoUpdate: v.AutoUpdate, StartAtLogin: loginItemExists(),
		Folder: folderState(wow), Addon: addonStatus{Bundled: programVersion(), Installed: installedAddon(wow)},
		Saves: []saveStatus{}, Sends: []sendStatus{}}
	if key != "" {
		st.Key = optional(digest(key))
	}
	for _, r := range sends {
		st.Sends = append(st.Sends, sendStatus{At: r.At, Bytes: r.Bytes, Result: r.Result})
	}
	d.mu.Lock()
	results, busy, update := d.results, d.busy, d.update
	if busy == "" && d.linking {
		busy = "link"
	}
	d.mu.Unlock()
	for _, sv := range saves {
		st.Saves = append(st.Saves, saveState(sv, key, results[sv.path], sends))
	}
	if busy != "" {
		st.Busy = optional(busy)
	}
	if update != nil {
		st.Update = &newRelease{Version: update.Version}
	}
	return st
}

// saveState keeps the last pass's state while the file is unchanged; a changed file is pending.
func saveState(sv save, key string, last saveResult, sends []sendRecord) saveStatus {
	file, id := stat(sv.path), digest(sv.path)
	out := saveStatus{ID: id, Flavor: sv.flavor, WrittenAt: file.modTime.UnixMilli(), Bytes: file.size, State: "pending"}
	switch {
	case key == "":
		out.State = "unlinked"
	case last.state != "" && last.file.equal(file) && last.state != "unlinked":
		out.State = last.state
	}
	for _, r := range sends {
		if r.Save == id && r.Result != "failed" {
			out.SentAt = &r.At
			break
		}
	}
	return out
}

func folderState(wow string) folderStatus {
	if wow == "" {
		return folderStatus{Error: optional("No game folder was selected.")}
	}
	if _, err := validateInstall(wow); err != nil {
		var unknown unknownClients
		errors.As(err, &unknown)
		return folderStatus{Root: optional(wow), Error: optional(err.Error()), Unknown: unknown}
	}
	return folderStatus{Root: optional(wow)}
}

func installedAddon(wow string) *string {
	i, err := validateInstall(wow)
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(addonTOC(i))
	if version := tocVersion(data); err == nil && version != "" {
		return &version
	}
	return nil
}

func optional(text string) *string { return &text }

var errCancelled = errors.New("cancelled")

// chooseFolder opens the folder picker at the current folder, validates the choice as setup does and keeps it.
func (d *daemon) chooseFolder() error {
	root, err := pickInstall(d.gameFolder(), "Choose the WoW Forever folder World Recorder reads.")
	if errors.Is(err, errSelectionCancelled) {
		return errCancelled
	}
	if err != nil {
		return err
	}
	if err := d.s.write("game-folder", []byte(root+"\n")); err != nil {
		return pathless(err)
	}
	d.mu.Lock()
	d.wow = root
	d.mu.Unlock()
	d.rewatch()
	if loginItemExists() {
		return setLoginItem(true, d.exe, d.loginArgs())
	}
	return nil
}

func (d *daemon) loginArgs() []string {
	return append([]string{"run"}, pinGameFolder(d.opts, d.gameFolder()).args...)
}

func (d *daemon) installAddon() error {
	_, err := installAddon([]string{d.gameFolder()})
	return err
}

// applySettings stores paused and autoUpdate; startAtLogin writes or removes the login item. Resuming wakes the worker.
func (d *daemon) applySettings(patch settingsPatch) error {
	if patch.StartAtLogin != nil && *patch.StartAtLogin != loginItemExists() {
		if err := setLoginItem(*patch.StartAtLogin, d.exe, d.loginArgs()); err != nil {
			return err
		}
	}
	before := d.s.readSettings()
	after := patch.apply(before)
	if after != before {
		if err := d.s.writeSettings(after); err != nil {
			return pathless(err)
		}
	}
	if before.Paused && !after.Paused {
		d.signal()
	}
	return nil
}

// startLink runs the browser link flow beside the worker, so passes and commands go on while the player approves.
// A new key wakes the worker, which sends unless sending is paused.
func (d *daemon) startLink() error {
	d.mu.Lock()
	if d.linking {
		d.mu.Unlock()
		return errBusy
	}
	d.linking = true
	d.mu.Unlock()
	d.broadcast()
	d.links.Add(1)
	go func() {
		defer d.links.Done()
		d.s.link(d.stop, d.opts.endpoint) // link logs its own result
		d.mu.Lock()
		d.linking = false
		d.mu.Unlock()
		d.signal()
		d.broadcast()
	}()
	return nil
}

// sendNow is the player's own request, so it sends while paused too.
func (d *daemon) sendNow() {
	d.force.Store(true)
	d.signal()
}

// uninstall removes the login item and exits, even when the removal failed. On macOS removing it ends this
// process, so the log line comes first.
func (d *daemon) uninstall() {
	d.s.log("uninstall result=started")
	if err := unregister(); err != nil {
		d.s.log("uninstall result=failed: %v", err)
	}
	os.Exit(0)
}
