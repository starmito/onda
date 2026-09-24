package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/starmito/onda/internal/cli"
)

func userPresetsFile() string {
	return filepath.Join(mustConfigDir(), "presets_user.json")
}

func defaultPresetFile() string {
	return filepath.Join(mustConfigDir(), "default_preset.json")
}

func deletedPresetsFile() string {
	return filepath.Join(mustConfigDir(), "presets_deleted.json")
}

var (
	userPresets       map[string]cli.Preset
	userPresetsMu     sync.RWMutex
	defaultPresetName string
	defaultPresetMu   sync.RWMutex
	deletedPresets    map[string]struct{}
	deletedPresetsMu  sync.RWMutex
)

func init() {
	userPresets = make(map[string]cli.Preset)
	deletedPresets = make(map[string]struct{})

	loadDeletedPresets()

	// Seed 4 built-in presets with Locked=true — only if they don't exist in user presets
	// and they have not been deleted by the user.
	seedPresets()

	loadUserPresets()
	loadDefaultPreset()
}

// seedPresets inserts the 4 default presets into the Presets map.
// If a user preset with the same name exists on disk, the user version takes precedence
// (loaded later in loadUserPresets which puts into userPresets, and getAllPresets lets user presets override).
// Presets listed in presets_deleted.json are skipped so users can permanently remove built-ins.
func seedPresets() {
	factory := map[string]cli.Preset{
		"Voces Total": {
			Name:        "Voces Total",
			Description: "1 paso: Vocal separa voces → instrumental guardado, voces enviadas a resultado",
			Pitch:       0,
			Locked:      true,
			Steps: []cli.PipelineStep{
				{
					ID:      "vocal",
					Model:   "BS_Roformer_Viperx",
					Type:    "vocal",
					Enabled: true,
					Stems: map[string]cli.StemRoute{
						"vocals":       {Action: cli.StemSave, Target: "result"},
						"instrumental": {Action: cli.StemSave, Target: "result"},
					},
				},
			},
		},
		"Eliminador de Voz": {
			Name:        "Eliminador de Voz",
			Description: "1 paso: Vocal elimina voces, solo instrumental guardado",
			Pitch:       0,
			Locked:      true,
			Steps: []cli.PipelineStep{
				{
					ID:      "vocal",
					Model:   "BS_Roformer_Viperx",
					Type:    "vocal",
					Enabled: true,
					Stems: map[string]cli.StemRoute{
						"vocals":       {Action: cli.StemDiscard},
						"instrumental": {Action: cli.StemSave, Target: "result"},
					},
				},
			},
		},
		"Separador Completo": {
			Name:        "Separador Completo",
			Description: "2 pasos: Vocal separa voces → Demucs separa el instrumental en drums, bass, other",
			Pitch:       0,
			Locked:      true,
			Steps: []cli.PipelineStep{
				{
					ID:      "vocal",
					Model:   "BS_Roformer_Viperx",
					Type:    "vocal",
					Enabled: true,
					Stems: map[string]cli.StemRoute{
						"vocals":       {Action: cli.StemSave, Target: "result"},
						"instrumental": {Action: cli.ActionRoute, Target: "step:demucs"},
					},
				},
				{
					ID:      "demucs",
					Model:   "htdemucs_ft",
					Type:    "demucs",
					Enabled: true,
					Stems: map[string]cli.StemRoute{
						"drums":  {Action: cli.StemSave, Target: "result"},
						"bass":   {Action: cli.StemSave, Target: "result"},
						"other":  {Action: cli.StemSave, Target: "result"},
						"vocals": {Action: cli.StemDiscard},
					},
				},
			},
		},
		"Solo Instrumentos": {
			Name:        "Solo Instrumentos",
			Description: "1 paso: Demucs separa stems, descarta voces",
			Pitch:       0,
			Locked:      true,
			Steps: []cli.PipelineStep{
				{
					ID:      "demucs",
					Model:   "htdemucs_ft",
					Type:    "demucs",
					Enabled: true,
					Stems: map[string]cli.StemRoute{
						"drums":  {Action: cli.StemSave, Target: "result"},
						"bass":   {Action: cli.StemSave, Target: "result"},
						"other":  {Action: cli.StemSave, Target: "result"},
						"vocals": {Action: cli.StemDiscard},
					},
				},
			},
		},
	}

	for name, preset := range factory {
		if !isPresetDeleted(name) {
			cli.Presets[name] = preset
		}
	}
}

