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
)

func setupStorageTestRoot(t *testing.T) string {
	t.Helper()
	root := setTestRoot(t, "storage-test-")

	for _, dir := range []string{"input", "input_rubberband", "daw-data", "output", "models", "logs"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	return root
}

func newStorageTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/storage/usage", s.handleStorageUsage)
	s.mux.HandleFunc("POST /api/storage/clean", s.handleStorageClean)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestStorageUsage_ExpectedKeysAndSum(t *testing.T) {
	root := setupStorageTestRoot(t)

	writeTestFile(t, filepath.Join(root, "input", "a.wav"), []byte("aa"))
	writeTestFile(t, filepath.Join(root, "input", "b.wav"), []byte("bbb"))
	writeTestFile(t, filepath.Join(root, "input_rubberband", "pitch.wav"), []byte("pitch"))
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "original", "original.wav"), []byte("orig"))
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "edits", "eq.wav"), []byte("edit"))
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "tmp", "tmp.txt"), []byte("temp"))
	writeTestFile(t, filepath.Join(root, "output", "song1", "vocals.wav"), []byte("vocals"))
	writeTestFile(t, filepath.Join(root, "models", "model.pth"), []byte("modeldata"))
	writeTestFile(t, filepath.Join(root, "logs", "onda.log"), []byte("logline"))

	srv := newStorageTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/api/storage/usage")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var body struct {
		Folders   map[string]folderUsage `json:"folders"`
		FreeBytes int64                  `json:"free_bytes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	expectedKeys := []string{"input", "input_rubberband", "daw-data", "output", "models", "logs"}
	for _, key := range expectedKeys {
		if _, ok := body.Folders[key]; !ok {
			t.Errorf("missing folder key %q", key)
		}
	}

	if body.Folders["input"].Files != 2 {
		t.Errorf("input files: expected 2, got %d", body.Folders["input"].Files)
	}
	if body.Folders["input"].Bytes != 5 {
		t.Errorf("input bytes: expected 5, got %d", body.Folders["input"].Bytes)
	}
	if body.Folders["daw-data"].Files != 3 {
		t.Errorf("daw-data files: expected 3, got %d", body.Folders["daw-data"].Files)
	}
	if body.Folders["output"].Files != 1 {
		t.Errorf("output files: expected 1, got %d", body.Folders["output"].Files)
	}
	if body.FreeBytes <= 0 {
		t.Errorf("expected positive free_bytes, got %d", body.FreeBytes)
	}
}

func TestStorageUsage_ModelsEntryCount(t *testing.T) {
	root := setupStorageTestRoot(t)

	// Two real model weight files plus auxiliary files that must not count.
	writeTestFile(t, filepath.Join(root, "models", "Demucs_Models", "model_a.pth"), []byte("model_a_data"))
	writeTestFile(t, filepath.Join(root, "models", "Demucs_Models", "model_b.pth"), []byte("model_b"))
	writeTestFile(t, filepath.Join(root, "models", "Demucs_Models", "model_a.yaml"), []byte("config"))
	writeTestFile(t, filepath.Join(root, "models", "Demucs_Models", ".gitattributes"), []byte("gitattrs"))
	writeTestFile(t, filepath.Join(root, "models", "VR_Models", "model_c.ckpt"), []byte("model_c"))

	srv := newStorageTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/api/storage/usage")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var body struct {
		Folders   map[string]folderUsage `json:"folders"`
		FreeBytes int64                  `json:"free_bytes"`
		Models    struct {
			Entries int64 `json:"entries"`
			Bytes   int64 `json:"bytes"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body.Folders["models"].Files != 5 {
		t.Errorf("models folder files: expected 5 (including aux), got %d", body.Folders["models"].Files)
	}
	if body.Models.Entries != 3 {
		t.Errorf("models entries: expected 3, got %d", body.Models.Entries)
	}
	wantBytes := int64(len("model_a_data") + len("model_b") + len("model_c"))
	if body.Models.Bytes != wantBytes {
		t.Errorf("models bytes: expected %d, got %d", wantBytes, body.Models.Bytes)
	}
}

