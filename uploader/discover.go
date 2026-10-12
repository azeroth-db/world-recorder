package main

import (
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

func installs(wow string) []string {
	if wow != "" {
		return []string{wow}
	}
	return defaultInstalls()
}

func findSaves(roots []string) []save {
	var saves []save
	for _, root := range roots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		for _, flavor := range subdirectories(absolute) {
			saves = append(saves, flavorSaves(absolute, flavor)...)
		}
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

func snapshot(saves []save) map[string]fileState {
	states := map[string]fileState{}
	for _, s := range saves {
		if info, err := os.Stat(s.path); err == nil {
			states[s.path] = fileState{size: info.Size(), modTime: info.ModTime()}
		}
	}
	return states
}