func loadUserPresets() {
	userPresetsMu.Lock()
	defer userPresetsMu.Unlock()
	userPresets = make(map[string]cli.Preset)
	loadUserPresetsLocked()
}

// loadUserPresetsLocked reads user presets from disk into the already-reset
// userPresets map. The caller must hold userPresetsMu (write lock).
func loadUserPresetsLocked() {
	data, err := readConfigFile(userPresetsFile())
	if err != nil {
		return
	}
	var rawPresets map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawPresets); err != nil {
		return
	}
	for name, raw := range rawPresets {
		preset := migratePreset(raw)
		if preset.Name == "" {
			preset.Name = name
		}
		userPresets[name] = preset
	}
}

// migratePreset attempts to parse a preset from disk.
// Supports both the new format (with Steps) and the old format (with ViperxEnabled, etc.).
func migratePreset(data json.RawMessage) cli.Preset {
	// Try new format first
	var newPreset cli.Preset
	if err := json.Unmarshal(data, &newPreset); err == nil && len(newPreset.Steps) > 0 {
		return newPreset
	}

	// Try old format and migrate. The legacy JSON uses viperxEnabled/viperxStems;
	// we read those tags but convert the step to a plain "vocal" step.
	var oldPreset struct {
		Name          string   `json:"name"`
		VocalEnabled  bool     `json:"viperxEnabled"`
		DemucsEnabled bool     `json:"demucsEnabled"`
		VocalModel    string   `json:"vocalModel"`
		VocalOverlap  int      `json:"vocalOverlap"`
		StemModel     string   `json:"stemModel"`
		DrumsModel    string   `json:"drumsModel"`
		BassModel     string   `json:"bassModel"`
		OtherModel    string   `json:"otherModel"`
		VocalStems    []string `json:"viperxStems"`
		DemucsStems   []string `json:"demucsStems"`
		Pitch         int      `json:"pitch"`
		Description   string   `json:"description"`
	}
	if err := json.Unmarshal(data, &oldPreset); err != nil {
		// Not valid old format either — return empty
		return cli.Preset{}
	}

	// Build new format from old
	migrated := cli.Preset{
		Name:        oldPreset.Name,
		Pitch:       oldPreset.Pitch,
		Description: oldPreset.Description,
	}

	// Vocal step
	if oldPreset.VocalEnabled {
		vocalModel := oldPreset.VocalModel
		if vocalModel == "" {
			vocalModel = "BS_Roformer_Viperx"
		}
		step := cli.PipelineStep{
			ID:      "vocal",
			Model:   vocalModel,
			Type:    "vocal",
			Enabled: true,
			Stems:   make(map[string]cli.StemRoute),
		}
		if len(oldPreset.VocalStems) > 0 {
			for _, s := range oldPreset.VocalStems {
				step.Stems[s] = cli.StemRoute{Action: cli.StemSave, Target: "result"}
			}
		} else {
			step.Stems["vocals"] = cli.StemRoute{Action: cli.StemSave, Target: "result"}
			step.Stems["instrumental"] = cli.StemRoute{Action: cli.StemSave, Target: "result"}
		}
		migrated.Steps = append(migrated.Steps, step)
	}

	// Demucs step
	if oldPreset.DemucsEnabled {
		stemModel := oldPreset.StemModel
		if stemModel == "" {
			stemModel = "htdemucs_ft"
		}
		step := cli.PipelineStep{
			ID:      "demucs",
			Model:   stemModel,
			Type:    "demucs",
			Enabled: true,
			Stems:   make(map[string]cli.StemRoute),
		}
		if len(oldPreset.DemucsStems) > 0 {
			for _, s := range oldPreset.DemucsStems {
				step.Stems[s] = cli.StemRoute{Action: cli.StemSave, Target: "result"}
			}
		} else {
			step.Stems["drums"] = cli.StemRoute{Action: cli.StemSave, Target: "result"}
			step.Stems["bass"] = cli.StemRoute{Action: cli.StemSave, Target: "result"}
			step.Stems["other"] = cli.StemRoute{Action: cli.StemSave, Target: "result"}
			step.Stems["vocals"] = cli.StemRoute{Action: cli.StemSave, Target: "result"}
		}
		migrated.Steps = append(migrated.Steps, step)
	}

	return migrated
}

