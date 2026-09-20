package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestReadImageFile_PriorityAndFallback(t *testing.T) {
	appDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("ONDA_APP_DIR", appDir)
	t.Setenv("ONDA_DATA_DIR", dataDir)

	// Image path takes precedence.
	if err := os.WriteFile(filepath.Join(appDir, "hf_models.json"), []byte(`{"source":"app"}`), 0o644); err != nil {
		t.Fatalf("failed to write app file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "hf_models.json"), []byte(`{"source":"data"}`), 0o644); err != nil {
		t.Fatalf("failed to write data file: %v", err)
	}

	data, err := readImageFile("hf_models.json")
	if err != nil {
		t.Fatalf("readImageFile failed: %v", err)
	}
	if string(data) != `{"source":"app"}` {
		t.Errorf("expected app file, got %q", string(data))
	}

	// Fallback to data root when file is missing from image path.
	if err := os.Remove(filepath.Join(appDir, "hf_models.json")); err != nil {
		t.Fatalf("failed to remove app file: %v", err)
	}
	data, err = readImageFile("hf_models.json")
	if err != nil {
		t.Fatalf("readImageFile fallback failed: %v", err)
	}
	if string(data) != `{"source":"data"}` {
		t.Errorf("expected data file, got %q", string(data))
	}
}

func TestHandleModelsCatalogHF(t *testing.T) {
	setTestRoot(t, "hf-catalog-")

	// Write a minimal HF catalog into the app directory so the endpoint has
	// content to serve even without the real bundled file.
	appDir := t.TempDir()
	t.Setenv("ONDA_APP_DIR", appDir)
	catalog := map[string]any{
		"source": "hf",
		"repo":   "test/repo",
		"categories": map[string]any{
			"test-category": map[string]any{
				"parent": "test-parent",
				"models": []map[string]any{
					{"name": "model-a", "filename": "model-a.pth", "hf_path": "model-a.pth", "size_mb": 1.5},
				},
			},
		},
	}
	b, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("failed to marshal catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "hf_models.json"), b, 0o644); err != nil {
		t.Fatalf("failed to write hf_models.json: %v", err)
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/models/catalog/hf", s.handleModelsCatalogHF)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/models/catalog/hf")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["source"] != "hf" {
		t.Errorf("source = %v, want hf", result["source"])
	}
	if result["repo"] != "test/repo" {
		t.Errorf("repo = %v, want test/repo", result["repo"])
	}
}

func TestHandleModelsCatalogHF_NotFound(t *testing.T) {
	setTestRoot(t, "hf-catalog-missing-")

	// Ensure neither app dir nor data root has the file.
	appDir := t.TempDir()
	t.Setenv("ONDA_APP_DIR", appDir)

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/models/catalog/hf", s.handleModelsCatalogHF)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/models/catalog/hf")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
