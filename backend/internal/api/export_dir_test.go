package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newExportDirTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/storage/config", s.handleStorageConfigGet)
	s.mux.HandleFunc("POST /api/storage/config", s.handleStorageConfigPost)
	s.mux.HandleFunc("POST /api/audio/export", s.handleExport)
	s.mux.HandleFunc("POST /api/stems/merge", s.handleStemsMerge)
	s.mux.HandleFunc("POST /api/daw/midi/export", s.handleMidiExport)
	s.mux.HandleFunc("GET /api/export/files/{file}", s.handleExportFileServe)
	s.mux.HandleFunc("GET /api/daw/stems", s.handleListStems)
	s.mux.HandleFunc("GET /api/pitch/{song}", s.handleListPitchSubgroups)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestExportDirConfigPrecedence verifies env > settings > default.
func TestExportDirConfigPrecedence(t *testing.T) {
	root := setupStorageTestRoot(t)

	envDir := filepath.Join(root, "env-exports")
	settingsDir := filepath.Join(root, "settings-exports")
	for _, d := range []string{envDir, settingsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", d, err)
		}
	}

	settingsPath := filepath.Join(root, ".onda-settings.json")
	t.Setenv("ONDA_SETTINGS_FILE", settingsPath)

	// 1. Default: <dataRoot>/exports.
	defaultExportDir := filepath.Join(root, "exports")
	t.Setenv("ONDA_DATA_DIR", root)
	t.Setenv("ONDA_EXPORT_DIR", "")
	if err := os.WriteFile(settingsPath, []byte(`{"data_root":""}`), 0o644); err != nil {
		t.Fatalf("failed to write settings: %v", err)
	}

	srv := newExportDirTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/api/storage/config")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	var body storageConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if body.ExportDir != defaultExportDir || body.ExportSource != "default" {
		t.Fatalf("default: expected %q/default, got %q/%q", defaultExportDir, body.ExportDir, body.ExportSource)
	}

	// 2. Settings win over default.
	if err := os.WriteFile(settingsPath, []byte(fmt.Sprintf(`{"export_dir":%q}`, settingsDir)), 0o644); err != nil {
		t.Fatalf("failed to write settings: %v", err)
	}
	resp, err = srv.Client().Get(srv.URL + "/api/storage/config")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if body.ExportDir != settingsDir || body.ExportSource != "settings" {
		t.Fatalf("settings: expected %q/settings, got %q/%q", settingsDir, body.ExportDir, body.ExportSource)
	}

	// 3. Env wins over settings.
	t.Setenv("ONDA_EXPORT_DIR", envDir)
	resp, err = srv.Client().Get(srv.URL + "/api/storage/config")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if body.ExportDir != envDir || body.ExportSource != "env" {
		t.Fatalf("env: expected %q/env, got %q/%q", envDir, body.ExportDir, body.ExportSource)
	}
}

func TestStorageConfigPost_ExportDirMissingPath(t *testing.T) {
	setupStorageTestRoot(t)
	srv := newStorageConfigTestServer(t)

	body := `{"export_dir":"/no/existe/onda-exports"}`
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
	if !bytes.Contains(b, []byte("does not exist")) && !bytes.Contains(b, []byte("cannot be accessed")) {
		t.Errorf("expected clear missing-path error, got %s", string(b))
	}
}

