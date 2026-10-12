package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const foreverFlavor = "_classic_beta_"

// build.sh copies ../WorldRecorder here before building; go:embed cannot reach a parent directory.
//
//go:embed addon
var addonFiles embed.FS

func (s *state) setup(opts options, dialog bool) error {
	report, err := s.installAll(opts)
	if !dialog {
		fmt.Println(strings.Join(report, "\n"))
		return err
	}
	if err != nil {
		report = append(report, err.Error())
	}
	showDialog(strings.Join(report, "\n\n"), err != nil)
	return err
}

func (s *state) installAll(opts options) ([]string, error) {
	addons, err := installAddon(installs(opts.wow))
	if err != nil {
		return nil, err
	}
	report := []string{"The World Recorder addon is installed in " + strings.Join(addons, " and ") + "."}
	exe, err := installSelf(s.dir)
	if err == nil {
		err = register(exe, append([]string{"run"}, opts.args...))
	}
	if err != nil {
		return report, fmt.Errorf("The uploader could not be installed: %v", pathless(err))
	}
	report = append(report, "The uploader starts at login and sends your records to wowdb.wiki.")
	if s.readKey() != "" {
		return append(report, "This computer is linked to your wowdb.wiki account."), nil
	}
	if err := s.link(opts.endpoint); err != nil {
		return report, fmt.Errorf("This computer is not linked to a wowdb.wiki account yet (%v). Open World Recorder again to link it.", err)
	}
	report = append(report, "This computer is now linked to your wowdb.wiki account.")
	// Send at once, so the account page that confirmed the link shows the records while it is open.
	if _, err := s.pass(opts.wow, opts.endpoint); err != nil {
		return report, err
	}
	return report, nil
}

func installAddon(roots []string) ([]string, error) {
	files, err := fs.Sub(addonFiles, "addon")
	if err != nil {
		return nil, err
	}
	var installed []string
	for _, root := range roots {
		if info, err := os.Stat(filepath.Join(root, foreverFlavor)); err != nil || !info.IsDir() {
			continue
		}
		addons := filepath.Join(root, foreverFlavor, "Interface", "AddOns")
		if err := os.RemoveAll(filepath.Join(addons, "WorldRecorder")); err != nil {
			return nil, pathless(err)
		}
		if err := os.MkdirAll(addons, 0o755); err != nil {
			return nil, pathless(err)
		}
		if err := os.CopyFS(addons, files); err != nil {
			return nil, pathless(err)
		}
		installed = append(installed, addons)
	}
	if len(installed) == 0 {
		return nil, fmt.Errorf("No WoW Forever client (%s) was found in %s.", foreverFlavor, strings.Join(roots, ", "))
	}
	return installed, nil
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
	fresh, old := target+".new", target+".old"
	if err := errors.Join(os.RemoveAll(fresh), os.RemoveAll(old), copyTree(source, fresh)); err != nil {
		return err
	}
	if err := os.Rename(target, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(fresh, target); err != nil {
		return err
	}
	os.RemoveAll(old)
	return nil
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
