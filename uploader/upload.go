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
	version      = "4"
	payloadForm  = 1
	stableWindow = 5 * time.Second
)

type converted struct {
	save
	file fileState
	data []byte
	hash string
}

// invalidSave marks a file that cannot be converted exactly; a later write by the game can fix it.
type invalidSave struct{ error }

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
	ID        string `json:"id"`
	Duplicate bool   `json:"duplicate"`
}

// saveResult is what the last pass found for one save, for the file as it was then.
type saveResult struct {
	save
	file     fileState
	state    string
	uploaded bool
}

func readStable(path string) ([]byte, fileState, error) {
	for {
		before, err := os.Stat(path)
		if err != nil {
			return nil, fileState{}, pathless(err)
		}
		if wait := stableWindow - time.Since(before.ModTime()); wait > 0 {
			time.Sleep(wait)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fileState{}, pathless(err)
		}
		after, err := os.Stat(path)
		if err == nil && after.Size() == before.Size() && after.ModTime().Equal(before.ModTime()) {
			return data, fileState{size: after.Size(), modTime: after.ModTime()}, nil
		}
	}
}

// convert reads a save as JSON, with the combat log facts the last pass read beside the addon's own tables.
func (st *state) convert(s save) (converted, error) {
	source, file, err := readStable(s.path)
	if err != nil {
		return converted{save: s, file: stat(s.path)}, err
	}
	invalid := converted{save: s, file: file}
	globals, err := parseSavedVariables(source)
	if err != nil {
		return invalid, invalidSave{err}
	}
	if builds := st.readCombatLog().observed(); len(builds) > 0 {
		globals[combatLogName] = builds
	}
	data, err := encodeJSON(globals)
	if err != nil {
		return invalid, invalidSave{err}
	}
	sum := sha256.Sum256(data)
	return converted{save: s, file: file, data: data, hash: hex.EncodeToString(sum[:])}, nil
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
	a, err := post(endpoint, key, compressed)
	if err == nil {
		err = s.acknowledge(c.path, key, c.hash)
	}
	s.log("send flavor=%s raw=%d gzip=%d result=%s", c.flavor, len(raw), len(compressed), result(a, err))
	s.recordSend(sendRecord{At: time.Now().UnixMilli(), Save: digest(c.path), Bytes: len(compressed), Result: outcome(a, err)})
	return err
}

func result(a answer, err error) string {
	if err != nil {
		return "failed: " + err.Error()
	}
	return outcome(a, err) + " id=" + a.ID
}

func outcome(a answer, err error) string {
	switch {
	case err != nil:
		return "failed"
	case a.Duplicate:
		return "duplicate"
	}
	return "accepted"
}

func post(endpoint, key string, body []byte) (answer, error) {
	var a answer
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return a, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Set("Authorization", "Bearer "+key)
	response, err := do(request)
	if err != nil {
		return a, err
	}
	defer response.Body.Close()
	reply, err := io.ReadAll(response.Body)
	if err != nil {
		return a, err
	}
	if response.StatusCode != http.StatusOK {
		return a, statusError(response.StatusCode)
	}
	json.Unmarshal(reply, &a)
	return a, nil
}

// pass sends every save that changed since its last accepted send. After a 403 it sends nothing more.
func (s *state) pass(wow, endpoint string) ([]saveResult, error) {
	s.readClients(endpoint)
	key := s.readKey()
	if err := s.readLedger(); err != nil {
		s.log("ledger result=failed: %v", pathless(err))
	}
	found := installs(wow)
	if len(found) == 0 {
		err := folderReason(wow)
		s.log("game folder result=refused: %v", err)
		return nil, err
	}
	s.refreshAddons(endpoint, found)
	s.readCombatLogs(found)
	saves := findSaves(found)
	results := make([]saveResult, 0, len(saves))
	var banned error
	for _, sv := range saves {
		r := saveResult{save: sv, file: stat(sv.path), state: "refused"}
		if banned == nil {
			r, banned = s.passSave(sv, &key, endpoint)
		}
		results = append(results, r)
	}
	counts := map[string]int{}
	for _, r := range results {
		counts[r.state]++
		if r.uploaded {
			counts["uploaded"]++
		}
	}
	if counts["unlinked"] > 0 {
		s.log("not linked: run setup or link to link this computer to a wowdb.wiki account")
	}
	s.log("pass saves=%d sent=%d failed=%d unlinked=%d", len(saves), counts["uploaded"], len(saves)-counts["sent"]-counts["unlinked"], counts["unlinked"])
	return results, banned
}

func (s *state) passSave(sv save, key *string, endpoint string) (saveResult, error) {
	c, err := s.convert(sv)
	r := saveResult{save: sv, file: c.file}
	if err != nil {
		s.log("read flavor=%s result=failed: %v", sv.flavor, err)
		r.state = "failed"
		if errors.As(err, new(invalidSave)) {
			r.state = "invalid"
		}
		return r, nil
	}
	switch {
	case s.sent(sv.path, *key, c.hash):
		r.state = "sent"
	case *key == "":
		r.state = "unlinked"
	default:
		return s.sendSave(c, key, endpoint)
	}
	return r, nil
}

func (s *state) sendSave(c converted, key *string, endpoint string) (saveResult, error) {
	r := saveResult{save: c.save, file: c.file, state: "failed"}
	switch err := s.send(endpoint, *key, c); {
	case err == nil:
		r.state, r.uploaded = "sent", true
	case errors.Is(err, statusError(http.StatusForbidden)):
		s.log("%v", errBanned)
		r.state = "refused"
		return r, errBanned
	case errors.Is(err, statusError(http.StatusUnauthorized)):
		s.log("key result=rejected: this computer must be linked to a wowdb.wiki account again")
		s.dropKey(*key)
		*key = ""
		r.state = "unlinked"
	}
	return r, nil
}

// notSent counts the saves a pass left unsent.
func notSent(results []saveResult) int {
	n := 0
	for _, r := range results {
		if r.state != "sent" {
			n++
		}
	}
	return n
}
