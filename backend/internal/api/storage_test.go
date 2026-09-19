package api

import (
	"bytes"
	"encoding/json"
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
	root, err := os.MkdirTemp(".", "storage-test-")
	if err != nil {
		t.Fatalf("failed to create test root: %v", err)
	}
	t.Setenv("ONDA_ROOT", root)
	t.Cleanup(func() { os.RemoveAll(root) })

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
