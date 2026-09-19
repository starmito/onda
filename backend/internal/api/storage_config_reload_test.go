package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/starmito/onda/internal/cli"
)

func TestStorageConfigPost_ReloadsInMemoryConfig(t *testing.T) {
	rootA := newTestRoot(t, "storage-reload-a-")
	rootB := newTestRoot(t, "storage-reload-b-")
	settingsPath := filepath.Join(newTestRoot(t, "storage-reload-settings-"), ".onda-settings.json")

	writeUserPresetFile(t, rootA, "PresetA", "preset from root A")
	writeUISettingsFile(t, rootA, "#ff0000")
	writeExportProfilesFile(t, rootA, "wav")

	writeUserPresetFile(t, rootB, "PresetB", "preset from root B")
	writeUISettingsFile(t, rootB, "#0000ff")
	writeExportProfilesFile(t, rootB, "flac")

	t.Setenv("ONDA_SETTINGS_FILE", settingsPath)
	t.Setenv("ONDA_DATA_DIR", rootA)

	srv := newReloadTestServer(t)

	assertPresetExists(t, srv, "PresetA", true)
	assertPresetExists(t, srv, "PresetB", false)
	assertUIAccent(t, srv, "#ff0000")
	assertDefaultFormat(t, srv, "wav")

	postStorageConfig(t, srv, rootB)

	assertPresetExists(t, srv, "PresetA", false)
	assertPresetExists(t, srv, "PresetB", true)
	assertUIAccent(t, srv, "#0000ff")
	assertDefaultFormat(t, srv, "flac")
}

func TestStorageConfigPost_ReloadsDefaultPreset(t *testing.T) {
	rootA := newTestRoot(t, "storage-reload-dp-a-")
	rootB := newTestRoot(t, "storage-reload-dp-b-")
	settingsPath := filepath.Join(newTestRoot(t, "storage-reload-dp-settings-"), ".onda-settings.json")

	writeDefaultPresetFile(t, rootA, "PresetA")
	writeDefaultPresetFile(t, rootB, "PresetB")

	t.Setenv("ONDA_SETTINGS_FILE", settingsPath)
	t.Setenv("ONDA_DATA_DIR", rootA)

	srv := newReloadTestServer(t)

	assertDefaultPreset(t, srv, "PresetA")
	postStorageConfig(t, srv, rootB)
	assertDefaultPreset(t, srv, "PresetB")
}

func newReloadTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	// Simulate server startup: load the in-memory config from the current root.
	loadUserPresets()
	loadDefaultPreset()
	if err := loadUISettings(); err != nil {
		t.Fatalf("failed to load UI settings: %v", err)
	}
	if err := loadExportProfiles(); err != nil {
		t.Fatalf("failed to load export profiles: %v", err)
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/presets", s.handleGetPresets)
	s.mux.HandleFunc("GET /api/presets/default", s.handleGetDefaultPreset)
	s.mux.HandleFunc("GET /api/settings/ui", s.handleGetUISettings)
	s.mux.HandleFunc("GET /api/export/profiles", s.handleGetExportProfiles)
	s.mux.HandleFunc("POST /api/storage/config", s.handleStorageConfigPost)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)
	return srv
}

func writeUserPresetFile(t *testing.T, root, name, description string) {
	t.Helper()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	presets := map[string]cli.Preset{
		name: {
			Name:        name,
			Description: description,
			Steps: []cli.PipelineStep{
				{ID: "vocal", Model: "BS_Roformer_Viperx", Type: "vocal", Enabled: true},
			},
		},
	}
	data, err := json.Marshal(presets)
	if err != nil {
		t.Fatalf("failed to marshal preset: %v", err)
	}
	path := filepath.Join(configDir, "presets_user.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write user presets: %v", err)
	}
}

func writeDefaultPresetFile(t *testing.T, root, name string) {
	t.Helper()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	data, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		t.Fatalf("failed to marshal default preset: %v", err)
	}
	path := filepath.Join(configDir, "default_preset.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write default preset: %v", err)
	}
}

func writeUISettingsFile(t *testing.T, root, accent string) {
	t.Helper()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	settings := UISettings{Accent: accent, Theme: "dark", FontSize: "medium", Scale: 100}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("failed to marshal ui settings: %v", err)
	}
	path := filepath.Join(configDir, "ui_settings.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write ui settings: %v", err)
	}
}

func writeExportProfilesFile(t *testing.T, root, defaultFormat string) {
	t.Helper()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	profiles := AudioExportProfiles{
		DefaultFormat: defaultFormat,
		NameTemplate:  "{song} ({pitches}) ({suffix})",
		Formats: map[string]*FormatProfile{
			"wav":  {BitDepth: "32f", SampleRate: "source"},
			"flac": {Compression: 5, BitDepth: "24"},
			"mp3":  {Bitrate: "320k", Mode: "cbr"},
		},
	}
	data, err := json.Marshal(profiles)
	if err != nil {
		t.Fatalf("failed to marshal export profiles: %v", err)
	}
	path := filepath.Join(configDir, "audio_export_profiles.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write export profiles: %v", err)
	}
}

func postStorageConfig(t *testing.T, srv *httptest.Server, root string) {
	t.Helper()
	body := fmt.Sprintf(`{"root":%q}`, root)
	resp, err := srv.Client().Post(srv.URL+"/api/storage/config", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/storage/config failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := json.Marshal(resp.Body)
		t.Fatalf("POST /api/storage/config returned %d: %s", resp.StatusCode, string(b))
	}
}

func assertPresetExists(t *testing.T, srv *httptest.Server, name string, want bool) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/api/presets")
	if err != nil {
		t.Fatalf("GET /api/presets failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/presets returned %d", resp.StatusCode)
	}
	var presets map[string]cli.Preset
	if err := json.NewDecoder(resp.Body).Decode(&presets); err != nil {
		t.Fatalf("failed to decode presets: %v", err)
	}
	if _, ok := presets[name]; ok != want {
		t.Errorf("preset %q existence = %v, want %v", name, ok, want)
	}
}

func assertDefaultPreset(t *testing.T, srv *httptest.Server, want string) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/api/presets/default")
	if err != nil {
		t.Fatalf("GET /api/presets/default failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/presets/default returned %d", resp.StatusCode)
	}
	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode default preset: %v", err)
	}
	if got["name"] != want {
		t.Errorf("default preset name = %q, want %q", got["name"], want)
	}
}

func assertUIAccent(t *testing.T, srv *httptest.Server, want string) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/api/settings/ui")
	if err != nil {
		t.Fatalf("GET /api/settings/ui failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/settings/ui returned %d", resp.StatusCode)
	}
	var got UISettings
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode ui settings: %v", err)
	}
	if got.Accent != want {
		t.Errorf("ui accent = %q, want %q", got.Accent, want)
	}
}

func assertDefaultFormat(t *testing.T, srv *httptest.Server, want string) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/api/export/profiles")
	if err != nil {
		t.Fatalf("GET /api/export/profiles failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/export/profiles returned %d", resp.StatusCode)
	}
	var got AudioExportProfiles
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode export profiles: %v", err)
	}
	if got.DefaultFormat != want {
		t.Errorf("default format = %q, want %q", got.DefaultFormat, want)
	}
}
