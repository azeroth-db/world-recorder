package main

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

const saveName = "WorldRecorder.lua"

type save struct {
	flavor string
	path   string
}

type fileState struct {
	size    int64
	modTime time.Time
}

func installs(wow string) []install {
	if wow == "" {
		wow = savedGameFolder()
	}
	if wow != "" {
		return validInstalls([]string{wow})
	}
	return validInstalls(defaultInstalls())
}

// folderReason says why the chosen game folder, or the default locations, hold no WoW Forever client World Recorder reads.
func folderReason(wow string) error {
	if wow == "" {
		wow = savedGameFolder()
	}
	if wow == "" {
		return errors.New("WoW Forever was not found automatically.")
	}
	_, err := validateInstall(wow)
	return err
}

func findSaves(found []install) []save {
	var saves []save
	for _, i := range found {
		saves = append(saves, flavorSaves(i.root, i.folder)...)
	}
	return saves
}

func flavorSaves(root, flavor string) []save {
	accounts := filepath.Join(root, flavor, "WTF", "Account")
	var saves []save
	for _, account := range subdirectories(accounts) {
		path := filepath.Join(accounts, account, "SavedVariables", saveName)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			saves = append(saves, save{flavor: flavor, path: path})
		}
	}
	return saves
}

func subdirectories(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}

func stat(path string) fileState {
	info, err := os.Stat(path)
	if err != nil {
		return fileState{}
	}
	return fileState{size: info.Size(), modTime: info.ModTime()}
}

func (f fileState) equal(other fileState) bool {
	return f.size == other.size && f.modTime.Equal(other.modTime)
}

func snapshot(saves []save) map[string]fileState {
	states := map[string]fileState{}
	for _, s := range saves {
		states[s.path] = stat(s.path)
	}
	return states
}
