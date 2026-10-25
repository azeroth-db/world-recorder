package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The game's combat log file (Logs/WoWCombatLog*.txt, written while the addon or /combatlog turns logging on) keeps
// what the client withholds from addons: a creature's GUID, health, level and casts. Only creatures the game
// controls are read; every player, pet and guardian is skipped and nothing of theirs leaves the computer.

const (
	combatLogName = "WorldRecorderCombatLog"
	// Unit flags (COMBATLOG_OBJECT_*): an outsider the game controls, of creature type.
	flagOutsider = 0x8
	flagNPC      = 0x200
	flagTypeNPC  = 0x800
	baseFields   = 8
)

type creatureFacts struct {
	Levels map[string]int64 `json:"levels"`
	Maps   map[string]bool  `json:"maps"`
	Spells map[string]int64 `json:"spells"`
}

type encounterFacts struct {
	Map       int64           `json:"map"`
	Starts    int64           `json:"starts"`
	Kills     int64           `json:"kills"`
	Creatures map[string]bool `json:"creatures"`
}

type buildFacts struct {
	Creatures  map[string]*creatureFacts  `json:"creatures"`
	Encounters map[string]*encounterFacts `json:"encounters"`
}

// logFile is how far a file was read and the context its next line continues.
type logFile struct {
	Offset    int64  `json:"offset"`
	Advanced  int    `json:"advanced"`
	Encounter string `json:"encounter"`
}

type combatLog struct {
	Files  map[string]*logFile    `json:"files"`
	Builds map[string]*buildFacts `json:"builds"`
}

func (s *state) readCombatLog() combatLog {
	log := combatLog{}
	if data, err := os.ReadFile(s.path("combatlog.json")); err == nil {
		json.Unmarshal(data, &log)
	}
	if log.Files == nil {
		log.Files = map[string]*logFile{}
	}
	if log.Builds == nil {
		log.Builds = map[string]*buildFacts{}
	}
	return log
}

// observed is every build whose logs gave a creature or an encounter: what an upload carries.
func (log combatLog) observed() map[string]*buildFacts {
	builds := map[string]*buildFacts{}
	for build, facts := range log.Builds {
		if len(facts.Creatures) > 0 || len(facts.Encounters) > 0 {
			builds[build] = facts
		}
	}
	return builds
}

func (s *state) writeCombatLog(log combatLog) error {
	data, err := json.Marshal(log)
	if err != nil {
		return err
	}
	pending := s.path("combatlog.json.next")
	if err := os.WriteFile(pending, data, 0o600); err != nil {
		return err
	}
	return os.Rename(pending, s.path("combatlog.json"))
}

// readCombatLogs reads the lines each install's log files gained since the last pass, under the install's build.
func (s *state) readCombatLogs(found []install) combatLog {
	log := s.readCombatLog()
	for _, i := range found {
		root, build := i.root, i.version[strings.LastIndex(i.version, ".")+1:]
		files, _ := filepath.Glob(filepath.Join(i.client(), "Logs", "WoWCombatLog*.txt"))
		facts := log.Builds[build]
		if facts == nil && len(files) > 0 {
			facts = &buildFacts{Creatures: map[string]*creatureFacts{}, Encounters: map[string]*encounterFacts{}}
			log.Builds[build] = facts
		}
		for _, path := range files {
			key := filepath.Join(root, filepath.Base(path))
			file := log.Files[key]
			if file == nil {
				file = &logFile{}
				log.Files[key] = file
			}
			if err := readLogFile(path, file, facts); err != nil {
				s.log("combat log result=failed: %v", pathless(err))
			}
		}
	}
	if err := s.writeCombatLog(log); err != nil {
		s.log("combat log result=failed: %v", pathless(err))
	}
	return log
}

func readLogFile(path string, file *logFile, facts *buildFacts) error {
	stream, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer stream.Close()
	info, err := stream.Stat()
	if err != nil {
		return err
	}
	if info.Size() < file.Offset {
		*file = logFile{}
	}
	if _, err := stream.Seek(file.Offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(stream, 1<<20)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			// A line the game has not finished writing is read on the next pass.
			if err == io.EOF {
				return nil
			}
			return err
		}
		file.Offset += int64(len(line))
		readLine(strings.TrimRight(line, "\r\n"), file, facts)
	}
}

// logFields splits one event at its commas; a quoted name keeps its commas.
func logFields(text string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c == '"':
			quoted = !quoted
		case c == ',' && !quoted:
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	return append(out, b.String())
}

func number(text string) (int64, bool) {
	value, err := strconv.ParseInt(text, 0, 64)
	return value, err == nil
}

