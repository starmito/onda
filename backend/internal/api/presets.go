package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/starmito/onda/internal/cli"
)

const userPresetsFile = "/config/presets_user.json"
const defaultPresetFile = "/config/default_preset.json"

var (
	userPresets      map[string]cli.Preset
	userPresetsMu    sync.RWMutex
	defaultPresetName string
	defaultPresetMu  sync.RWMutex
)

func init() {
	userPresets = make(map[string]cli.Preset)
	loadUserPresets()
	loadDefaultPreset()
}

func loadUserPresets() {
	data, err := os.ReadFile(userPresetsFile)
	if err != nil {
		return
	}
	var presets map[string]cli.Preset
	if err := json.Unmarshal(data, &presets); err != nil {
		return
	}
	userPresets = presets
}

// saveUserPresetsLocked writes user presets to disk.
// Must be called with userPresetsMu already held (write lock).
func saveUserPresetsLocked() error {
	data, err := json.MarshalIndent(userPresets, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal presets: %w", err)
	}
	if err := os.WriteFile(userPresetsFile, data, 0644); err != nil {
		return fmt.Errorf("write presets: %w", err)
	}
	return nil
}

// getAllPresets returns built-in presets + user presets merged.
// User presets with the same name override built-in ones.
func getAllPresets() map[string]cli.Preset {
	result := make(map[string]cli.Preset, len(cli.Presets)+len(userPresets))
	for k, v := range cli.Presets {
		result[k] = v
	}
	userPresetsMu.RLock()
	defer userPresetsMu.RUnlock()
	for k, v := range userPresets {
		result[k] = v
	}
	return result
}

func (s *Server) handleGetPresets(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getAllPresets())
}

func (s *Server) handleSavePreset(w http.ResponseWriter, r *http.Request) {
	var preset cli.Preset
	if err := json.NewDecoder(r.Body).Decode(&preset); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}
	if preset.Name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "preset name is required"})
		return
	}

	userPresetsMu.Lock()
	userPresets[preset.Name] = preset
	err := saveUserPresetsLocked()
	userPresetsMu.Unlock()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	Log("backend", "success", "Preset saved: "+preset.Name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleDeletePreset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "preset name is required"})
		return
	}

	userPresetsMu.Lock()
	delete(userPresets, name)
	err := saveUserPresetsLocked()
	userPresetsMu.Unlock()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	Log("backend", "info", "Preset deleted: "+name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// loadDefaultPreset reads the default preset name from disk.
func loadDefaultPreset() {
	data, err := os.ReadFile(defaultPresetFile)
	if err != nil {
		return
	}
	var entry struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &entry); err != nil {
		return
	}
	defaultPresetMu.Lock()
	defaultPresetName = entry.Name
	defaultPresetMu.Unlock()
}

func (s *Server) handleGetDefaultPreset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	defaultPresetMu.RLock()
	name := defaultPresetName
	defaultPresetMu.RUnlock()
	if name == "" {
		json.NewEncoder(w).Encode(nil)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"name": name})
}

func (s *Server) handleSetDefaultPreset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "name is required"})
		return
	}

	defaultPresetMu.Lock()
	defaultPresetName = body.Name
	defaultPresetMu.Unlock()

	data, err := json.Marshal(map[string]string{"name": body.Name})
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "marshal error"})
		return
	}
	if err := os.WriteFile(defaultPresetFile, data, 0644); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "write error"})
		return
	}

	Log("backend", "success", "Default preset set: "+body.Name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
