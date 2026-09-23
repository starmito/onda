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

func TestHandleModelsUpload_UsesConfigStemsWithoutType(t *testing.T) {
	s, root := setupModelUploadTest(t)

	yaml := `training:
  instruments:
    - vocals
    - drums
    - bass
    - other
    - guitar
    - piano
  target_instrument: vocals
`
	files := []struct{ name string; data []byte }{
		{"SixStem.ckpt", []byte("weights")},
		{"SixStem.yaml", []byte(yaml)},
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
	if len(resp.Stems) != 6 {
		t.Errorf("stems = %v, want 6 stems", resp.Stems)
	}
	if resp.NumStems != 6 {
		t.Errorf("num_stems = %d, want 6", resp.NumStems)
	}
	if resp.Inferred {
		t.Error("inferred should be false when config provides stems")
	}

	modelDir := filepath.Join(root, "models", "VR_Models", "SixStem")
	manifest, ok := loadModelManifest(modelDir)
	if !ok {
		t.Fatalf("failed to load generated manifest")
	}
	if len(manifest.Stems.Stems) != 6 {
		t.Errorf("manifest stems = %v, want 6 stems", manifest.Stems.Stems)
	}
	if manifest.Inferred {
		t.Error("manifest inferred should be false")
	}
}

func TestHandleModelsUpload_ConfigOnlyUpdatesManifest(t *testing.T) {
	s, root := setupModelUploadTest(t)

	// Upload the weight alone first: the manifest is inferred.
	files := []struct{ name string; data []byte }{
		{"LaterConfig.ckpt", []byte("weights")},
	}
	body, contentType := buildModelUploadBodyFiles(t, files)
	uploadReq := httptest.NewRequest(http.MethodPost, "/api/models/upload", body)
	uploadReq.Header.Set("Content-Type", contentType)
	s.mux.ServeHTTP(httptest.NewRecorder(), uploadReq)

	modelDir := filepath.Join(root, "models", "VR_Models", "LaterConfig")
	manifest1, ok := loadModelManifest(modelDir)
	if !ok {
		t.Fatalf("initial manifest missing")
	}
	if !manifest1.Inferred {
		t.Error("initial manifest should be inferred")
	}

	// Upload the matching config later: the manifest must be updated.
	yaml := `training:
  instruments:
    - drums
    - bass
    - other
`
	cfgFiles := []struct{ name string; data []byte }{
		{"LaterConfig.yaml", []byte(yaml)},
	}
	body, contentType = buildModelUploadBodyFiles(t, cfgFiles)
	req := httptest.NewRequest(http.MethodPost, "/api/models/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for config-only upload, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp ModelUploadResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Stems) != 3 {
		t.Errorf("stems = %v, want [drums bass other]", resp.Stems)
	}
	if resp.Inferred {
		t.Error("inferred should be false after config update")
	}

	manifest2, ok := loadModelManifest(modelDir)
	if !ok {
		t.Fatalf("updated manifest missing")
	}
	if len(manifest2.Stems.Stems) != 3 {
		t.Errorf("updated manifest stems = %v, want 3 stems", manifest2.Stems.Stems)
	}
	if manifest2.Inferred {
		t.Error("updated manifest should not be inferred")
	}
}

func TestHandleModelsUpload_ConfigOnlyNoMatchRejects(t *testing.T) {
	s, _ := setupModelUploadTest(t)

	files := []struct{ name string; data []byte }{
		{"Orphan.yaml", []byte("model:\n  type: bs_roformer\n")},
	}
	body, contentType := buildModelUploadBodyFiles(t, files)
	req := httptest.NewRequest(http.MethodPost, "/api/models/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

// realSWRoformer6StemYAML is a stripped but structurally faithful excerpt of the
// BandSplit-Roformer SW 6-stem config (config_BandSplit-Roformer_SW_by-jarredou.yaml).
// It exercises the parser with !!python/tuple tags, target_instrument: null and the
// real 6-stem instrument list.
const realSWRoformer6StemYAML = `audio:
  chunk_size: 588800
  dim_f: 1024
  dim_t: 801
  hop_length: 441
  n_fft: 2048
  num_channels: 2
  sample_rate: 44100
  min_mean_abs: 0.000

model:
  dim: 256
  depth: 12
  stereo: true
  num_stems: 6
  time_transformer_depth: 1
  freq_transformer_depth: 1
  linear_transformer_depth: 0
  freqs_per_bands: !!python/tuple
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 2
    - 4
    - 4
    - 4
    - 4
    - 4
    - 4
    - 4
    - 4
    - 4
    - 4
    - 4
    - 4
    - 12
    - 12
    - 12
    - 12
    - 12
    - 12
    - 12
    - 12
    - 24
    - 24
    - 24
    - 24
    - 24
    - 24
    - 24
    - 24
    - 48
    - 48
    - 48
    - 48
    - 48
    - 48
    - 48
    - 48
    - 128
    - 129
  dim_head: 64
  heads: 8
  attn_dropout: 0.1
  ff_dropout: 0.1
  flash_attn: true
  dim_freqs_in: 1025
  stft_n_fft: 2048
  stft_hop_length: 512
  stft_win_length: 2048
  stft_normalized: false
  mask_estimator_depth: 2
  multi_stft_resolution_loss_weight: 1.0
  multi_stft_resolutions_window_sizes: !!python/tuple
    - 4096
    - 2048
    - 1024
    - 512
    - 256
  multi_stft_hop_size: 147
  multi_stft_normalized: False
  mlp_expansion_factor: 4
  use_torch_checkpoint: False
  skip_connection: False

training:
  batch_size: 2
  gradient_accumulation_steps: 1
  grad_clip: 0
  instruments: ['bass', 'drums', 'other', 'vocals', 'guitar', 'piano']
  patience: 3
  reduce_factor: 0.95
  target_instrument: null
  num_epochs: 1000
  num_steps: 1000
  augmentation: false
  augmentation_type: simple1
  use_mp3_compress: false
  augmentation_mix: true
  augmentation_loudness: true
  augmentation_loudness_type: 1
  augmentation_loudness_min: 0.5
  augmentation_loudness_max: 1.5
  q: 0.95
  coarse_loss_clip: true
  ema_momentum: 0.999
  optimizer: adam
  lr: 1.0e-5
  other_fix: false
  use_amp: true

augmentations:
  enable: true
  loudness: true
  loudness_min: 0.5
  loudness_max: 1.5
  mixup: true
  mixup_probs: !!python/tuple
    - 0.2
    - 0.02
  mixup_loudness_min: 0.5
  mixup_loudness_max: 1.5

  all:
    channel_shuffle: 0.5
    random_inverse: 0.1
    random_polarity: 0.5

  vocals:
    pitch_shift: 0.1
  bass:
    pitch_shift: 0.1
  drums:
    pitch_shift: 0.1
  other:
    pitch_shift: 0.1

inference:
  batch_size: 1
  dim_t: 1101
  num_overlap: 2
  normalize: false
`

func TestHandleModelsUpload_RealSW6StemYAML(t *testing.T) {
	s, root := setupModelUploadTest(t)

	files := []struct{ name string; data []byte }{
		{"BS_Roformer_SW_6stem.ckpt", []byte("weights")},
		{"config_BandSplit-Roformer_SW_by-jarredou.yaml", []byte(realSWRoformer6StemYAML)},
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
	if resp.Name != "BS_Roformer_SW_6stem" {
		t.Errorf("name = %q, want BS_Roformer_SW_6stem", resp.Name)
	}
	if resp.DisplayName != "BS_Roformer_SW_6stem" {
		t.Errorf("display_name = %q, want BS_Roformer_SW_6stem", resp.DisplayName)
	}
	if resp.Type != "bs_roformer" {
		t.Errorf("type = %q, want bs_roformer", resp.Type)
	}
	if resp.Category != "Roformer" {
		t.Errorf("category = %q, want Roformer", resp.Category)
	}
	wantStems := []string{"bass", "drums", "other", "vocals", "guitar", "piano"}
	if len(resp.Stems) != len(wantStems) {
		t.Errorf("stems = %v, want %v", resp.Stems, wantStems)
	}
	for i, want := range wantStems {
		if resp.Stems[i] != want {
			t.Errorf("stems[%d] = %q, want %q", i, resp.Stems[i], want)
		}
	}
	if resp.NumStems != 6 {
		t.Errorf("num_stems = %d, want 6", resp.NumStems)
	}
	if resp.Inferred {
		t.Error("inferred should be false when config provides stems")
	}

	modelDir := filepath.Join(root, "models", "VR_Models", "BS_Roformer_SW_6stem")
	manifest, ok := loadModelManifest(modelDir)
	if !ok {
		t.Fatalf("failed to load generated manifest")
	}
	if manifest.Type != "bs_roformer" {
		t.Errorf("manifest type = %q, want bs_roformer", manifest.Type)
	}
	if len(manifest.Stems.Stems) != 6 {
		t.Errorf("manifest stems = %v, want 6 stems", manifest.Stems.Stems)
	}
	if manifest.Inferred {
		t.Error("manifest inferred should be false")
	}
	if manifest.Origin != "upload" {
		t.Errorf("manifest origin = %q, want upload", manifest.Origin)
	}
}

func TestHandleModelsUpload_ConfigOnlyUpdateRealSW6Stem(t *testing.T) {
	s, root := setupModelUploadTest(t)

	// First upload: weight alone -> inferred 2-stem manifest.
	files1 := []struct{ name string; data []byte }{
		{"BS_Roformer_SW_6stem.ckpt", []byte("weights")},
	}
	body, contentType := buildModelUploadBodyFiles(t, files1)
	req1 := httptest.NewRequest(http.MethodPost, "/api/models/upload", body)
	req1.Header.Set("Content-Type", contentType)
	s.mux.ServeHTTP(httptest.NewRecorder(), req1)

	modelDir := filepath.Join(root, "models", "VR_Models", "BS_Roformer_SW_6stem")
	manifest1, ok := loadModelManifest(modelDir)
	if !ok {
		t.Fatalf("initial manifest missing")
	}
	if !manifest1.Inferred {
		t.Error("initial manifest should be inferred")
	}
	if len(manifest1.Stems.Stems) != 2 {
		t.Errorf("initial manifest stems = %v, want 2 stems", manifest1.Stems.Stems)
	}

	// Second upload: weight + config together so the sidecar is stored under
	// the model directory. After this the manifest must no longer be inferred.
	files2 := []struct{ name string; data []byte }{
		{"BS_Roformer_SW_6stem.ckpt", []byte("weights")},
		{"config_BandSplit-Roformer_SW_by-jarredou.yaml", []byte(realSWRoformer6StemYAML)},
	}
	body, contentType = buildModelUploadBodyFiles(t, files2)
	req2 := httptest.NewRequest(http.MethodPost, "/api/models/upload", body)
	req2.Header.Set("Content-Type", contentType)
	s.mux.ServeHTTP(httptest.NewRecorder(), req2)

	manifest2, ok := loadModelManifest(modelDir)
	if !ok {
		t.Fatalf("updated manifest missing")
	}
	if len(manifest2.Stems.Stems) != 6 {
		t.Errorf("updated manifest stems = %v, want 6 stems", manifest2.Stems.Stems)
	}
	if manifest2.Inferred {
		t.Error("updated manifest should not be inferred after reupload with config")
	}

	// Third upload: only the sidecar config again. Because the config file is
	// already present in the model directory, the backend can still associate it
	// and regenerate the manifest from it.
	files3 := []struct{ name string; data []byte }{
		{"config_BandSplit-Roformer_SW_by-jarredou.yaml", []byte(realSWRoformer6StemYAML)},
	}
	body, contentType = buildModelUploadBodyFiles(t, files3)
	req3 := httptest.NewRequest(http.MethodPost, "/api/models/upload", body)
	req3.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req3)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for config-only upload, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp ModelUploadResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	wantStems := []string{"bass", "drums", "other", "vocals", "guitar", "piano"}
	if len(resp.Stems) != len(wantStems) {
		t.Errorf("resp.stems = %v, want %v", resp.Stems, wantStems)
	}
	if resp.Inferred {
		t.Error("inferred should be false after config-only reupload")
	}

	manifest3, ok := loadModelManifest(modelDir)
	if !ok {
		t.Fatalf("final manifest missing")
	}
	if len(manifest3.Stems.Stems) != 6 {
		t.Errorf("final manifest stems = %v, want 6 stems", manifest3.Stems.Stems)
	}
	if manifest3.Inferred {
		t.Error("final manifest should not be inferred")
	}
}

func TestHandleModelsUpload_ReuploadWithConfigUpdatesManifest(t *testing.T) {
	s, root := setupModelUploadTest(t)

	// First upload: weight alone -> inferred manifest.
	files1 := []struct{ name string; data []byte }{
		{"BS_Roformer_SW_6stem.ckpt", []byte("weights")},
	}
	body, contentType := buildModelUploadBodyFiles(t, files1)
	req1 := httptest.NewRequest(http.MethodPost, "/api/models/upload", body)
	req1.Header.Set("Content-Type", contentType)
	s.mux.ServeHTTP(httptest.NewRecorder(), req1)

	modelDir := filepath.Join(root, "models", "VR_Models", "BS_Roformer_SW_6stem")
	manifest1, ok := loadModelManifest(modelDir)
	if !ok {
		t.Fatalf("initial manifest missing")
	}
	if !manifest1.Inferred {
		t.Error("initial manifest should be inferred")
	}

	// Second upload: weight + config together -> manifest must be updated.
	files2 := []struct{ name string; data []byte }{
		{"BS_Roformer_SW_6stem.ckpt", []byte("weights")},
		{"config_BandSplit-Roformer_SW_by-jarredou.yaml", []byte(realSWRoformer6StemYAML)},
	}
	body, contentType = buildModelUploadBodyFiles(t, files2)
	req2 := httptest.NewRequest(http.MethodPost, "/api/models/upload", body)
	req2.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req2)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for reupload, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp ModelUploadResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Inferred {
		t.Error("inferred should be false after reupload with config")
	}
	if len(resp.Stems) != 6 {
		t.Errorf("resp.stems = %v, want 6 stems", resp.Stems)
	}

	manifest2, ok := loadModelManifest(modelDir)
	if !ok {
		t.Fatalf("updated manifest missing")
	}
	if len(manifest2.Stems.Stems) != 6 {
		t.Errorf("updated manifest stems = %v, want 6 stems", manifest2.Stems.Stems)
	}
	if manifest2.Inferred {
		t.Error("updated manifest should not be inferred")
	}
}