// creature is the game id of a creature GUID (Creature-0-server-map-zone-id-spawn) and the map it names.
func creature(guid string, flags string) (id, mapID string, ok bool) {
	parts := strings.Split(guid, "-")
	unit, valid := number(flags)
	if len(parts) != 7 || (parts[0] != "Creature" && parts[0] != "Vehicle") || !valid ||
		unit&flagOutsider == 0 || unit&flagNPC == 0 || unit&flagTypeNPC == 0 {
		return "", "", false
	}
	if _, numeric := number(parts[5]); !numeric {
		return "", "", false
	}
	return parts[5], parts[3], true
}

func (f *buildFacts) creature(id string) *creatureFacts {
	row := f.Creatures[id]
	if row == nil {
		row = &creatureFacts{Levels: map[string]int64{}, Maps: map[string]bool{}, Spells: map[string]int64{}}
		f.Creatures[id] = row
	}
	return row
}

// prefix is how many fields an event's prefix adds after the base fields, before its advanced block.
func prefix(event string) (int, bool) {
	switch {
	case strings.HasPrefix(event, "SWING_"):
		return 0, true
	case strings.HasPrefix(event, "SPELL_"), strings.HasPrefix(event, "RANGE_"):
		return 3, true
	}
	return 0, false
}

func readLine(line string, file *logFile, facts *buildFacts) {
	// "<date> <time>  EVENT,...": two spaces end the timestamp.
	at := strings.Index(line, "  ")
	if at < 0 {
		return
	}
	row := logFields(line[at+2:])
	event, args := row[0], row[1:]
	switch event {
	case "ENCOUNTER_START":
		encounterStart(args, file, facts)
		return
	case "ENCOUNTER_END":
		encounterEnd(args, file, facts)
		return
	}
	size, known := prefix(event)
	if !known || len(args) < baseFields+size {
		return
	}
	units := [][2]string{{args[0], args[2]}, {args[4], args[6]}}
	for side, unit := range units {
		id, mapID, ok := creature(unit[0], unit[1])
		if !ok {
			continue
		}
		row := facts.creature(id)
		row.Maps[mapID] = true
		if encounter := facts.Encounters[file.Encounter]; encounter != nil {
			encounter.Creatures[id] = true
		}
		if side == 0 && size == 3 {
			spell := args[baseFields]
			if _, ok := number(spell); ok {
				if event == "SPELL_CAST_SUCCESS" {
					row.Spells[spell]++
				} else if _, seen := row.Spells[spell]; !seen {
					row.Spells[spell] = 0
				}
			}
		}
	}
	advanced(event, args, size, units, file, facts)
}

// advanced reads the unit facts advanced combat logging appends after the prefix: infoGUID, ownerGUID, currentHP,
// maxHP, ..., level last. The block ends SPELL_CAST_SUCCESS, which sets how many fields it has in this file.
func advanced(event string, args []string, size int, units [][2]string, file *logFile, facts *buildFacts) {
	start := baseFields + size
	if event == "SPELL_CAST_SUCCESS" && len(args)-start >= 16 && len(args)-start <= 17 {
		file.Advanced = len(args) - start
	}
	if file.Advanced == 0 || len(args) < start+file.Advanced {
		return
	}
	block := args[start : start+file.Advanced]
	for _, unit := range units {
		if unit[0] != block[0] {
			continue
		}
		id, _, ok := creature(unit[0], unit[1])
		current, okCurrent := number(block[2])
		health, okHealth := number(block[3])
		level, okLevel := number(block[file.Advanced-1])
		if !ok || !okCurrent || !okHealth || !okLevel || health <= 0 || current > health || level <= 0 {
			return
		}
		row, key := facts.creature(id), strconv.FormatInt(level, 10)
		if health > row.Levels[key] {
			row.Levels[key] = health
		}
		return
	}
}

// ENCOUNTER_START,encounterID,"name",difficultyID,groupSize,instanceID
func encounterStart(args []string, file *logFile, facts *buildFacts) {
	if len(args) < 5 {
		return
	}
	if _, ok := number(args[0]); !ok {
		return
	}
	row := facts.Encounters[args[0]]
	if row == nil {
		row = &encounterFacts{Creatures: map[string]bool{}}
		facts.Encounters[args[0]] = row
	}
	if mapID, ok := number(args[4]); ok {
		row.Map = mapID
	}
	row.Starts++
	file.Encounter = args[0]
}

// ENCOUNTER_END,encounterID,"name",difficultyID,groupSize,success
func encounterEnd(args []string, file *logFile, facts *buildFacts) {
	if len(args) >= 5 && facts.Encounters[args[0]] != nil && args[4] == "1" {
		facts.Encounters[args[0]].Kills++
	}
	file.Encounter = ""
}
