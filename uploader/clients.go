package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// client is one WoW Forever client the site accepts saves from: its Battle.net product code, its folder inside the World
// of Warcraft folder, the version prefixes of the builds World Recorder reads and the interface numbers its addon names.
type gameClient struct {
	Product    string   `json:"product"`
	Flavor     string   `json:"flavor"`
	Builds     []string `json:"builds"`
	Interfaces []int    `json:"interfaces"`
}

// clientContract is the site's /assets/world-recorder/clients.json, read from /api/contrib/clients, which answers past the
// page cache, so a client added on the host reaches the program without a site release; its realms are the site's own
// business.
type clientContract struct {
	Clients []gameClient `json:"clients"`
}

const clientsFile = "clients.json"

var (
	clientFolder  = regexp.MustCompile(`^_[a-z0-9]+(?:_[a-z0-9]+)*_$`)
	clientProduct = regexp.MustCompile(`^wow[a-z0-9_]*$`)
	buildPrefix   = regexp.MustCompile(`^\d+\.\d+\.$`)

	knownMu sync.Mutex
	known   []gameClient
)

var errNoClients = errors.New("World Recorder has not read the WoW Forever versions it supports from Azeroth DB yet. Connect to the internet and open World Recorder again.")

func (c gameClient) reads(version string) bool {
	return slices.ContainsFunc(c.Builds, func(prefix string) bool { return strings.HasPrefix(version, prefix) })
}

// interfaceLine is the TOC value naming every interface number of the client.
func (c gameClient) interfaceLine() string {
	numbers := make([]string, len(c.Interfaces))
	for i, n := range c.Interfaces {
		numbers[i] = strconv.Itoa(n)
	}
	return strings.Join(numbers, ", ")
}

func knownClients() []gameClient {
	knownMu.Lock()
	defer knownMu.Unlock()
	return known
}

// parseClients admits only a contract whose every value is a plain name, so a client folder never leaves the game folder.
func parseClients(data []byte) ([]gameClient, error) {
	var c clientContract
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if len(c.Clients) == 0 {
		return nil, errors.New("the contract lists no client")
	}
	for _, cl := range c.Clients {
		if !clientProduct.MatchString(cl.Product) || !clientFolder.MatchString(cl.Flavor) || len(cl.Builds) == 0 || len(cl.Interfaces) == 0 ||
			slices.ContainsFunc(cl.Builds, func(b string) bool { return !buildPrefix.MatchString(b) }) ||
			slices.ContainsFunc(cl.Interfaces, func(n int) bool { return n <= 0 }) {
			return nil, fmt.Errorf("the contract's client %q is malformed", cl.Product)
		}
	}
	return c.Clients, nil
}

// readClients takes the client contract from the site and keeps a copy; while the site cannot be reached the copy serves.
func (s *state) readClients(endpoint string) {
	clients, err := fetchClients(endpoint)
	if err == nil {
		data, _ := json.Marshal(clientContract{Clients: clients})
		if writeErr := s.write(clientsFile, data); writeErr != nil {
			s.log("clients result=not kept: %v", pathless(writeErr))
		}
	} else {
		data, readErr := os.ReadFile(s.path(clientsFile))
		if readErr == nil {
			clients, readErr = parseClients(data)
		}
		if readErr != nil {
			s.log("clients result=failed: %v", err)
			return
		}
		s.log("clients result=kept copy: %v", err)
	}
	knownMu.Lock()
	known = clients
	knownMu.Unlock()
}

func fetchClients(endpoint string) ([]gameClient, error) {
	base, err := siteBase(endpoint)
	if err != nil {
		return nil, err
	}
	response, err := get(base + "/api/contrib/clients")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, statusError(response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return parseClients(data)
}

// pickerHint names the folders the picker accepts: the World of Warcraft folder or a client folder inside it.
func pickerHint() string {
	var folders []string
	for _, c := range knownClients() {
		if !slices.Contains(folders, c.Flavor) {
			folders = append(folders, c.Flavor)
		}
	}
	if len(folders) == 0 {
		return "Select the World of Warcraft folder."
	}
	return "Select the World of Warcraft folder or its " + strings.Join(folders, " or ") + " folder."
}