func TestStorageConfigPost_ExportDirReadOnlyPath(t *testing.T) {
	root := setTestRoot(t, "export-dir-ro-")
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatalf("failed to chmod root read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	srv := newStorageConfigTestServer(t)
	body := fmt.Sprintf(`{"export_dir":%q}`, root)
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
	if !bytes.Contains(b, []byte("not writable")) {
		t.Errorf("expected clear writable error, got %s", string(b))
	}
}

func TestStorageConfigPost_ExportDirTraversal(t *testing.T) {
	setupStorageTestRoot(t)
	srv := newStorageConfigTestServer(t)

	body := `{"export_dir":"/app/../etc"}`
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
	if !bytes.Contains(b, []byte("parent references")) && !bytes.Contains(b, []byte("..")) {
		t.Errorf("expected clear traversal error, got %s", string(b))
	}
}

func TestStorageConfigPost_ExportDirEmpty(t *testing.T) {
	root := setupStorageTestRoot(t)
	settingsPath := filepath.Join(root, ".onda-settings.json")
	t.Setenv("ONDA_SETTINGS_FILE", settingsPath)
	if err := os.WriteFile(settingsPath, []byte(`{"export_dir":"/some/dir"}`), 0o644); err != nil {
		t.Fatalf("failed to seed settings: %v", err)
	}
	t.Setenv("ONDA_EXPORT_DIR", "")

	srv := newStorageConfigTestServer(t)
	body := `{"export_dir":""}`
	resp, err := srv.Client().Post(srv.URL+"/api/storage/config", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	saved, err := loadStorageSettings()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	if saved.ExportDir != "" {
		t.Errorf("expected export_dir cleared, got %q", saved.ExportDir)
	}
}

func TestStorageConfigPost_ExportDirValid(t *testing.T) {
	root := setupStorageTestRoot(t)
	exportDir := filepath.Join(root, "my-exports")
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		t.Fatalf("failed to create export dir: %v", err)
	}

	settingsPath := filepath.Join(root, ".onda-settings.json")
	t.Setenv("ONDA_SETTINGS_FILE", settingsPath)
	t.Setenv("ONDA_DATA_DIR", root)
	t.Setenv("ONDA_EXPORT_DIR", "")

	srv := newExportDirTestServer(t)
	body := fmt.Sprintf(`{"export_dir":%q}`, exportDir)
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
	if respBody.ExportDir != exportDir {
		t.Errorf("export_dir: expected %q, got %q", exportDir, respBody.ExportDir)
	}
	if respBody.ExportSource != "env" {
		t.Errorf("export_source: expected env, got %q", respBody.ExportSource)
	}

	saved, err := loadStorageSettings()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	if saved.ExportDir != exportDir {
		t.Errorf("persisted export_dir: expected %q, got %q", exportDir, saved.ExportDir)
	}
}

func TestHandleExport_WAV_UsesExportDir(t *testing.T) {
	root := setupStorageTestRoot(t)
	exportDir := filepath.Join(root, "exports")
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		t.Fatalf("failed to create export dir: %v", err)
	}
	t.Setenv("ONDA_DATA_DIR", root)
	t.Setenv("ONDA_EXPORT_DIR", exportDir)

	content := []byte("exported-audio-content")
	writeTestFile(t, filepath.Join(root, "daw-data", "mix", "original", "mix.wav"), content)

	srv := newExportDirTestServer(t)
	body := `{"file":"mix.wav","format":"wav"}`
	resp, err := srv.Client().Post(srv.URL+"/api/audio/export", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var er ExportResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if er.File != "export_mix.wav" {
		t.Fatalf("expected export_mix.wav, got %s", er.File)
	}
	if er.URL != "/api/export/files/export_mix.wav" {
		t.Fatalf("expected /api/export/files/export_mix.wav, got %s", er.URL)
	}

	exportedPath := filepath.Join(exportDir, "export_mix.wav")
	if _, err := os.Stat(exportedPath); err != nil {
		t.Fatalf("expected exported file at %s: %v", exportedPath, err)
	}

	// Download via the dedicated route.
	downloadResp, err := srv.Client().Get(srv.URL + er.URL)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	defer downloadResp.Body.Close()
	if downloadResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(downloadResp.Body)
		t.Fatalf("expected 200 on download, got %d: %s", downloadResp.StatusCode, string(b))
	}
	downloaded, err := io.ReadAll(downloadResp.Body)
	if err != nil {
		t.Fatalf("failed to read download body: %v", err)
	}
	if !bytes.Equal(downloaded, content) {
		t.Fatalf("downloaded content mismatch")
	}
}

