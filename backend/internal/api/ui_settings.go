package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
)

const uiSettingsFile = "/app/config/ui_settings.json"

// UISettings holds persisted UI configuration (accent, theme, font size, scale).
type UISettings struct {
	Accent   string `json:"accent"`
	Theme    string `json:"theme"`
	FontSize string `json:"fontSize"`
	Scale    int    `json:"scale"`
}

var (
	uiSettings   UISettings
	uiSettingsMu sync.RWMutex
)

func loadUISettings() error {
	data, err := os.ReadFile(uiSettingsFile)
	if err != nil {
		if os.IsNotExist(err) {
			// File does not exist — use defaults
			uiSettingsMu.Lock()
			uiSettings = UISettings{
				Accent:   "#6c5ce7",
				Theme:    "dark",
				FontSize: "medium",
				Scale:    100,
			}
			uiSettingsMu.Unlock()
			return nil
		}
		return fmt.Errorf("read ui settings: %w", err)
	}

	uiSettingsMu.Lock()
	defer uiSettingsMu.Unlock()
	if err := json.Unmarshal(data, &uiSettings); err != nil {
		return fmt.Errorf("unmarshal ui settings: %w", err)
	}
	return nil
}

func saveUISettings(settings *UISettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ui settings: %w", err)
	}
	if err := os.WriteFile(uiSettingsFile, data, 0644); err != nil {
		return fmt.Errorf("write ui settings: %w", err)
	}
	return nil
}

func (s *Server) handleGetUISettings(w http.ResponseWriter, r *http.Request) {
	uiSettingsMu.RLock()
	settings := uiSettings
	uiSettingsMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

func (s *Server) handleSaveUISettings(w http.ResponseWriter, r *http.Request) {
	var newSettings UISettings
	if err := json.NewDecoder(r.Body).Decode(&newSettings); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	if err := saveUISettings(&newSettings); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	uiSettingsMu.Lock()
	uiSettings = newSettings
	uiSettingsMu.Unlock()

	Log("backend", "success", "UI settings saved")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
