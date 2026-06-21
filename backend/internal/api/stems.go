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

// StemsResponse lists available stems for DAW import.
type StemsResponse struct {
	Output map[string][]string `json:"output"`
	Pitch  []string            `json:"pitch"`
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

// handleListStems returns all audio stems under output/ and input_rubberband/.
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

	projectRoot := findProjectRoot()
	outputDir := filepath.Join(projectRoot, "output")
	pitchDir := filepath.Join(projectRoot, "input_rubberband")

	resp := StemsResponse{
		Output: make(map[string][]string),
		Pitch:  []string{},
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

	// List .wav files directly under input_rubberband/.
	pitchEntries, err := os.ReadDir(pitchDir)
	if err == nil {
		for _, entry := range pitchEntries {
			if entry.IsDir() {
				continue
			}
			name := filepath.Base(entry.Name())
			if !isAudioStem(name) {
				continue
			}
			resp.Pitch = append(resp.Pitch, name)
		}
		sort.Strings(resp.Pitch)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
