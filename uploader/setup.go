package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// addonArchive is the addon package the signed release.json names, read from the file whose SHA-256 was checked.
type addonArchive struct {
	*zip.Reader
	file *os.File
}

func (a addonArchive) Close() error { return a.file.Close() }

// addonPackage is the addon this program installs: the archive of the signed release.json, kept in the configuration
// folder as addon.zip beside that release.json as addon.json. The kept archive serves while its release is not older
// than the program; otherwise the release the site serves now is downloaded and kept.
func (s *state) addonPackage(endpoint string) (addonArchive, error) {
	if kept, err := s.keptAddon(); err == nil {
		return kept, nil
	}
	origin, err := siteBase(endpoint)
	if err != nil {
		return addonArchive{}, err
	}
	r, err := fetchRelease(origin)
	if err != nil {
		return addonArchive{}, fmt.Errorf("The World Recorder addon could not be downloaded from Azeroth DB: %v.", err)
	}
	if newer(programVersion, r.Version) {
		return addonArchive{}, fmt.Errorf("Azeroth DB serves the World Recorder addon %s, older than this program's %s.", r.Version, programVersion)
	}
	archive, err := s.download(origin, r.Addon)
	if err == nil {
		err = os.Rename(archive, s.path(addonFile))
	}
	if err == nil {
		err = s.writeJSON(addonReleaseFile, r)
	}
	if err != nil {
		os.Remove(archive)
		return addonArchive{}, fmt.Errorf("The World Recorder addon could not be downloaded from Azeroth DB: %v.", pathless(err))
	}
	return s.keptAddon()
}

const (
	addonFile        = "addon.zip"
	addonReleaseFile = "addon.json"
)

// keptAddon opens the kept archive only when its release carries the release key's signature, is not older than the
// program and names the SHA-256 of the file as opened.
func (s *state) keptAddon() (addonArchive, error) {
	var r release
	data, err := os.ReadFile(s.path(addonReleaseFile))
	if err != nil || json.Unmarshal(data, &r) != nil || !r.Verify(releaseKey) || newer(programVersion, r.Version) {
		return addonArchive{}, errors.New("no kept addon of this release")
	}
	file, err := os.Open(s.path(addonFile))
	if err != nil {
		return addonArchive{}, err
	}
	sum := sha256.New()
	size, err := io.Copy(sum, file)
	if err == nil && !strings.EqualFold(hex.EncodeToString(sum.Sum(nil)), r.Addon.SHA256) {
		err = errors.New("the kept addon does not match its SHA-256")
	}
	var files *zip.Reader
	if err == nil {
		files, err = zip.NewReader(file, size)
	}
	if err != nil {
		file.Close()
		return addonArchive{}, err
	}
	return addonArchive{Reader: files, file: file}, nil
}

// setup from a double-click links through the browser, then opens the World Recorder window of the installed
// program; a bare binary without that window opens the player's World Recorder page instead. Only a failure shows
// a dialog.
func (s *state) setup(opts options, dialog bool) error {
	linked := s.keyAccepted(opts.endpoint)
	report, exe, err := s.installAll(opts)
	switch {
	case !dialog:
		fmt.Println(strings.Join(report, "\n"))
	case err != nil:
		showDialog(err.Error(), true)
	case linked:
		if base, err := siteBase(opts.endpoint); err == nil && s.openControl(exe, base) != nil {
			openBrowser(base + "/account/world-recorder")
		}
	}
	return err
}

// installAll also returns the installed program, once it is installed.
func (s *state) installAll(opts options) ([]string, string, error) {
	// Recheck immediately before writing; registry discovery is only a hint.
	i, err := validateInstall(opts.wow)
	if err != nil {
		return nil, "", err
	}
	root := i.root
	opts = pinGameFolder(opts, root)
	addons, err := s.installAddon(opts.endpoint, []string{root})
	if err != nil {
		return nil, "", err
	}
	report := []string{"The World Recorder addon is installed in " + strings.Join(addons, " and ") + "."}
	if err := s.write("game-folder", []byte(root+"\n")); err != nil {
		return report, "", pathless(err)
	}
	exe, err := installSelf(s.dir)
	if err == nil {
		err = register(exe, append([]string{"run"}, opts.args...))
	}
	if err != nil {
		return report, "", fmt.Errorf("The uploader could not be installed: %v", pathless(err))
	}
	report = append(report, "The uploader starts at login and sends your records to Azeroth DB.")
	if s.readKey() != "" {
		return append(report, "This computer is linked to your Azeroth DB account."), exe, nil
	}
	stop, cancel := interrupted()
	defer cancel()
	if err := s.link(stop, opts.endpoint); err != nil {
		return report, exe, fmt.Errorf("This computer is not linked to an Azeroth DB account yet (%v). Open World Recorder again to link it.", err)
	}
	report = append(report, "This computer is now linked to your Azeroth DB account.")
	// Send at once, so the account page that confirmed the link shows the records while it is open.
	if _, err := s.pass(opts.wow, opts.endpoint); err != nil {
		return report, exe, err
	}
	return report, exe, nil
}

func (s *state) installAddon(endpoint string, roots []string) ([]string, error) {
	files, err := s.addonPackage(endpoint)
	if err != nil {
		return nil, err
	}
	defer files.Close()
	var installed []string
	for _, root := range roots {
		clients, err := clientsIn(root)
		if err != nil {
			return nil, err
		}
		for _, i := range clients {
			if err := addonInto(files, i); err != nil {
				return nil, err
			}
			installed = append(installed, filepath.Join(i.client(), "Interface", "AddOns"))
		}
	}
	if len(installed) == 0 {
		return nil, errors.New("WoW Forever was not found on this computer. Install it with the Battle.net app, then open World Recorder again.")
	}
	return installed, nil
}