func TestHandleExport_MP3_UsesExportDir(t *testing.T) {
	skipIfMissingBinary(t, "ffmpeg")

	root := setupStorageTestRoot(t)
	exportDir := filepath.Join(root, "exports")
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		t.Fatalf("failed to create export dir: %v", err)
	}
	t.Setenv("ONDA_DATA_DIR", root)
	t.Setenv("ONDA_EXPORT_DIR", exportDir)

	sourcePath := filepath.Join(root, "input", "source.wav")
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "0.1", "-acodec", "pcm_s16le", sourcePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create test wav: %v\n%s", err, string(out))
	}

	srv := newExportDirTestServer(t)
	body := `{"file":"source.wav","format":"mp3"}`
	resp, err := srv.Client().Post(srv.URL+"/api/audio/export", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var er ExportResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if er.File != "export_source.mp3" {
		t.Fatalf("expected export_source.mp3, got %s", er.File)
	}
	if er.URL != "/api/export/files/export_source.mp3" {
		t.Fatalf("expected /api/export/files/export_source.mp3, got %s", er.URL)
	}

	exportedPath := filepath.Join(exportDir, "export_source.mp3")
	if _, err := os.Stat(exportedPath); err != nil {
		t.Fatalf("expected exported file at %s: %v", exportedPath, err)
	}
}

func TestHandleStemsMerge_UsesExportDir(t *testing.T) {
	skipIfMissingBinary(t, "ffmpeg")

	root := setupStorageTestRoot(t)
	exportDir := filepath.Join(root, "exports")
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		t.Fatalf("failed to create export dir: %v", err)
	}
	t.Setenv("ONDA_DATA_DIR", root)
	t.Setenv("ONDA_EXPORT_DIR", exportDir)

	songDir := filepath.Join(root, "output", "cancion1")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}
	for _, stem := range []string{"vocals.wav", "instrumental.wav"} {
		cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "0.1", "-acodec", "pcm_s16le", filepath.Join(songDir, stem))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("failed to create test wav %s: %v\n%s", stem, err, string(out))
		}
	}

	srv := newExportDirTestServer(t)
	body := `{"song":"cancion1","stems":["vocals.wav","instrumental.wav"],"format":"flac"}`
	resp, err := srv.Client().Post(srv.URL+"/api/stems/merge", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var mr MergeResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !strings.HasPrefix(mr.File, "merge_cancion1") {
		t.Fatalf("expected merge_cancion1 prefix, got %s", mr.File)
	}
	if mr.URL != "/api/export/files/"+mr.File {
		t.Fatalf("expected /api/export/files/%s, got %s", mr.File, mr.URL)
	}

	exportedPath := filepath.Join(exportDir, mr.File)
	if _, err := os.Stat(exportedPath); err != nil {
		t.Fatalf("expected merged file at %s: %v", exportedPath, err)
	}
}

func TestHandleMidiExport_UsesExportDir(t *testing.T) {
	root := setupStorageTestRoot(t)
	exportDir := filepath.Join(root, "exports")
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		t.Fatalf("failed to create export dir: %v", err)
	}
	t.Setenv("ONDA_DATA_DIR", root)
	t.Setenv("ONDA_EXPORT_DIR", exportDir)

	srv := newExportDirTestServer(t)
	body := `{"tracks":[{"index":0,"name":"T","notes":[{"track":0,"channel":0,"key":60,"velocity":100,"start_ms":0,"end_ms":500}]}],"bpm":120}`
	resp, err := srv.Client().Post(srv.URL+"/api/daw/midi/export", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var mr MidiExportResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if mr.File != "export.mid" {
		t.Fatalf("expected export.mid, got %s", mr.File)
	}
	if mr.URL != "/api/export/files/export.mid" {
		t.Fatalf("expected /api/export/files/export.mid, got %s", mr.URL)
	}

	exportedPath := filepath.Join(exportDir, "export.mid")
	if _, err := os.Stat(exportedPath); err != nil {
		t.Fatalf("expected MIDI file at %s: %v", exportedPath, err)
	}

	// Download via the dedicated route.
	downloadResp, err := srv.Client().Get(srv.URL + mr.URL)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	defer downloadResp.Body.Close()
	if downloadResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(downloadResp.Body)
		t.Fatalf("expected 200 on download, got %d: %s", downloadResp.StatusCode, string(b))
	}
}