func TestStorageClean_Tmp(t *testing.T) {
	root := setupStorageTestRoot(t)
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "original", "original.wav"), []byte("orig"))
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "imports", "import.wav"), []byte("import"))
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "edits", "eq.wav"), []byte("edit"))
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "tmp", "tmp.txt"), []byte("temp"))

	srv := newStorageTestServer(t)
	body := `{"action":"tmp"}`
	resp, err := srv.Client().Post(srv.URL+"/api/storage/clean", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var result cleanResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Action != "tmp" {
		t.Errorf("expected action tmp, got %q", result.Action)
	}
	if result.Files != 1 {
		t.Errorf("expected 1 file deleted, got %d", result.Files)
	}
	if result.Bytes != 4 {
		t.Errorf("expected 4 bytes deleted, got %d", result.Bytes)
	}

	if _, err := os.Stat(filepath.Join(root, "daw-data", "song1", "tmp", "tmp.txt")); !os.IsNotExist(err) {
		t.Errorf("tmp file should have been deleted")
	}
	for _, p := range []string{
		filepath.Join(root, "daw-data", "song1", "original", "original.wav"),
		filepath.Join(root, "daw-data", "song1", "imports", "import.wav"),
		filepath.Join(root, "daw-data", "song1", "edits", "eq.wav"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("file should not have been deleted: %s (%v)", p, err)
		}
	}
}

func TestStorageClean_AllEdits_KeepsOriginalsAndImports(t *testing.T) {
	root := setupStorageTestRoot(t)
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "original", "original.wav"), []byte("orig"))
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "imports", "import.wav"), []byte("import"))
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "edits", "eq.wav"), []byte("edit"))
	writeTestFile(t, filepath.Join(root, "daw-data", "song1", "edits", "reverb.wav"), []byte("reverb"))

	srv := newStorageTestServer(t)
	body := `{"action":"all-edits"}`
	resp, err := srv.Client().Post(srv.URL+"/api/storage/clean", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var result cleanResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Action != "all-edits" {
		t.Errorf("expected action all-edits, got %q", result.Action)
	}
	if result.Files != 2 {
		t.Errorf("expected 2 files deleted, got %d", result.Files)
	}

	for _, p := range []string{
		filepath.Join(root, "daw-data", "song1", "original", "original.wav"),
		filepath.Join(root, "daw-data", "song1", "imports", "import.wav"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("file should not have been deleted: %s (%v)", p, err)
		}
	}
	for _, p := range []string{
		filepath.Join(root, "daw-data", "song1", "edits", "eq.wav"),
		filepath.Join(root, "daw-data", "song1", "edits", "reverb.wav"),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("edit file should have been deleted: %s", p)
		}
	}
}

