package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/starmito/onda/internal/cli"
	"gopkg.in/yaml.v3"
)

func TestConfigDir_RespectsEnvAndSettings(t *testing.T) {
	root := setTestRoot(t, "config-dir-precedence-")
	settingsPath := filepath.Join(newTestRoot(t, "config-dir-settings-"), ".onda-settings.json")
	envDir := filepath.Join(root, "config-env")
	settingsDir := filepath.Join(root, "config-settings")
	for _, d := range []string{envDir, settingsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", d, err)
		}
	}

	t.Setenv("ONDA_SETTINGS_FILE", settingsPath)
	t.Setenv("ONDA_DATA_DIR", root)

	// Default: <dataRoot>/config.
	t.Setenv("ONDA_CONFIG_DIR", "")
	if err := os.WriteFile(settingsPath, []byte(`{"data_root":""}`), 0o644); err != nil {
		t.Fatalf("failed to write settings: %v", err)
	}
	if got := configDir(); got != filepath.Join(root, "config") {
		t.Errorf("default config dir = %q, want %q", got, filepath.Join(root, "config"))
	}

	// Settings win over default.
	if err := os.WriteFile(settingsPath, []byte(fmt.Sprintf(`{"config_dir":%q}`, settingsDir)), 0o644); err != nil {
		t.Fatalf("failed to write settings: %v", err)
	}
	if got := configDir(); got != settingsDir {
		t.Errorf("settings config dir = %q, want %q", got, settingsDir)
	}

	// Env wins over settings.
	t.Setenv("ONDA_CONFIG_DIR", envDir)
	if got := configDir(); got != envDir {
		t.Errorf("env config dir = %q, want %q", got, envDir)
	}
}

func TestValidateConfigDir_RejectsOutsideRoot(t *testing.T) {
	root := setTestRoot(t, "config-dir-outside-")
	t.Setenv("ONDA_DATA_DIR", root)

	outside := filepath.Join(filepath.Dir(root), "otra-config")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("failed to create outside dir: %v", err)
	}

	err := validateConfigDir(outside)
	if err == nil {
		t.Fatal("expected validation error for config dir outside root")
	}
	if !strings.Contains(err.Error(), "La carpeta de configuración debe estar dentro de la raíz de datos") {
		t.Errorf("expected Spanish error message, got %q", err.Error())
	}
}

func TestValidateConfigDir_AcceptsInsideRoot(t *testing.T) {
	root := setTestRoot(t, "config-dir-inside-")
	t.Setenv("ONDA_DATA_DIR", root)

	inside := filepath.Join(root, "mi-config")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatalf("failed to create inside dir: %v", err)
	}

	if err := validateConfigDir(inside); err != nil {
		t.Errorf("expected inside dir to be accepted, got %v", err)
	}
}

func TestStorageConfigPost_ConfigDir(t *testing.T) {
	root := setTestRoot(t, "config-dir-post-")
	settingsPath := filepath.Join(newTestRoot(t, "config-dir-post-settings-"), ".onda-settings.json")
	t.Setenv("ONDA_SETTINGS_FILE", settingsPath)
	t.Setenv("ONDA_DATA_DIR", root)

	configDir := filepath.Join(root, "config_prueba")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	srv := newStorageConfigTestServer(t)
	body := fmt.Sprintf(`{"config_dir":%q}`, configDir)
	resp, err := srv.Client().Post(srv.URL+"/api/storage/config", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var respBody storageConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if respBody.ConfigDir != configDir {
		t.Errorf("config_dir = %q, want %q", respBody.ConfigDir, configDir)
	}
	if respBody.ConfigSource != "env" {
		t.Errorf("config_source = %q, want env (applied in hot)", respBody.ConfigSource)
	}

	saved, err := loadStorageSettings()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	if saved.ConfigDir != configDir {
		t.Errorf("persisted config_dir = %q, want %q", saved.ConfigDir, configDir)
	}

	if got := os.Getenv("ONDA_CONFIG_DIR"); got != configDir {
		t.Errorf("ONDA_CONFIG_DIR = %q, want %q", got, configDir)
	}
}

func TestStorageConfigPost_ConfigDirOutsideRootRejected(t *testing.T) {
	root := setTestRoot(t, "config-dir-post-rejected-")
	outside := filepath.Join(filepath.Dir(root), "config-fuera")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("failed to create outside dir: %v", err)
	}

	srv := newStorageConfigTestServer(t)
	body := fmt.Sprintf(`{"config_dir":%q}`, outside)
	resp, err := srv.Client().Post(srv.URL+"/api/storage/config", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, string(b))
	}
	b, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(b, []byte("La carpeta de configuración debe estar dentro de la raíz de datos")) {
		t.Errorf("expected Spanish validation error, got %s", string(b))
	}
}