func TestHandleMidiExport_DefaultUsesExportDir(t *testing.T) {
	root := setupStorageTestRoot(t)
	t.Setenv("ONDA_DATA_DIR", root)
	t.Setenv("ONDA_EXPORT_DIR", "")

	srv := newExportDirTestServer(t)
	body := `{"tracks":[{"index":0,"name":"T","notes":[{"track":0,"channel":0,"key":60,"velocity":100,"start_ms":0,"end_ms":500}]}],"bpm":120}`
	resp, err := srv.Client().Post(srv.URL+"/api/daw/midi/export", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var mr MidiExportResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if mr.File != "export.mid" {
		t.Fatalf("expected export.mid, got %s", mr.File)
	}
	if mr.URL != "/api/export/files/export.mid" {
		t.Fatalf("expected /api/export/files/export.mid, got %s", mr.URL)
	}

	exportedPath := filepath.Join(root, "exports", "export.mid")
	if _, err := os.Stat(exportedPath); err != nil {
		t.Fatalf("expected MIDI file at default export dir %s: %v", exportedPath, err)
	}
}

// TestHandleStemsMerge_DefaultExportDir verifies that, with no explicit export
// directory configured, a merge is written to <dataRoot>/exports and does not
// appear in the stem or pitch subgroup listings.
func TestHandleStemsMerge_DefaultExportDir(t *testing.T) {
	skipIfMissingBinary(t, "ffmpeg")

	root := setupStorageTestRoot(t)
	t.Setenv("ONDA_DATA_DIR", root)
	t.Setenv("ONDA_EXPORT_DIR", "")

	songDir := filepath.Join(root, "output", "cancion1")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}
	for _, stem := range []string{"vocals.wav", "instrumental.wav"} {
		cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "0.1", "-acodec", "pcm_s16le", filepath.Join(songDir, stem))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("failed to create test wav %s: %v\n%s", stem, err, string(out))
		}
	}

	srv := newExportDirTestServer(t)
	body := `{"song":"cancion1","stems":["vocals.wav","instrumental.wav"],"format":"flac"}`
	resp, err := srv.Client().Post(srv.URL+"/api/stems/merge", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var mr MergeResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	defaultExportDir := filepath.Join(root, "exports")
	exportedPath := filepath.Join(defaultExportDir, mr.File)
	if _, err := os.Stat(exportedPath); err != nil {
		t.Fatalf("expected merged file at default export dir %s: %v", exportedPath, err)
	}

	legacyPath := filepath.Join(songDir, mr.File)
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("merged file must not be written next to stems: %s", legacyPath)
	}

	// The export must not appear in the stem list.
	stemsResp, err := srv.Client().Get(srv.URL + "/api/daw/stems")
	if err != nil {
		t.Fatalf("stems request failed: %v", err)
	}
	defer stemsResp.Body.Close()
	var stems StemsResponse
	if err := json.NewDecoder(stemsResp.Body).Decode(&stems); err != nil {
		t.Fatalf("failed to decode stems: %v", err)
	}
	for _, name := range stems.Output["cancion1"] {
		if name == mr.File {
			t.Fatalf("export file %q must not appear in /api/daw/stems", mr.File)
		}
	}

	// The export must not appear in the pitch subgroup list.
	pitchResp, err := srv.Client().Get(srv.URL + "/api/pitch/cancion1")
	if err != nil {
		t.Fatalf("pitch request failed: %v", err)
	}
	defer pitchResp.Body.Close()
	var subgroups []PitchSubgroup
	if err := json.NewDecoder(pitchResp.Body).Decode(&subgroups); err != nil {
		t.Fatalf("failed to decode pitch subgroups: %v", err)
	}
	for _, sg := range subgroups {
		for _, f := range sg.Files {
			if f.Name == mr.File {
				t.Fatalf("export file %q must not appear in /api/pitch/{song}", mr.File)
			}
		}
	}
}

