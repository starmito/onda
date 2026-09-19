package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// exportProfilesFile returns the runtime path for the persisted audio export
// profiles file. It is resolved under the configured data root so the file is
// stored in [RAIZ]/config/ rather than being hardcoded to /config/.
func exportProfilesFile() string {
	return filepath.Join(mustSub("config"), "audio_export_profiles.json")
}

// AudioExportProfiles holds persisted audio export configuration.
type AudioExportProfiles struct {
	DefaultFormat string                    `json:"defaultFormat"`
	NameTemplate  string                    `json:"nameTemplate,omitempty"`
	Formats       map[string]*FormatProfile `json:"formats"`
}

// FormatProfile describes the settings for a single export format.
type FormatProfile struct {
	BitDepth    string `json:"bitDepth,omitempty"`
	SampleRate  string `json:"sampleRate,omitempty"`
	Compression int    `json:"compression,omitempty"`
	Bitrate     string `json:"bitrate,omitempty"`
	Mode        string `json:"mode,omitempty"`
}

var (
	exportProfiles   AudioExportProfiles = defaultExportProfiles()
	exportProfilesMu sync.RWMutex
)

func defaultExportProfiles() AudioExportProfiles {
	return AudioExportProfiles{
		DefaultFormat: "flac",
		NameTemplate:  "{song} ({pitches}) ({suffix})",
		Formats: map[string]*FormatProfile{
			"wav": {BitDepth: "32f", SampleRate: "source"},
			"flac": {Compression: 5, BitDepth: "24"},
			"mp3": {Bitrate: "320k", Mode: "cbr"},
		},
	}
}

func loadExportProfiles() error {
	return loadExportProfilesAt(exportProfilesFile(), &exportProfiles)
}

func loadExportProfilesAt(path string, dest *AudioExportProfiles) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			*dest = defaultExportProfiles()
			return nil
		}
		return fmt.Errorf("read export profiles: %w", err)
	}

	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("unmarshal export profiles: %w", err)
	}
	return nil
}

func saveExportProfiles(settings *AudioExportProfiles) error {
	return saveExportProfilesAt(exportProfilesFile(), settings)
}

func saveExportProfilesAt(path string, settings *AudioExportProfiles) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal export profiles: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write export profiles: %w", err)
	}
	return nil
}

func (s *Server) handleGetExportProfiles(w http.ResponseWriter, r *http.Request) {
	exportProfilesMu.RLock()
	profiles := exportProfiles
	exportProfilesMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profiles)
}

func (s *Server) handleSaveExportProfiles(w http.ResponseWriter, r *http.Request) {
	var newProfiles AudioExportProfiles
	if err := json.NewDecoder(r.Body).Decode(&newProfiles); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	if err := saveExportProfiles(&newProfiles); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	exportProfilesMu.Lock()
	exportProfiles = newProfiles
	exportProfilesMu.Unlock()

	Log("backend", "success", "Export profiles saved")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
