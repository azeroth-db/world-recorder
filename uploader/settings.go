package main

import (
	"bufio"
	"encoding/json"
	"os"
	"slices"
)

// settings is settings.json; start at login is the login item itself.
type settings struct {
	Paused     bool `json:"paused"`
	AutoUpdate bool `json:"autoUpdate"`
}

type settingsPatch struct {
	Paused       *bool `json:"paused"`
	AutoUpdate   *bool `json:"autoUpdate"`
	StartAtLogin *bool `json:"startAtLogin"`
}

// sendRecord is one line of sends.jsonl; save is the SHA-256 of the save's path.
type sendRecord struct {
	At     int64  `json:"at"`
	Save   string `json:"save"`
	Bytes  int    `json:"bytes"`
	Result string `json:"result"`
}

func (s *state) readSettings() settings {
	v := settings{AutoUpdate: true}
	if data, err := os.ReadFile(s.path("settings.json")); err == nil {
		json.Unmarshal(data, &v)
	}
	return v
}

func (s *state) writeSettings(v settings) error {
	data, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	return s.write("settings.json", append(data, '\n'))
}

func (p settingsPatch) apply(v settings) settings {
	for _, field := range []struct {
		value  *bool
		target *bool
	}{{p.Paused, &v.Paused}, {p.AutoUpdate, &v.AutoUpdate}} {
		if field.value != nil {
			*field.target = *field.value
		}
	}
	return v
}

func (s *state) recordSend(r sendRecord) {
	line, err := json.Marshal(r)
	if err == nil {
		var file *os.File
		file, err = os.OpenFile(s.path("sends.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, err = file.Write(append(line, '\n'))
			file.Close()
		}
	}
	if err != nil {
		s.log("history result=failed: %v", pathless(err))
	}
}

// readSends returns every recorded send, newest first.
func (s *state) readSends() []sendRecord {
	file, err := os.Open(s.path("sends.jsonl"))
	if err != nil {
		return nil
	}
	defer file.Close()
	var sends []sendRecord
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		var r sendRecord
		if json.Unmarshal(lines.Bytes(), &r) == nil {
			sends = append(sends, r)
		}
	}
	slices.Reverse(sends)
	return sends
}
