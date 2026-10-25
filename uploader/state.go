package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var installID = regexp.MustCompile(`^[0-9a-f]{32}$`)

type state struct {
	dir     string
	install string
	ledger  ledger
}

// ledger records, for the key it was written under, the hash of each save last accepted.
// Another key starts an empty ledger, so a computer linked again sends its saves to the new account.
type ledger struct {
	Key   string            `json:"key"`
	Saves map[string]string `json:"saves"`
}

func openState() (*state, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	s := &state{dir: filepath.Join(base, "wowdb-recorder")}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	if s.install, err = s.readInstallID(); err != nil {
		return nil, err
	}
	return s, s.readLedger()
}

func (s *state) path(name string) string { return filepath.Join(s.dir, name) }

func (s *state) readInstallID() (string, error) {
	data, err := os.ReadFile(s.path("install-id"))
	if errors.Is(err, fs.ErrNotExist) {
		return s.createInstallID()
	}
	id := strings.TrimSpace(string(data))
	if err == nil && !installID.MatchString(id) {
		err = fmt.Errorf("install-id is not 32 hex digits")
	}
	return id, err
}

func (s *state) createInstallID() (string, error) {
	b := make([]byte, 16)
	rand.Read(b)
	id := hex.EncodeToString(b)
	return id, os.WriteFile(s.path("install-id"), []byte(id+"\n"), 0o600)
}

// readLedger rereads the file before every pass: setup and the login item share it.
func (s *state) readLedger() error {
	s.ledger = ledger{}
	data, err := os.ReadFile(s.path("ledger.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.ledger)
}

func (s *state) sent(path, key, hash string) bool {
	return s.ledger.Key == digest(key) && s.ledger.Saves[digest(path)] == hash
}

// acknowledge merges into the ledger on disk: setup and the login item can both pass right after a link.
func (s *state) acknowledge(path, key, hash string) error {
	if err := s.readLedger(); err != nil {
		s.ledger = ledger{}
	}
	if s.ledger.Key != digest(key) || s.ledger.Saves == nil {
		s.ledger = ledger{Key: digest(key), Saves: map[string]string{}}
	}
	s.ledger.Saves[digest(path)] = hash
	data, err := json.MarshalIndent(s.ledger, "", " ")
	if err != nil {
		return err
	}
	return s.write("ledger.json", append(data, '\n'))
}

// write replaces the file through a temporary file of its own, so two processes never write one temporary path.
func (s *state) write(name string, data []byte) error {
	temp, err := os.CreateTemp(s.dir, name+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	_, err = temp.Write(data)
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(temp.Name(), 0o600)
	}
	if err != nil {
		return err
	}
	return os.Rename(temp.Name(), s.path(name))
}

func (s *state) writeJSON(name string, value any) error {
	data, err := json.MarshalIndent(value, "", " ")
	if err != nil {
		return err
	}
	return s.write(name, append(data, '\n'))
}

func (s *state) readKey() string {
	data, _ := os.ReadFile(s.path("key"))
	return strings.TrimSpace(string(data))
}

func (s *state) storeKey(key string) error { return s.write("key", []byte(key+"\n")) }

func (s *state) dropKey(key string) {
	if s.readKey() == key {
		os.Remove(s.path("key"))
	}
}

func (s *state) log(format string, args ...any) {
	line := time.Now().Format(time.RFC3339) + " " + fmt.Sprintf(format, args...) + "\n"
	fmt.Fprint(os.Stderr, line)
	file, err := os.OpenFile(s.path("uploader.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "log:", pathless(err))
		return
	}
	defer file.Close()
	file.WriteString(line)
}

func digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func pathless(err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return fmt.Errorf("%s: %w", pathErr.Op, pathErr.Err)
	}
	return err
}
