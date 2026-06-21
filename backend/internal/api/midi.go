package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/smf"
)

// MidiNote represents a single MIDI note for the piano roll.
type MidiNote struct {
	Track    int     `json:"track"`
	Channel  uint8   `json:"channel"`
	Key      uint8   `json:"key"`
	Velocity uint8   `json:"velocity"`
	StartMs  float64 `json:"start_ms"`
	EndMs    float64 `json:"end_ms"`
}

// MidiTrack represents a parsed MIDI track.
type MidiTrack struct {
	Index int        `json:"index"`
	Name  string     `json:"name"`
	Notes []MidiNote `json:"notes"`
}

// MidiParseResponse is returned by POST /api/daw/midi/parse.
type MidiParseResponse struct {
	Tracks []MidiTrack `json:"tracks"`
	BPM    float64     `json:"bpm"`
}

// MidiExportRequest is the JSON body for POST /api/daw/midi/export.
type MidiExportRequest struct {
	Tracks []MidiTrack `json:"tracks"`
	BPM    float64     `json:"bpm"`
}

// handleMidiParse parses a .mid file into JSON note data.
// POST /api/daw/midi/parse
func (s *Server) handleMidiParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("method %s not allowed", r.Method)})
		return
	}

	// Look for the file in input/ and daw-data/ (same pattern as trim.go)
	var midiData []byte
	var err error

	var req struct {
		File string `json:"file"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.File != "" {
		safeName := filepath.Base(req.File)
		projectRoot := findProjectRoot()
		midiPath := filepath.Join(projectRoot, "input", safeName)
		if _, statErr := os.Stat(midiPath); os.IsNotExist(statErr) {
			midiPath = filepath.Join(projectRoot, "daw-data", safeName)
			if _, statErr := os.Stat(midiPath); os.IsNotExist(statErr) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": "file not found"})
				return
			}
		}
		midiData, err = os.ReadFile(midiPath)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to read file: " + err.Error()})
			return
		}
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "file is required"})
		return
	}

	smfFile, err := smf.ReadFrom(bytes.NewReader(midiData))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to parse MIDI: " + err.Error()})
		return
	}

	// Get tempo from SMF
	bpm := 120.0
	tc := smfFile.TempoChanges()
	if len(tc) > 0 && tc[0].BPM > 0 {
		bpm = tc[0].BPM
	}

	resp := MidiParseResponse{
		Tracks: make([]MidiTrack, 0),
		BPM:    bpm,
	}

	for trackIdx, track := range smfFile.Tracks {
		mt := MidiTrack{
			Index: trackIdx,
			Name:  fmt.Sprintf("Track %d", trackIdx+1),
			Notes: make([]MidiNote, 0),
		}

		// activeNotes tracks NoteOn events by key+channel
		type activeKey struct {
			channel uint8
			key     uint8
		}
		activeNotes := make(map[activeKey]struct {
			startAbsMicros int64
			startDelta     uint32
			velocity       uint8
		})

		var absTicks int64
		for _, ev := range track {
			absTicks += int64(ev.Delta)
			absMicros := smfFile.TimeAt(absTicks)

			var channel, key, velocity uint8
			if ev.Message.GetNoteOn(&channel, &key, &velocity) && velocity > 0 {
				ak := activeKey{channel, key}
				activeNotes[ak] = struct {
					startAbsMicros int64
					startDelta     uint32
					velocity       uint8
				}{startAbsMicros: absMicros, velocity: velocity}
			} else if ev.Message.GetNoteOff(&channel, &key, &velocity) ||
				(ev.Message.GetNoteOn(&channel, &key, &velocity) && velocity == 0) {
				ak := activeKey{channel, key}
				if start, ok := activeNotes[ak]; ok {
					note := MidiNote{
						Track:    trackIdx,
						Channel:  channel,
						Key:      key,
						Velocity: start.velocity,
						StartMs:  float64(start.startAbsMicros) / 1000.0,
						EndMs:    float64(absMicros) / 1000.0,
					}
					mt.Notes = append(mt.Notes, note)
					delete(activeNotes, ak)
				}
			}
		}

		// Close any unclosed notes (end at last event time)
		for ak, start := range activeNotes {
			mt.Notes = append(mt.Notes, MidiNote{
				Track:    trackIdx,
				Channel:  ak.channel,
				Key:      ak.key,
				Velocity: start.velocity,
				StartMs:  float64(start.startAbsMicros) / 1000.0,
				EndMs:    float64(absTicks) * 60000.0 / (bpm * 960.0),
			})
		}

		if len(mt.Notes) > 0 {
			resp.Tracks = append(resp.Tracks, mt)
		}
	}

	if len(resp.Tracks) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// handleMidiExport exports note JSON data to a .mid file.
// POST /api/daw/midi/export
func (s *Server) handleMidiExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("method %s not allowed", r.Method)})
		return
	}

	var req MidiExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	if len(req.Tracks) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "tracks cannot be empty"})
		return
	}

	if req.BPM <= 0 {
		req.BPM = 120
	}

	// Ticks per quarter note
	const ticksPerBeat = 960

	smfFile := smf.NewSMF1()
	smfFile.TimeFormat = smf.MetricTicks(ticksPerBeat)

	for _, mt := range req.Tracks {
		track := smf.Track{}

		// Group notes by their start time
		// Sort notes by start time first
		for i := 0; i < len(mt.Notes)-1; i++ {
			for j := i + 1; j < len(mt.Notes); j++ {
				if mt.Notes[i].StartMs > mt.Notes[j].StartMs {
					mt.Notes[i], mt.Notes[j] = mt.Notes[j], mt.Notes[i]
				}
			}
		}

		var currentTicks int64
		for _, note := range mt.Notes {
			// Calculate ticks for note start
			startTicks := int64(note.StartMs * req.BPM * float64(ticksPerBeat) / 60000.0)
			deltaTicks := uint32(startTicks - currentTicks)
			if deltaTicks > 0x7FFFFFFF {
				deltaTicks = 0x7FFFFFFF
			}
			currentTicks = startTicks

			if note.Channel > 15 {
				note.Channel = 0
			}
			if note.Key > 127 {
				note.Key = 60
			}
			if note.Velocity > 127 {
				note.Velocity = 100
			}
			if note.Velocity == 0 {
				note.Velocity = 100
			}

			track.Add(uint32(deltaTicks), midi.NoteOn(note.Channel, note.Key, note.Velocity))

			// Calculate NoteOff ticks
			endTicks := int64(note.EndMs * req.BPM * float64(ticksPerBeat) / 60000.0)
			offDelta := uint32(endTicks - currentTicks)
			if offDelta > 0x7FFFFFFF {
				offDelta = 0x7FFFFFFF
			}
			track.Add(offDelta, midi.NoteOff(note.Channel, note.Key))
			currentTicks = endTicks
		}

		track.Close(0)
		smfFile.Add(track)
	}

	midiBytes, err := smfFile.Bytes()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to encode MIDI: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "audio/midi")
	w.Header().Set("Content-Disposition", "attachment; filename=\"export.mid\"")
	w.WriteHeader(http.StatusOK)
	w.Write(midiBytes)
}

// handleMidiDevices lists connected MIDI input/output devices.
// GET /api/daw/midi/devices
func (s *Server) handleMidiDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("method %s not allowed", r.Method)})
		return
	}

	type deviceInfo struct {
		Name     string `json:"name"`
		Port     int    `json:"port"`
		IsOutput bool   `json:"is_output"`
	}

	var devices []deviceInfo

	// Try to list MIDI devices; if drivers fail, return empty list
	func() {
		defer func() {
			recover()
		}()

		inPorts := midi.GetInPorts()
		for i, p := range inPorts {
			devices = append(devices, deviceInfo{
				Name:     p.String(),
				Port:     i,
				IsOutput: false,
			})
		}

		outPorts := midi.GetOutPorts()
		for i, p := range outPorts {
			devices = append(devices, deviceInfo{
				Name:     p.String(),
				Port:     i,
				IsOutput: true,
			})
		}
	}()

	if devices == nil {
		devices = []deviceInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(devices)
}