// TestListStemsFiltersLegacyExports verifies that export files already present
// in the song directory (written before the default export dir moved to
// <dataRoot>/exports) are not listed as stems.
func TestListStemsFiltersLegacyExports(t *testing.T) {
	root := setupStorageTestRoot(t)
	t.Setenv("ONDA_DATA_DIR", root)

	songDir := filepath.Join(root, "output", "cancion1")
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}
	writeTestFile(t, filepath.Join(songDir, "vocals.wav"), []byte("vocals"))
	writeTestFile(t, filepath.Join(songDir, "merge_cancion1.flac"), []byte("legacy export"))
	writeTestFile(t, filepath.Join(songDir, "export.mid"), []byte("legacy midi"))

	srv := newExportDirTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/api/daw/stems")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var stems StemsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stems); err != nil {
		t.Fatalf("failed to decode stems: %v", err)
	}
	want := []string{"vocals.wav"}
	if got := stems.Output["cancion1"]; !sliceEqual(got, want) {
		t.Fatalf("expected stems %v, got %v", want, got)
	}
}

// TestListPitchSubgroupsFiltersLegacyExports verifies that export files already
// present inside a pitch subgroup directory are not listed.
func TestListPitchSubgroupsFiltersLegacyExports(t *testing.T) {
	root := setupStorageTestRoot(t)
	t.Setenv("ONDA_DATA_DIR", root)

	songDir := filepath.Join(root, "output", "cancion1")
	pitchDir := filepath.Join(songDir, "cancion1_pitch-1")
	if err := os.MkdirAll(pitchDir, 0o755); err != nil {
		t.Fatalf("failed to create pitch dir: %v", err)
	}
	writeTestFile(t, filepath.Join(pitchDir, "bass_pitch-1.wav"), []byte("bass"))
	writeTestFile(t, filepath.Join(pitchDir, "merge_cancion1.flac"), []byte("legacy export in subgroup"))

	srv := newExportDirTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/api/pitch/cancion1")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var subgroups []PitchSubgroup
	if err := json.NewDecoder(resp.Body).Decode(&subgroups); err != nil {
		t.Fatalf("failed to decode pitch subgroups: %v", err)
	}
	if len(subgroups) != 1 {
		t.Fatalf("expected 1 subgroup, got %d", len(subgroups))
	}
	want := []string{"bass_pitch-1.wav"}
	var got []string
	for _, f := range subgroups[0].Files {
		got = append(got, f.Name)
	}
	if !sliceEqual(got, want) {
		t.Fatalf("expected subgroup files %v, got %v", want, got)
	}
}

// TestNoHardcodedExportDirPaths fails if any production source file hardcodes
// an export directory path, ensuring the export folder stays configurable.
func TestNoHardcodedExportDirPaths(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot get working directory: %v", err)
	}
	repoRoot := findProjectRootFrom(cwd)
	if repoRoot == "" {
		t.Fatal("cannot find repository root (no VERSION marker)")
	}

	// Only fixed export destination directory names are forbidden. The API
	// route /api/export/files/ is legitimate and must be ignored.
	forbidden := []string{"/app/exports", "/exports"}

	backendDir := filepath.Join(repoRoot, "backend")
	var violations []string
	if err := filepath.Walk(backendDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		if strings.HasSuffix(p, "_test.go") {
			return nil
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repoRoot, p)
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			for _, lit := range forbidden {
				if strings.Contains(line, lit) {
					violations = append(violations, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("walk backend go files: %v", err)
	}

	if len(violations) > 0 {
		t.Fatalf("hardcoded export paths found:\n%s", strings.Join(violations, "\n"))
	}
}
