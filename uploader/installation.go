package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var errSelectionCancelled = errors.New("installation cancelled")

// install is a validated WoW Forever client: the World of Warcraft folder, the client's folder inside it, its build and
// the interface numbers the contract gives its addon.
type install struct {
	root       string
	folder     string
	version    string
	interfaces string
}

func (i install) client() string { return filepath.Join(i.root, i.folder) }

// gameRoot is the World of Warcraft folder of a chosen path: the path itself, or the folder above a client folder.
func gameRoot(path string) (string, error) {
	path = strings.Trim(strings.TrimSpace(path), `"`)
	if path == "" {
		return "", errors.New("No game folder was selected.")
	}
	root, err := filepath.Abs(path)
	if err != nil {
		return "", pathless(err)
	}
	if clientFolder.MatchString(filepath.Base(root)) {
		root = filepath.Dir(root)
	}
	return root, nil
}

// validateInstall accepts the folder when it holds a client World Recorder reads, and returns the first; see clientsIn.
func validateInstall(path string) (install, error) {
	found, err := clientsIn(path)
	if err != nil {
		return install{}, err
	}
	return found[0], nil
}

// clientsIn lists every client of the site's contract that Battle.net lists as active in the folder at a version the
// contract names, with its own folder identity and executable, so a beta and its launch client installed side by side
// are both read. Product codes are reused across betas, so the code alone does not identify Forever.
func clientsIn(path string) ([]install, error) {
	root, err := gameRoot(path)
	if err != nil {
		return nil, err
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		return nil, fmt.Errorf("The selected game folder is unavailable: %w", pathless(err))
	}
	active, err := activeBuilds(filepath.Join(root, ".build.info"))
	if err != nil {
		return nil, err
	}
	clients := knownClients()
	if len(clients) == 0 {
		return nil, errNoClients
	}
	var found []install
	var broken error
	for _, c := range clients {
		version, ok := active[c.Product]
		if !ok || !c.reads(version) || flavorProduct(filepath.Join(root, c.Flavor)) != c.Product ||
			slices.ContainsFunc(found, func(i install) bool { return i.folder == c.Flavor }) {
			continue
		}
		i := install{root: root, folder: c.Flavor, version: version, interfaces: c.interfaceLine()}
		if err := validateClientExecutable(i.client(), version); err != nil {
			broken = err
			continue
		}
		found = append(found, i)
	}
	if len(found) > 0 {
		return found, nil
	}
	if broken != nil {
		return nil, broken
	}
	return nil, unknownClient(root, active)
}

// seenClient is a World of Warcraft client the folder holds that the contract does not list.
type seenClient struct {
	Version string `json:"version"`
	Folder  string `json:"folder"`
}

// unknownClients names those clients, so the player learns which game World Recorder saw; the status carries them as
// data for the window and the site to say in the player's language.
type unknownClients []seenClient

func (u unknownClients) Error() string {
	named := make([]string, len(u))
	for i, c := range u {
		named[i] = fmt.Sprintf("World of Warcraft %s in its %s folder", c.Version, c.Folder)
	}
	return fmt.Sprintf("This folder holds %s, which World Recorder does not read yet.", strings.Join(named, " and "))
}

func unknownClient(root string, active map[string]string) error {
	var seen unknownClients
	for _, folder := range subdirectories(root) {
		product := flavorProduct(filepath.Join(root, folder))
		if version, ok := active[product]; ok && clientFolder.MatchString(folder) {
			seen = append(seen, seenClient{Version: version, Folder: folder})
		}
	}
	if len(seen) == 0 {
		return errors.New("WoW Forever is not installed in the selected folder. Choose its Battle.net installation folder.")
	}
	return seen
}

// flavorProduct is the product a client folder's .flavor.info names, or "" when it names none.
func flavorProduct(client string) string {
	data, err := os.ReadFile(filepath.Join(client, ".flavor.info"))
	lines := strings.SplitN(strings.TrimPrefix(string(data), "\ufeff"), "\n", 2)
	if err != nil || len(lines) != 2 || strings.TrimSpace(lines[0]) != "Product Flavor!STRING:0" {
		return ""
	}
	return strings.TrimSpace(lines[1])
}

// activeBuilds maps each product .build.info lists as active to its version.
func activeBuilds(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("The selected folder has no readable Battle.net installation metadata (.build.info).")
	}
	defer file.Close()
	r := csv.NewReader(file)
	r.Comma = '|'
	header, err := r.Read()
	if err != nil {
		return nil, errors.New("Battle.net installation metadata could not be read.")
	}
	for i, h := range header {
		header[i], _, _ = strings.Cut(strings.TrimPrefix(h, "\ufeff"), "!")
	}
	product, active, build := slices.Index(header, "Product"), slices.Index(header, "Active"), slices.Index(header, "Version")
	if product < 0 || active < 0 || build < 0 {
		return nil, errors.New("Battle.net installation metadata is missing product, active or version information.")
	}
	builds := map[string]string{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			return builds, nil
		}
		if err != nil {
			return nil, errors.New("Battle.net installation metadata is incomplete. Finish or repair the game installation in Battle.net.")
		}
		if row[active] == "1" {
			builds[row[product]] = row[build]
		}
	}
}

func validInstalls(candidates []string) []install {
	var found []install
	for _, candidate := range candidates {
		clients, _ := clientsIn(candidate)
		for _, i := range clients {
			if !slices.ContainsFunc(found, func(other install) bool { return same(i.client(), other.client()) }) {
				found = append(found, i)
			}
		}
	}
	return found
}

// Resolve before account validation, addon replacement or login registration.
func chooseInstall(explicit, saved string) (string, error) {
	var initial, reason string
	if explicit != "" || saved != "" {
		initial = explicit
		if initial == "" {
			initial = saved
		}
		if i, err := validateInstall(initial); err == nil {
			return i.root, nil
		} else {
			reason = err.Error()
		}
	} else {
		found := validInstalls(defaultInstalls())
		if len(found) == 1 {
			return found[0].root, nil
		}
		reason = "WoW Forever was not found automatically."
		if len(found) > 1 {
			initial = found[0].root
			reason = "More than one WoW Forever installation was found. Choose the one you play."
		}
	}
	return pickInstall(initial, reason)
}

// pickInstall opens the folder picker until the player selects a valid installation or cancels.
func pickInstall(initial, reason string) (string, error) {
	for {
		selected, err := selectGameFolder(initial, reason)
		if err != nil {
			return "", err
		}
		i, err := validateInstall(selected)
		if err == nil {
			return i.root, nil
		}
		initial, reason = selected, err.Error()
	}
}

func savedGameFolder() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	data, _ := os.ReadFile(filepath.Join(base, "wowdb-recorder", "game-folder"))
	return strings.TrimSpace(string(data))
}

func pinGameFolder(opts options, root string) options {
	opts.wow = root
	var args []string
	for _, arg := range opts.args {
		if !strings.HasPrefix(arg, "--wow=") {
			args = append(args, arg)
		}
	}
	opts.args = append(args, "--wow="+root)
	return opts
}
