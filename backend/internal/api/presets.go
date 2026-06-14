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

	// Seed 4 built-in presets with Locked=true — only if they don't exist in user presets.
	seedPresets()

	loadUserPresets()
	loadDefaultPreset()
}

// seedPresets inserts the 4 default presets into the Presets map.
// If a user preset with the same name exists on disk, the user version takes precedence
// (loaded later in loadUserPresets which puts into userPresets, and getAllPresets lets user presets override).
func seedPresets() {
	// 1. Separador Voces Total → 1 step ViperX, vocals=route_to_result, instrumental=save
	cli.Presets["Voces Total"] = cli.Preset{
		Name:        "Voces Total",
		Description: "1 paso: ViperX separa voces → instrumental guardado, voces enviadas a resultado",
		Pitch:       0,
		Locked:      true,
		Steps: []cli.PipelineStep{
			{
				ID:      "viperx",
				Model:   "BS_Roformer_Viperx",
				Type:    "viperx",
				Enabled: true,
				Stems: map[string]cli.StemRoute{
					"vocals":       {Action: cli.StemSave, Target: "result"},
					"instrumental": {Action: cli.StemSave, Target: "result"},
				},
			},
		},
	}

	// 2. Eliminador de Voz → 1 step ViperX, vocals=discard, instrumental=save
	cli.Presets["Eliminador de Voz"] = cli.Preset{
		Name:        "Eliminador de Voz",
		Description: "1 paso: ViperX elimina voces, solo instrumental guardado",
		Pitch:       0,
		Locked:      true,
		Steps: []cli.PipelineStep{
			{
				ID:      "viperx",
				Model:   "BS_Roformer_Viperx",
				Type:    "viperx",
				Enabled: true,
				Stems: map[string]cli.StemRoute{
					"vocals":       {Action: cli.StemDiscard},
					"instrumental": {Action: cli.StemSave, Target: "result"},
				},
			},
		},
	}

	// 3. Separador Completo → 2 steps: ViperX vocals→route, Demucs htdemucs_ft drums,bass,other,vocals
	cli.Presets["Separador Completo"] = cli.Preset{
		Name:        "Separador Completo",
		Description: "2 pasos: ViperX separa voces → Demucs separa en drums, bass, other, vocals",
		Pitch:       0,
		Locked:      true,
		Steps: []cli.PipelineStep{
			{
				ID:      "viperx",
				Model:   "BS_Roformer_Viperx",
				Type:    "viperx",
				Enabled: true,
				Stems: map[string]cli.StemRoute{
					"vocals":       {Action: cli.StemRoute, Target: "step:demucs"},
					"instrumental": {Action: cli.StemRoute, Target: "step:demucs"},
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
					"vocals": {Action: cli.StemSave, Target: "result"},
				},
			},
		},
	}

	// 4. Solo Instrumentos → 1 step Demucs htdemucs_ft, drums,bass,other=save, vocals=discard
	cli.Presets["Solo Instrumentos"] = cli.Preset{
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
	}
}

func loadUserPresets() {
	data, err := os.ReadFile(userPresetsFile)
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

	// Try old format and migrate
	var oldPreset struct {
		Name          string   `json:"name"`
		ViperxEnabled bool     `json:"viperxEnabled"`
		DemucsEnabled bool     `json:"demucsEnabled"`
		VocalModel    string   `json:"vocalModel"`
		VocalOverlap  int      `json:"vocalOverlap"`
		StemModel     string   `json:"stemModel"`
		DrumsModel    string   `json:"drumsModel"`
		BassModel     string   `json:"bassModel"`
		OtherModel    string   `json:"otherModel"`
		ViperxStems   []string `json:"viperxStems"`
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

	// ViperX step
	if oldPreset.ViperxEnabled {
		vocalModel := oldPreset.VocalModel
		if vocalModel == "" {
			vocalModel = "BS_Roformer_Viperx"
		}
		step := cli.PipelineStep{
			ID:      "viperx",
			Model:   vocalModel,
			Type:    "viperx",
			Enabled: true,
			Stems:   make(map[string]cli.StemRoute),
		}
		if len(oldPreset.ViperxStems) > 0 {
			for _, s := range oldPreset.ViperxStems {
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

	// Check if the preset is locked (built-in) — cannot delete
	if p, ok := getAllPresets()[name]; ok && p.Locked {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "cannot delete a locked preset"})
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