func TestConfigDir_ReloadReadsFromNewDir(t *testing.T) {
	root := setTestRoot(t, "config-dir-reload-")
	settingsPath := filepath.Join(newTestRoot(t, "config-dir-reload-settings-"), ".onda-settings.json")

	legacyDir := filepath.Join(root, "config")
	newDir := filepath.Join(root, "config_prueba")
	for _, d := range []string{legacyDir, newDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", d, err)
		}
	}

	writeUserPresetFile(t, root, "LegacyPreset", "preset from legacy")
	writeUISettingsFile(t, root, "#ff0000")
	writeExportProfilesFile(t, root, "wav")

	// Place different files in the new config dir.
	writeUserPresetsAt(t, newDir, "NewPreset", "preset from new dir")
	writeUISettingsAt(t, newDir, "#0000ff")
	writeExportProfilesAt(t, newDir, "flac")

	t.Setenv("ONDA_SETTINGS_FILE", settingsPath)
	t.Setenv("ONDA_DATA_DIR", root)

	srv := newReloadTestServer(t)

	assertPresetExists(t, srv, "LegacyPreset", true)
	assertPresetExists(t, srv, "NewPreset", false)
	assertUIAccent(t, srv, "#ff0000")
	assertDefaultFormat(t, srv, "wav")

	postStorageConfigDir(t, srv, newDir)

	assertPresetExists(t, srv, "LegacyPreset", false)
	assertPresetExists(t, srv, "NewPreset", true)
	assertUIAccent(t, srv, "#0000ff")
	assertDefaultFormat(t, srv, "flac")
}

func TestConfigDir_LegacyFallback(t *testing.T) {
	root := setTestRoot(t, "config-dir-fallback-")
	settingsPath := filepath.Join(newTestRoot(t, "config-dir-fallback-settings-"), ".onda-settings.json")

	legacyDir := filepath.Join(root, "config")
	newDir := filepath.Join(root, "config_prueba")
	for _, d := range []string{legacyDir, newDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", d, err)
		}
	}

	// Only the legacy dir has the files.
	writeUserPresetsAt(t, legacyDir, "OnlyLegacy", "preset only in legacy")
	writeUISettingsAt(t, legacyDir, "#00ff00")
	writeExportProfilesAt(t, legacyDir, "mp3")

	t.Setenv("ONDA_SETTINGS_FILE", settingsPath)
	t.Setenv("ONDA_DATA_DIR", root)

	// Point config dir to the new (empty) directory.
	if err := os.Setenv("ONDA_CONFIG_DIR", newDir); err != nil {
		t.Fatalf("failed to set ONDA_CONFIG_DIR: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("ONDA_CONFIG_DIR") })

	srv := newReloadTestServer(t)

	assertPresetExists(t, srv, "OnlyLegacy", true)
	assertUIAccent(t, srv, "#00ff00")
	assertDefaultFormat(t, srv, "mp3")
}

func TestConfigDir_LegacyFallback_ModelConfig(t *testing.T) {
	root := setTestRoot(t, "config-dir-model-fallback-")
	settingsPath := filepath.Join(newTestRoot(t, "config-dir-model-fallback-settings-"), ".onda-settings.json")

	legacyDir := filepath.Join(root, "config", "model_configs")
	newDir := filepath.Join(root, "config_prueba", "model_configs")
	for _, d := range []string{legacyDir, newDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", d, err)
		}
	}

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      4,
		Segment:     10,
		Jobs:        2,
	}
	writeModelConfigYamlAt(t, legacyDir, "htdemucs_ft", cfg)

	t.Setenv("ONDA_SETTINGS_FILE", settingsPath)
	t.Setenv("ONDA_DATA_DIR", root)
	if err := os.Setenv("ONDA_CONFIG_DIR", filepath.Join(root, "config_prueba")); err != nil {
		t.Fatalf("failed to set ONDA_CONFIG_DIR: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("ONDA_CONFIG_DIR") })

	req := &SeparateRequest{
		Input:     "/app/input/song.wav",
		Demucs:    true,
		StemModel: "htdemucs_ft",
	}
	_, args, _, _, _ := buildPipelineArgs(req)

	if got := argValue(args, "--shifts"); got != "4" {
		t.Errorf("expected --shifts 4 from legacy fallback, got %q", got)
	}
}

func postStorageConfigDir(t *testing.T, srv *httptest.Server, dir string) {
	t.Helper()
	body := fmt.Sprintf(`{"config_dir":%q}`, dir)
	resp, err := srv.Client().Post(srv.URL+"/api/storage/config", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/storage/config failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /api/storage/config returned %d: %s", resp.StatusCode, string(b))
	}
}

func writeUserPresetsAt(t *testing.T, dir, name, description string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
	path := filepath.Join(dir, "presets_user.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write user presets: %v", err)
	}
}

func writeUISettingsAt(t *testing.T, dir, accent string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	settings := UISettings{Accent: accent, Theme: "dark", FontSize: "medium", Scale: 100}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("failed to marshal ui settings: %v", err)
	}
	path := filepath.Join(dir, "ui_settings.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write ui settings: %v", err)
	}
}

func writeExportProfilesAt(t *testing.T, dir, defaultFormat string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
	path := filepath.Join(dir, "audio_export_profiles.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write export profiles: %v", err)
	}
}

func writeModelConfigYamlAt(t *testing.T, dir, name string, cfg ModelConfigResponse) {
	t.Helper()
	data, err := yaml.Marshal(map[string]interface{}{
		"inference": map[string]interface{}{
			"dim_t":       cfg.SegmentSize,
			"num_overlap": 4,
			"batch_size":  cfg.BatchSize,
			"chunk_size":  cfg.ChunkSize,
		},
		"demucs": map[string]interface{}{
			"shifts":  cfg.Shifts,
			"segment": cfg.Segment,
			"jobs":    cfg.Jobs,
		},
	})
	if err != nil {
		t.Fatalf("failed to marshal model yaml: %v", err)
	}
	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write model yaml: %v", err)
	}
}
