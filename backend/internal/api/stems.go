package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PitchStemEntry describes a pitch-shifted stem available for DAW import.
type PitchStemEntry struct {
	Song  string `json:"song"`
	Pitch string `json:"pitch"`
	Stem  string `json:"stem"`
}

// StemsResponse lists available stems for DAW import.
type StemsResponse struct {
	Output map[string][]string `json:"output"`
	Pitch  []PitchStemEntry    `json:"pitch"`
}

var stemAudioExts = map[string]bool{
	".wav":  true,
	".mp3":  true,
	".flac": true,
	".ogg":  true,
	".m4a":  true,
	".aiff": true,
}

func isAudioStem(name string) bool {
	return stemAudioExts[strings.ToLower(filepath.Ext(name))]
}

// handleListStems returns all audio stems under output/.
// GET /api/daw/stems
func (s *Server) handleListStems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	projectRoot := resolveProjectRoot()
	outputDir := filepath.Join(projectRoot, "output")

	resp := StemsResponse{
		Output: make(map[string][]string),
		Pitch:  []PitchStemEntry{},
	}

	// Walk output/<song>/ directories for .wav stems.
	outputEntries, err := os.ReadDir(outputDir)
	if err == nil {
		for _, entry := range outputEntries {
			if !entry.IsDir() {
				continue
			}
			song := filepath.Base(entry.Name())
			stemDir := filepath.Join(outputDir, song)
			stemEntries, err := os.ReadDir(stemDir)
			if err != nil {
				continue
			}
			var stems []string
			for _, stemEntry := range stemEntries {
				if stemEntry.IsDir() {
					continue
				}
				name := filepath.Base(stemEntry.Name())
				if !isAudioStem(name) {
					continue
				}
				stems = append(stems, name)
			}
			sort.Strings(stems)
			if len(stems) > 0 {
				resp.Output[song] = stems
			}
		}
	}

	// Walk output/<song>/<song>_pitch<pitch>/ directories for pitch-shifted stems.
	if err == nil {
		for _, entry := range outputEntries {
			if !entry.IsDir() {
				continue
			}
			song := filepath.Base(entry.Name())
			songDir := filepath.Join(outputDir, song)
			songEntries, err := os.ReadDir(songDir)
			if err != nil {
				continue
			}
			for _, subEntry := range songEntries {
				if !subEntry.IsDir() {
					continue
				}
				subName := subEntry.Name()
				prefix := song + "_pitch"
				if !strings.HasPrefix(subName, prefix) {
					continue
				}
				pitch := strings.TrimPrefix(subName, prefix)
				if pitch == "" {
					continue
				}
				pitchStemDir := filepath.Join(songDir, subName)
				stemEntries, err := os.ReadDir(pitchStemDir)
				if err != nil {
					continue
				}
				for _, stemEntry := range stemEntries {
					if stemEntry.IsDir() {
						continue
					}
					name := filepath.Base(stemEntry.Name())
					if !isAudioStem(name) {
						continue
					}
					resp.Pitch = append(resp.Pitch, PitchStemEntry{
						Song:  song,
						Pitch: pitch,
						Stem:  name,
					})
				}
			}
		}
	}

	sort.Slice(resp.Pitch, func(i, j int) bool {
		if resp.Pitch[i].Song != resp.Pitch[j].Song {
			return resp.Pitch[i].Song < resp.Pitch[j].Song
		}
		if resp.Pitch[i].Pitch != resp.Pitch[j].Pitch {
			return resp.Pitch[i].Pitch < resp.Pitch[j].Pitch
		}
		return resp.Pitch[i].Stem < resp.Pitch[j].Stem
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
