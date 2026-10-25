//go:build darwin

package main

import (
	"debug/macho"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func selectGameFolder(initial, reason string) (string, error) {
	// Arguments carry paths and messages; they are never interpolated into AppleScript.
	script := `on run argv
try
set selectedFolder to choose folder with prompt (item 1 of argv)
return POSIX path of selectedFolder
on error number -128
return ""
end try
end run`
	out, err := exec.Command("osascript", "-e", script, reason+" "+pickerHint()).Output()
	if err != nil {
		return "", fmt.Errorf("The game folder picker could not open: %w", pathless(err))
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", errSelectionCancelled
	}
	return path, nil
}

func validateClientExecutable(client, version string) error {
	paths, _ := filepath.Glob(filepath.Join(client, "*.app", "Contents", "MacOS", "World of Warcraft"))
	for _, path := range paths {
		if f, err := macho.OpenFat(path); err == nil {
			f.Close()
			return nil
		}
		if f, err := macho.Open(path); err == nil {
			f.Close()
			return nil
		}
	}
	return errors.New("The WoW Forever executable is missing. Finish or repair this installation in Battle.net.")
}