// addonInto writes the addon package to the client's AddOns folder.
func addonInto(files fs.FS, i install) error {
	return pathless(placeAddon(files, filepath.Join(i.client(), "Interface", "AddOns"), i.interfaces))
}

// placeAddon writes the new addon beside the installed one and swaps the folders, so the game never sees half of it.
// Its TOC is programTOC's.
func placeAddon(files fs.FS, addons, interfaces string) error {
	if err := os.MkdirAll(addons, 0o755); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(addons, ".WorldRecorder-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	if err := os.CopyFS(temp, files); err != nil {
		return err
	}
	toc := filepath.Join(temp, "WorldRecorder", "WorldRecorder.toc")
	data, err := os.ReadFile(toc)
	if err == nil {
		err = os.WriteFile(toc, programTOC(data, interfaces), 0o644)
	}
	if err != nil {
		return err
	}
	old, err := swapIn(filepath.Join(temp, "WorldRecorder"), filepath.Join(addons, "WorldRecorder"))
	if err != nil {
		return err
	}
	return os.RemoveAll(old)
}

// programTOC is the TOC this program installs: its Interface line names the contract's interface numbers for the client,
// so a new client version never marks the addon out of date, and its X-Program line tells the addon that this program
// sends its saves, so the addon does not name the program at login. A copy an addon site installs over it lacks the line,
// and refreshAddons puts this one back.
func programTOC(toc []byte, interfaces string) []byte {
	var b strings.Builder
	for line := range strings.Lines(string(toc)) {
		switch {
		case strings.HasPrefix(line, "## X-Program:"):
			continue
		case strings.HasPrefix(line, "## Interface:"):
			line = "## Interface: " + interfaces + "\n## X-Program: World Recorder\n"
		}
		b.WriteString(line)
	}
	return []byte(b.String())
}

func addonTOC(i install) string {
	return filepath.Join(i.client(), "Interface", "AddOns", "WorldRecorder", "WorldRecorder.toc")
}

// refreshAddons installs the addon package again where the installed TOC differs from the one it would write, as after
// a game update raised the client's interface numbers, and into a client folder it sees for the first time, as when
// WoW Forever leaves beta for a client of its own; an addon the player removed stays removed. A client is remembered
// only once its addon is in place, so an install that failed is tried again at the next refresh.
func (s *state) refreshAddons(endpoint string, found []install) {
	if len(found) == 0 {
		return
	}
	files, err := s.addonPackage(endpoint)
	if err != nil {
		s.log("addon result=failed: %v", err)
		return
	}
	defer files.Close()
	built, err := fs.ReadFile(files, "WorldRecorder/WorldRecorder.toc")
	if err != nil {
		s.log("addon result=failed: %v", err)
		return
	}
	seen, first := s.seenClients()
	handled := slices.Clone(seen)
	for _, i := range found {
		fresh := !first && !slices.Contains(seen, i.client())
		installed, err := os.ReadFile(addonTOC(i))
		if err != nil && !fresh || err == nil && bytes.Equal(installed, programTOC(built, i.interfaces)) {
			handled = remember(handled, i.client())
			continue
		}
		if err := addonInto(files, i); err != nil {
			s.log("addon result=failed: %v", err)
			continue
		}
		handled = remember(handled, i.client())
		s.log("addon client=%s interface=%s result=installed", i.folder, i.interfaces)
	}
	if first || len(handled) > len(seen) {
		if err := s.writeJSON(clientsSeenFile, handled); err != nil {
			s.log("clients seen result=not kept: %v", pathless(err))
		}
	}
}

const clientsSeenFile = "clients-seen.json"

// seenClients is the client folders this program has handled; with no record yet, every client found now counts as
// seen, so a client whose addon the player removed before the record existed stays without it.
func (s *state) seenClients() (seen []string, first bool) {
	data, err := os.ReadFile(s.path(clientsSeenFile))
	if err != nil || json.Unmarshal(data, &seen) != nil {
		return nil, true
	}
	return seen, false
}

func remember(clients []string, client string) []string {
	if slices.Contains(clients, client) {
		return clients
	}
	return append(clients, client)
}

// installSelf copies the running program into dir and returns the executable the login item starts.
func installSelf(dir string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	source, name, inner := bundle(exe)
	target := filepath.Join(dir, name)
	stopOthers(filepath.Join(target, inner))
	if same(source, target) {
		return exe, nil
	}
	return filepath.Join(target, inner), replace(source, target)
}

func same(a, b string) bool {
	first, err := os.Stat(a)
	if err != nil {
		return false
	}
	second, err := os.Stat(b)
	return err == nil && os.SameFile(first, second)
}

// replace never writes into the installed copy: a running uploader keeps its old file.
func replace(source, target string) error {
	fresh := target + ".new"
	if err := errors.Join(os.RemoveAll(fresh), copyTree(source, fresh)); err != nil {
		return err
	}
	old, err := swapIn(fresh, target)
	if err == nil {
		os.RemoveAll(old)
	}
	return err
}

// swapIn renames target aside and fresh into its place, and returns where the previous target is kept.
// When the second rename fails, the previous target is renamed back.
func swapIn(fresh, target string) (string, error) {
	old := target + ".old"
	if err := os.RemoveAll(old); err != nil {
		return "", err
	}
	if err := os.Rename(target, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(fresh, target); err != nil {
		os.Rename(old, target)
		return "", err
	}
	return old, nil
}

// restore puts the copy swapIn kept back in place.
func restore(old, target string) error {
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	return os.Rename(old, target)
}

func copyTree(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.CopyFS(target, os.DirFS(source))
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o755)
}
