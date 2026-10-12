package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	version      = "3"
	payloadForm  = 1
	stableWindow = 5 * time.Second
)

type converted struct {
	save
	data []byte
	hash string
}

type payload struct {
	Format   int           `json:"format"`
	Install  string        `json:"install"`
	Uploader string        `json:"uploader"`
	Saves    []savePayload `json:"saves"`
}

type savePayload struct {
	Flavor string          `json:"flavor"`
	Data   json.RawMessage `json:"data"`
}

var errBanned = errors.New("the server refuses records from this wowdb.wiki account (status 403); sending stopped")

type statusError int

func (e statusError) Error() string { return fmt.Sprintf("status %d", int(e)) }

type answer struct {
	Accepted bool   `json:"accepted"`
	ID       string `json:"id"`
}

func readStable(path string) ([]byte, error) {
	for {
		before, err := os.Stat(path)
		if err != nil {
			return nil, pathless(err)
		}
		if wait := stableWindow - time.Since(before.ModTime()); wait > 0 {
			time.Sleep(wait)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, pathless(err)
		}
		after, err := os.Stat(path)
		if err == nil && after.Size() == before.Size() && after.ModTime().Equal(before.ModTime()) {
			return data, nil
		}
	}
}

func convert(s save) (converted, error) {
	source, err := readStable(s.path)
	if err != nil {
		return converted{}, err
	}
	globals, err := parseSavedVariables(source)
	if err != nil {
		return converted{}, err
	}
	data, err := encodeJSON(globals)
	if err != nil {
		return converted{}, err
	}
	sum := sha256.Sum256(data)
	return converted{save: s, data: data, hash: hex.EncodeToString(sum[:])}, nil
}

func encodeJSON(value any) ([]byte, error) {
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(value)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), err
}

func compress(data []byte) []byte {
	var b bytes.Buffer
	writer := gzip.NewWriter(&b)
	writer.Write(data)
	writer.Close()
	return b.Bytes()
}

func (s *state) body(c converted) ([]byte, error) {
	return encodeJSON(payload{
		Format:   payloadForm,
		Install:  s.install,
		Uploader: version,
		Saves:    []savePayload{{Flavor: c.flavor, Data: c.data}},
	})
}

func (s *state) send(endpoint, key string, c converted) error {
	raw, err := s.body(c)
	if err != nil {
		return err
	}
	compressed := compress(raw)
	id, err := post(endpoint, key, compressed)
	if err == nil {
		err = s.acknowledge(c.path, key, c.hash)
	}
	s.log("send flavor=%s raw=%d gzip=%d result=%s", c.flavor, len(raw), len(compressed), result(id, err))
	return err
}

func result(id string, err error) string {
	if err != nil {
		return "failed: " + err.Error()
	}
	return "accepted id=" + id
}

func post(endpoint, key string, body []byte) (string, error) {
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Set("Authorization", "Bearer "+key)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	reply, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", statusError(response.StatusCode)
	}
	var a answer
	json.Unmarshal(reply, &a)
	return a.ID, nil
}

func (s *state) pass(wow, endpoint string) (failed int, err error) {
	key := s.readKey()
	if err := s.readLedger(); err != nil {
		s.log("ledger result=failed: %v", pathless(err))
	}
	saves := findSaves(installs(wow))
	sent, unlinked := 0, 0
	for _, sv := range saves {
		c, err := convert(sv)
		if err != nil {
			s.log("read flavor=%s result=failed: %v", sv.flavor, err)
			failed++
			continue
		}
		if s.sent(sv.path, key, c.hash) {
			continue
		}
		if key == "" {
			unlinked++
			continue
		}
		switch err := s.send(endpoint, key, c); {
		case err == nil:
			sent++
		case errors.Is(err, statusError(http.StatusForbidden)):
			s.log("%v", errBanned)
			return failed + 1, errBanned
		case errors.Is(err, statusError(http.StatusUnauthorized)):
			s.log("key result=rejected: this computer must be linked to a wowdb.wiki account again")
			s.dropKey(key)
			key = ""
			unlinked++
		default:
			failed++
		}
	}
	if unlinked > 0 {
		s.log("not linked: run setup or link to link this computer to a wowdb.wiki account")
	}
	s.log("pass saves=%d sent=%d failed=%d unlinked=%d", len(saves), sent, failed, unlinked)
	return failed + unlinked, nil
}