func TestStorageClean_OrphanEdits_OnlySongsWithoutOriginal(t *testing.T) {
	root := setupStorageTestRoot(t)
	// Song with original: edits must survive.
	writeTestFile(t, filepath.Join(root, "daw-data", "with_original", "original", "original.wav"), []byte("orig"))
	writeTestFile(t, filepath.Join(root, "daw-data", "with_original", "edits", "eq.wav"), []byte("edit"))

	// Song without original: edits must be removed.
	writeTestFile(t, filepath.Join(root, "daw-data", "orphan", "edits", "reverb.wav"), []byte("reverb"))

	srv := newStorageTestServer(t)
	body := `{"action":"orphan-edits"}`
	resp, err := srv.Client().Post(srv.URL+"/api/storage/clean", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var result cleanResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Action != "orphan-edits" {
		t.Errorf("expected action orphan-edits, got %q", result.Action)
	}
	if result.Files != 1 {
		t.Errorf("expected 1 file deleted, got %d", result.Files)
	}

	if _, err := os.Stat(filepath.Join(root, "daw-data", "with_original", "edits", "eq.wav")); err != nil {
		t.Errorf("edit of song with original should survive")
	}
	if _, err := os.Stat(filepath.Join(root, "daw-data", "orphan", "edits", "reverb.wav")); !os.IsNotExist(err) {
		t.Errorf("orphan edit should have been deleted")
	}
}

func TestStorageClean_UnknownAction(t *testing.T) {
	setupStorageTestRoot(t)
	srv := newStorageTestServer(t)
	body := `{"action":"bad-action"}`
	resp, err := srv.Client().Post(srv.URL+"/api/storage/clean", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(b, []byte("unknown action")) {
		t.Errorf("expected clear unknown action error, got %s", string(b))
	}
}

// TestStorageUsage_DataRootFromEnv verifies that /api/storage/usage reads the
// configured ONDA_DATA_DIR instead of falling back to the project root. With the
// old code models/ and logs/ under the configured data root would be reported as
// 0 because the handler ignored ONDA_DATA_DIR.
func TestStorageUsage_DataRootFromEnv(t *testing.T) {
	assertTestRoot(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot get working directory: %v", err)
	}
	dataRoot, err := os.MkdirTemp(cwd, "storage-data-root-")
	if err != nil {
		t.Fatalf("failed to create data root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dataRoot) })

	t.Setenv("ONDA_DATA_DIR", dataRoot)

	writeTestFile(t, filepath.Join(dataRoot, "models", "model1.pth"), []byte("model1"))
	writeTestFile(t, filepath.Join(dataRoot, "models", "model2.pth"), []byte("model2"))
	writeTestFile(t, filepath.Join(dataRoot, "logs", "onda.log"), []byte("logline"))

	srv := newStorageTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/api/storage/usage")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var body struct {
		Folders   map[string]folderUsage `json:"folders"`
		FreeBytes int64                  `json:"free_bytes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body.Folders["models"].Files != 2 {
		t.Errorf("models files: expected 2, got %d", body.Folders["models"].Files)
	}
	if body.Folders["models"].Bytes != 12 {
		t.Errorf("models bytes: expected 12, got %d", body.Folders["models"].Bytes)
	}
	if body.Folders["logs"].Files != 1 {
		t.Errorf("logs files: expected 1, got %d", body.Folders["logs"].Files)
	}
	if body.Folders["logs"].Bytes != 7 {
		t.Errorf("logs bytes: expected 7, got %d", body.Folders["logs"].Bytes)
	}
	if body.FreeBytes <= 0 {
		t.Errorf("expected positive free_bytes, got %d", body.FreeBytes)
	}
}

func newStorageConfigTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/storage/config", s.handleStorageConfigGet)
	s.mux.HandleFunc("POST /api/storage/config", s.handleStorageConfigPost)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestStorageConfigGet_FromEnv(t *testing.T) {
	root := setupStorageTestRoot(t)
	t.Setenv("ONDA_DATA_DIR", root)

	srv := newStorageConfigTestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/api/storage/config")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var body storageConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body.CurrentRoot != root {
		t.Errorf("current_root: expected %q, got %q", root, body.CurrentRoot)
	}
	if body.Source != "env" {
		t.Errorf("source: expected env, got %q", body.Source)
	}
	if !body.Exists {
		t.Errorf("expected exists=true")
	}
	if !body.Writable {
		t.Errorf("expected writable=true")
	}
	for _, name := range allStorageFolders {
		if _, ok := body.Folders[name]; !ok {
			t.Errorf("missing folder key %q", name)
		}
	}
}

func TestStorageConfigPost_MissingPath(t *testing.T) {
	setupStorageTestRoot(t)
	srv := newStorageConfigTestServer(t)

	body := `{"root":"/no/existe/onda-data"}`
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

func TestStorageConfigPost_ReadOnlyPath(t *testing.T) {
	root := setTestRoot(t, "storage-config-ro-")
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatalf("failed to chmod root read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	srv := newStorageConfigTestServer(t)
	body := fmt.Sprintf(`{"root":%q}`, root)
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

func TestStorageConfigPost_Traversal(t *testing.T) {
	setupStorageTestRoot(t)
	srv := newStorageConfigTestServer(t)

	body := `{"root":"/app/../etc"}`
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

func TestStorageConfigPost_Valid(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot get working directory: %v", err)
	}
	root, err := os.MkdirTemp(cwd, "storage-config-valid-")
	if err != nil {
		t.Fatalf("failed to create temp root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	settingsPath := filepath.Join(root, ".onda-settings.json")
	t.Setenv("ONDA_SETTINGS_FILE", settingsPath)
	t.Setenv("ONDA_DATA_DIR", "")

	srv := newStorageConfigTestServer(t)
	body := fmt.Sprintf(`{"root":%q}`, root)
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
	if respBody.CurrentRoot != root {
		t.Errorf("current_root: expected %q, got %q", root, respBody.CurrentRoot)
	}

	if got := dataRoot(); got != root {
		t.Errorf("dataRoot() = %q, want %q", got, root)
	}

	saved, err := loadStorageSettings()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	if saved.DataRoot != root {
		t.Errorf("persisted data_root: expected %q, got %q", root, saved.DataRoot)
	}
}
