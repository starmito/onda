package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func setupModelUploadTest(t *testing.T) (*Server, string) {
	t.Helper()
	root := setTestRoot(t, "models-upload-")
	for _, dir := range []string{"output", "input", "models"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/models/upload", s.handleModelsUpload)
	return s, root
}

func buildModelUploadBody(t *testing.T, files []struct{ name string; data []byte }) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, f := range files {
		part, err := writer.CreateFormFile("file", f.name)
		if err != nil {
			t.Fatalf("failed to create form file: %v", err)
		}
		if _, err := io.Copy(part, bytes.NewReader(f.data)); err != nil {
			t.Fatalf("failed to write form file: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}

func TestHandleModelsUpload_CreatesManifestAndModel(t *testing.T) {
	s, root := setupModelUploadTest(t)

	body, contentType := buildUploadBody(t, "My_Roformer.ckpt", []byte("weights"))
	req := httptest.NewRequest(http.MethodPost, "/api/models/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp ModelUploadResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Name != "My_Roformer" {
		t.Errorf("name = %q, want My_Roformer", resp.Name)
	}
	if resp.Type != "bs_roformer" {
		t.Errorf("type = %q, want bs_roformer", resp.Type)
	}
	if resp.Category != "Roformer" {
		t.Errorf("category = %q, want Roformer", resp.Category)
	}
	if len(resp.Stems) != 2 || resp.Stems[0] != "vocals" || resp.Stems[1] != "instrumental" {
		t.Errorf("stems = %v, want [vocals instrumental]", resp.Stems)
	}
	if resp.ManifestMissing {
		t.Error("manifest_missing should be false")
	}

	modelDir := filepath.Join(root, "models", "VR_Models", "My_Roformer")
	if _, err := os.Stat(filepath.Join(modelDir, "My_Roformer.ckpt")); err != nil {
		t.Errorf("model file not saved: %v", err)
	}
	manifestPath := filepath.Join(modelDir, "model.manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("manifest not generated: %v", err)
	}
	manifest, ok := loadModelManifest(modelDir)
	if !ok {
		t.Fatalf("failed to load generated manifest")
	}
	if manifest.Type != "bs_roformer" {
		t.Errorf("manifest type = %q, want bs_roformer", manifest.Type)
	}
	if len(manifest.Flags) == 0 {
		t.Error("manifest flags should not be empty")
	}
}

func TestHandleModelsUpload_SavesSidecarConfig(t *testing.T) {
	s, root := setupModelUploadTest(t)

	files := []struct{ name string; data []byte }{
		{"Test_Demucs.pth", []byte("weights")},
		{"Test_Demucs.yaml", []byte("inference:\n  dim_t: 256\n")},
	}
	body, contentType := buildModelUploadBody(t, files)
	req := httptest.NewRequest(http.MethodPost, "/api/models/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	modelDir := filepath.Join(root, "models", "Demucs_Models", "Test_Demucs")
	if _, err := os.Stat(filepath.Join(modelDir, "Test_Demucs.pth")); err != nil {
		t.Errorf("model file not saved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(modelDir, "Test_Demucs.yaml")); err != nil {
		t.Errorf("sidecar yaml not saved: %v", err)
	}
}

func TestHandleModelsUpload_RejectsUnsupportedExtension(t *testing.T) {
	s, _ := setupModelUploadTest(t)

	body, contentType := buildUploadBody(t, "model.txt", []byte("not a model"))
	req := httptest.NewRequest(http.MethodPost, "/api/models/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleModelsUpload_RejectsMissingFile(t *testing.T) {
	s, _ := setupModelUploadTest(t)

	body, contentType := buildUploadBody(t, "", []byte(""))
	req := httptest.NewRequest(http.MethodPost, "/api/models/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}