// saveUserPresetsLocked writes user presets to disk.
// Must be called with userPresetsMu already held (write lock).
func saveUserPresetsLocked() error {
	data, err := json.MarshalIndent(userPresets, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal presets: %w", err)
	}
	path := userPresetsFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create presets dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write presets: %w", err)
	}
	return nil
}

// loadDeletedPresets reads the tombstone list of factory presets removed by the user.
func loadDeletedPresets() {
	deletedPresetsMu.Lock()
	defer deletedPresetsMu.Unlock()
	deletedPresets = make(map[string]struct{})
	data, err := readConfigFile(deletedPresetsFile())
	if err != nil {
		return
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return
	}
	for _, name := range names {
		deletedPresets[name] = struct{}{}
	}
}

// saveDeletedPresetsLocked writes the tombstone list to disk.
// Must be called with deletedPresetsMu already held (write lock).
func saveDeletedPresetsLocked() error {
	names := make([]string, 0, len(deletedPresets))
	for name := range deletedPresets {
		names = append(names, name)
	}
	data, err := json.MarshalIndent(names, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal deleted presets: %w", err)
	}
	path := deletedPresetsFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create deleted presets dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write deleted presets: %w", err)
	}
	return nil
}

// isPresetDeleted reports whether a factory preset has been removed by the user.
func isPresetDeleted(name string) bool {
	deletedPresetsMu.RLock()
	defer deletedPresetsMu.RUnlock()
	_, ok := deletedPresets[name]
	return ok
}

// getAllPresets returns built-in presets + user presets merged.
// User presets with the same name override built-in ones.
// Factory presets listed in presets_deleted.json are omitted.
func getAllPresets() map[string]cli.Preset {
	deletedPresetsMu.RLock()
	defer deletedPresetsMu.RUnlock()
	result := make(map[string]cli.Preset, len(cli.Presets)+len(userPresets))
	for k, v := range cli.Presets {
		if _, deleted := deletedPresets[k]; deleted {
			continue
		}
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

	// When saving, strip Locked=true so user can edit their copy
	preset.Locked = false

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

	p, ok := getAllPresets()[name]
	if !ok {
		// Preset not found — nothing to delete.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	if p.Locked {
		// Built-in preset: tombstone it so it stays removed across restarts.
		deletedPresetsMu.Lock()
		deletedPresets[name] = struct{}{}
		err := saveDeletedPresetsLocked()
		deletedPresetsMu.Unlock()
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// Also drop it from the in-memory built-in map so it disappears immediately.
		delete(cli.Presets, name)

		Log("backend", "info", "Factory preset deleted: "+name)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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

func (s *Server) handleRestoreDefaultPresets(w http.ResponseWriter, r *http.Request) {
	deletedPresetsMu.Lock()
	deletedPresets = make(map[string]struct{})
	err := saveDeletedPresetsLocked()
	deletedPresetsMu.Unlock()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Re-seed the in-memory built-in presets so they reappear immediately.
	seedPresets()

	Log("backend", "success", "Factory presets restored")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// loadDefaultPreset reads the default preset name from disk.
func loadDefaultPreset() {
	data, err := readConfigFile(defaultPresetFile())
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
	path := defaultPresetFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "write error"})
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "write error"})
		return
	}

	Log("backend", "success", "Default preset set: "+body.Name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
