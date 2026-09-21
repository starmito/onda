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
	s.mux.HandleFunc("GET /api/models/uploads", s.handleModelsUploads)
	s.mux.HandleFunc("DELETE /api/models/{name}", s.handleDeleteModel)
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

func buildModelUploadBodyFiles(t *testing.T, files []struct{ name string; data []byte }) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, f := range files {
		part, err := writer.CreateFormFile("files", f.name)
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

func TestHandleModelsUpload_UsesConfigTypeAndStems(t *testing.T) {
	s, root := setupModelUploadTest(t)

	// A .ckpt whose filename would normally be inferred as bs_roformer + vocals/instrumental,
	// but the YAML declares mdx23c with three stems.
	yaml := `model:
  type: mdx23c
  num_stems: 3
training:
  instruments:
    - drums
    - bass
    - other
  target_instrument: drums
`
	files := []struct{ name string; data []byte }{
		{"My_Custom.ckpt", []byte("weights")},
		{"My_Custom.yaml", []byte(yaml)},
	}
	body, contentType := buildModelUploadBodyFiles(t, files)
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
	if resp.Type != "mdx23c" {
		t.Errorf("type = %q, want mdx23c", resp.Type)
	}
	if resp.Category != "MDX" {
		t.Errorf("category = %q, want MDX", resp.Category)
	}
	if len(resp.Stems) != 3 || resp.Stems[0] != "drums" || resp.Stems[1] != "bass" || resp.Stems[2] != "other" {
		t.Errorf("stems = %v, want [drums bass other]", resp.Stems)
	}
	if resp.NumStems != 3 {
		t.Errorf("num_stems = %d, want 3", resp.NumStems)
	}
	if resp.Inferred {
		t.Error("inferred should be false when config provides type and stems")
	}

	modelDir := filepath.Join(root, "models", "VR_Models", "My_Custom")
	manifest, ok := loadModelManifest(modelDir)
	if !ok {
		t.Fatalf("failed to load generated manifest")
	}
	if manifest.Type != "mdx23c" {
		t.Errorf("manifest type = %q, want mdx23c", manifest.Type)
	}
	if len(manifest.Stems.Stems) != 3 {
		t.Errorf("manifest stems = %v, want 3 stems", manifest.Stems.Stems)
	}
	if manifest.Inferred {
		t.Error("manifest inferred should be false")
	}
	if manifest.Origin != "upload" {
		t.Errorf("manifest origin = %q, want upload", manifest.Origin)
	}
}

func TestHandleModelsUpload_InferredFlagWhenNoConfig(t *testing.T) {
	s, _ := setupModelUploadTest(t)

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
	if !resp.Inferred {
		t.Error("inferred should be true when no sidecar config is provided")
	}
}

func TestHandleModelsUploadsListsUploadedModels(t *testing.T) {
	s, _ := setupModelUploadTest(t)

	files := []struct{ name string; data []byte }{
		{"Listed_Model.pth", []byte("weights")},
	}
	body, contentType := buildModelUploadBodyFiles(t, files)
	uploadReq := httptest.NewRequest(http.MethodPost, "/api/models/upload", body)
	uploadReq.Header.Set("Content-Type", contentType)
	s.mux.ServeHTTP(httptest.NewRecorder(), uploadReq)

	listReq := httptest.NewRequest(http.MethodGet, "/api/models/uploads", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, listReq)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var listResp ModelsUploadsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	if len(listResp.Models) != 1 {
		t.Fatalf("expected 1 uploaded model, got %d", len(listResp.Models))
	}
	if listResp.Models[0].Name != "Listed_Model" {
		t.Errorf("name = %q, want Listed_Model", listResp.Models[0].Name)
	}
	if listResp.Models[0].Origin != "upload" {
		t.Errorf("origin = %q, want upload", listResp.Models[0].Origin)
	}
}

func TestHandleModelsUpload_SavesConfigWithDifferentBaseName(t *testing.T) {
	s, root := setupModelUploadTest(t)

	// Backend saves all sidecar config files, even if their base name does not
	// match the weight file, so the manifest parser can find them.
	files := []struct{ name string; data []byte }{
		{"Weight.pth", []byte("weights")},
		{"config.yaml", []byte("model:\n  type: scnet\ntraining:\n  instruments:\n    - vocals\n")},
	}
	body, contentType := buildModelUploadBodyFiles(t, files)
	req := httptest.NewRequest(http.MethodPost, "/api/models/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	modelDir := filepath.Join(root, "models", "VR_Models", "Weight")
	if _, err := os.Stat(filepath.Join(modelDir, "config.yaml")); err != nil {
		t.Errorf("config not saved: %v", err)
	}
}

func TestHandleDeleteModel_CleansUploadedModelDirectory(t *testing.T) {
	s, root := setupModelUploadTest(t)

	files := []struct{ name string; data []byte }{
		{"ToDelete.ckpt", []byte("weights")},
		{"ToDelete.yaml", []byte("model:\n  type: mdx23c\ntraining:\n  instruments:\n    - vocals\n")},
	}
	body, contentType := buildModelUploadBodyFiles(t, files)
	uploadReq := httptest.NewRequest(http.MethodPost, "/api/models/upload", body)
	uploadReq.Header.Set("Content-Type", contentType)
	s.mux.ServeHTTP(httptest.NewRecorder(), uploadReq)

	modelDir := filepath.Join(root, "models", "VR_Models", "ToDelete")
	if _, err := os.Stat(modelDir); err != nil {
		t.Fatalf("uploaded model directory should exist: %v", err)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/models/ToDelete", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, delReq)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if _, err := os.Stat(modelDir); !os.IsNotExist(err) {
		t.Errorf("model directory %q should have been removed", modelDir)
	}
}
