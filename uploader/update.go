package main

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"wowdb.wiki/recorder-uploader/manifest"
)

type asset = manifest.Asset

type release = manifest.Release

// releaseKey is the public half of the key publish.sh signs release.json with; a release it did not sign is refused.
var releaseKey = func() ed25519.PublicKey {
	key, err := base64.StdEncoding.DecodeString("qN9+VCajE7v4M4Mk+uk696sTOCzpFFTJvgnb8KOPFH4=")
	if err != nil || len(key) != ed25519.PublicKeySize {
		panic("release key")
	}
	return key
}()

// pendingUpdate is update.json: the version just installed and where the previous program is kept until
// the new one serves the port.
type pendingUpdate struct {
	Version  string `json:"version"`
	Previous string `json:"previous"`
}

func programAsset(r *release) asset {
	if runtime.GOOS == "windows" {
		return r.Windows
	}
	return r.Darwin
}

// programVersion is the addon's TOC version, which build.sh sets with -X main.programVersion; publish.sh releases the
// program and the addon under it.
var programVersion string

func tocVersion(toc []byte) string {
	for line := range strings.Lines(string(toc)) {
		if version, ok := strings.CutPrefix(strings.TrimSpace(line), "## Version:"); ok {
			return strings.TrimSpace(version)
		}
	}
	return ""
}

// newer compares dotted version numbers.
func newer(candidate, current string) bool {
	a, b := strings.Split(candidate, "."), strings.Split(current, ".")
	for i := range max(len(a), len(b)) {
		x, y := versionPart(a, i), versionPart(b, i)
		if x != y {
			return x > y
		}
	}
	return false
}

func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, _ := strconv.Atoi(parts[i])
	return n
}

func fetchRelease(origin string) (*release, error) {
	response, err := get(origin + "/assets/world-recorder/release.json")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, statusError(response.StatusCode)
	}
	var r release
	if err := json.NewDecoder(response.Body).Decode(&r); err != nil {
		return nil, err
	}
	if !r.Verify(releaseKey) {
		return nil, fmt.Errorf("release.json of version %q is unsigned or its signature does not match; it is refused", r.Version)
	}
	return &r, nil
}

// checkRelease runs on every wake; with automatic updates on, a newer release installs itself unless that
// version was already installed once, so a release that does not raise the running version cannot loop, or its
// download failed since this program started.
func (d *daemon) checkRelease() {
	r, err := fetchRelease(d.origin)
	if err != nil {
		d.s.log("release result=failed: %v", err)
		return
	}
	if !newer(r.Version, programVersion) {
		r = nil
	}
	d.mu.Lock()
	d.update = r
	skip := r != nil && (d.failed == r.Version || d.s.attempted() == r.Version)
	d.mu.Unlock()
	d.broadcast()
	if r != nil && !skip && d.s.readSettings().AutoUpdate {
		d.setBusy("update")
		d.applyUpdate(r)
		d.setBusy("")
	}
}

func (d *daemon) updateNow() error {
	r, err := fetchRelease(d.origin)
	if err != nil || !newer(r.Version, programVersion) {
		return err
	}
	return d.applyUpdate(r)
}

func (s *state) attempted() string {
	data, _ := os.ReadFile(s.path("update-attempt"))
	return strings.TrimSpace(string(data))
}

// applyUpdate swaps the program and restarts; it returns only when that failed, with the previous program back.
func (d *daemon) applyUpdate(r *release) error {
	previous, err := d.replaceProgram(r)
	if err == nil {
		err = d.s.writeJSON("update.json", pendingUpdate{Version: r.Version, Previous: previous})
	}
	if err != nil {
		d.mu.Lock()
		d.failed = r.Version
		d.mu.Unlock()
		d.s.log("update version=%s result=failed: %v", r.Version, pathless(err))
		return err
	}
	d.s.log("update version=%s result=installed", r.Version)
	d.closeListener()
	err = restartProgram(d.exe)
	d.s.log("restart result=failed: %v", err)
	if restoreErr := restore(previous, programPath(d.exe)); restoreErr != nil {
		d.s.log("restore result=failed: %v", pathless(restoreErr))
	} else {
		os.Remove(d.s.path("update.json"))
	}
	d.bind()
	return err
}

// replaceProgram records the version before the swap, so automatic updates never install it twice.
func (d *daemon) replaceProgram(r *release) (string, error) {
	program, err := d.s.download(d.origin, programAsset(r))
	if err != nil {
		return "", err
	}
	defer os.Remove(program)
	if err := d.s.write("update-attempt", []byte(r.Version+"\n")); err != nil {
		return "", err
	}
	return swapProgram(program, d.exe)
}

// programPath is what an update replaces: the app on macOS when the program runs from one, else the program file.
func programPath(exe string) string {
	source, _, _ := bundle(exe)
	return source
}

// finishUpdate runs once the new program serves the port: it installs the addon of its release and deletes the
// previous program. While the previous program is still exiting on Windows the deletion fails; the next start
// tries again.
func (d *daemon) finishUpdate() {
	var u pendingUpdate
	data, err := os.ReadFile(d.s.path("update.json"))
	if err != nil || json.Unmarshal(data, &u) != nil || !strings.HasSuffix(u.Previous, ".old") {
		return
	}
	if u.Version != programVersion {
		d.s.log("update version=%s result=not raised: running %s", u.Version, programVersion)
	}
	if installed := installedAddon(d.gameFolder()); installed == nil || *installed != programVersion {
		if err := d.installAddon(); err != nil {
			d.s.log("addon result=failed: %v", pathless(err))
		}
	}
	if err := os.RemoveAll(u.Previous); err != nil {
		d.s.log("previous program result=kept: %v", pathless(err))
		return
	}
	os.Remove(d.s.path("update.json"))
	d.s.log("update version=%s result=finished", u.Version)
}

// download fetches a file of the release from the site's own origin and keeps it only when its SHA-256 matches.
func (s *state) download(origin string, a asset) (string, error) {
	base, err := url.Parse(origin)
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(a.URL)
	if err != nil || a.URL == "" || a.SHA256 == "" {
		return "", errors.New("the release names no file for this computer")
	}
	address := base.ResolveReference(ref)
	if address.Host != base.Host || address.Scheme != base.Scheme {
		return "", errors.New("the release names a file on another site")
	}
	response, err := get(address.String())
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", statusError(response.StatusCode)
	}
	file, err := os.CreateTemp(s.dir, "download-*")
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	_, err = io.Copy(io.MultiWriter(file, sum), response.Body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil && !strings.EqualFold(hex.EncodeToString(sum.Sum(nil)), a.SHA256) {
		err = fmt.Errorf("%s does not match its SHA-256", path.Base(ref.Path))
	}
	if err != nil {
		os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

func extractZip(archive, dir string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if err := extractFile(f, dir); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, dir string) error {
	if !filepath.IsLocal(f.Name) || f.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("unexpected archive entry %q", f.Name)
	}
	target := filepath.Join(dir, f.Name)
	if f.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	source, err := f.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode().Perm()|0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, source)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}
