package main

import (
	"os"
	"path/filepath"
)

// watchTarget is a folder or file whose changes wake the program; tree also covers every folder below it.
type watchTarget struct {
	path string
	tree bool
}

// watchRoots are the client folders to watch: each contract client's folder in the chosen game folder, valid or not yet,
// or in the default locations.
func watchRoots(wow string) []string {
	roots := defaultInstalls()
	if root, err := gameRoot(wow); err == nil {
		roots = []string{root}
	}
	var clients []string
	for _, root := range roots {
		for _, c := range knownClients() {
			clients = append(clients, filepath.Join(root, c.Flavor))
		}
	}
	return clients
}

// accountFolder is the client's WTF/Account folder or, until the game creates it, the nearest existing folder on its way
// inside the game folder.
func accountFolder(client string) (dir string, complete bool) {
	root, account := filepath.Dir(client), filepath.Join(client, "WTF", "Account")
	for dir = account; ; dir = filepath.Dir(dir) {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, dir == account
		}
		if dir == root {
			return "", false
		}
	}
}

// screenshotFolder is the client's Screenshots folder or, until the game creates it with its first screenshot, the
// client folder, whose new entry wakes the program for the View on website click that created it.
func screenshotFolder(client string) (dir string, complete bool) {
	dir = filepath.Join(client, "Screenshots")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, true
	}
	return client, false
}
