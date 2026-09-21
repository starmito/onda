package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTargetDirForHFModel(t *testing.T) {
	root := setTestRoot(t, "download-hf-target-")
	_ = root

	tests := []struct {
		name       string
		modelType  string
		nombre     string
		weightPath string
		wantSuffix string
	}{
		{
			name:       "mel_band_roformer goes to VR_Models subdir",
			modelType:  "mel_band_roformer",
			nombre:     "MelBandRoformer",
			weightPath: "MelBandRoformer.ckpt",
			wantSuffix: filepath.Join("VR_Models", "MelBandRoformer"),
		},
		{
			name:       "bs_roformer goes to VR_Models subdir",
			modelType:  "bs_roformer",
			nombre:     "BS Roformer Viperx",
			weightPath: "model.ckpt",
			wantSuffix: filepath.Join("VR_Models", "BS_Roformer_Viperx"),
		},
		{
			name:       "mdx23c goes to MDX_Net_Models subdir",
			modelType:  "mdx23c",
			nombre:     "Kim Vocal 2",
			weightPath: "Kim_Vocal_2.onnx",
			wantSuffix: filepath.Join("MDX_Net_Models", "Kim_Vocal_2"),
		},
		{
			name:       "htdemucs preserves HF Demucs_v4 structure",
			modelType:  "htdemucs",
			nombre:     "htdemucs_ft",
			weightPath: "models/Demucs/Demucs_v4/htdemucs_ft.th",
			wantSuffix: filepath.Join("Demucs_Models", "models", "Demucs", "Demucs_v4"),
		},
		{
			name:       "htdemucs without repo path defaults to Demucs_v4",
			modelType:  "htdemucs",
			nombre:     "MyDemucs",
			weightPath: "model.th",
			wantSuffix: filepath.Join("Demucs_Models", "models", "Demucs", "Demucs_v4"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := targetDirForHFModel(tt.modelType, tt.nombre, tt.weightPath)
			if !strings.HasSuffix(got, tt.wantSuffix) {
				t.Errorf("targetDirForHFModel() = %q, want suffix %q", got, tt.wantSuffix)
			}
		})
	}
}

func TestSanitizeModelConfig(t *testing.T) {
	tests := []struct {
		name      string
		modelType string
		yaml      string
		wantDimT  int
		wantOverlap float64
		wantParsed bool
	}{
		{
			name:       "valid inference block",
			modelType:  "mel_band_roformer",
			yaml:       "inference:\n  dim_t: 256\n  num_overlap: 4\n  batch_size: 1\n  chunk_size: 120\n",
			wantDimT:   256,
			wantOverlap: 0.25,
			wantParsed: true,
		},
		{
			name:       "training dim_t is sanitized",
			modelType:  "bs_roformer",
			yaml:       "inference:\n  dim_t: 3105\n  num_overlap: 4\n",
			wantDimT:   512, // default for bs_roformer
			wantOverlap: 0.25,
			wantParsed: true,
		},
		{
			name:       "garbage integer value is cleaned",
			modelType:  "mel_band_roformer",
			yaml:       "inference:\n  dim_t: \"35}\"\n  num_overlap: \"4\"\n",
			wantDimT:   35,
			wantOverlap: 0.25,
			wantParsed: true,
		},
		{
			name:       "demucs block parsed",
			modelType:  "htdemucs",
			yaml:       "demucs:\n  shifts: 2\n  segment: 7\n  jobs: 4\n",
			wantDimT:   0,
			wantOverlap: 0.25,
			wantParsed: true,
		},
		{
			name:       "unparseable yaml returns defaults",
			modelType:  "mdx23c",
			yaml:       "inference: [not a mapping",
			wantDimT:   256,
			wantOverlap: 0.25,
			wantParsed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, parsed := sanitizeModelConfig([]byte(tt.yaml), tt.modelType)
			if parsed != tt.wantParsed {
				t.Errorf("parsed = %v, want %v", parsed, tt.wantParsed)
			}
			if cfg.SegmentSize != tt.wantDimT {
				t.Errorf("SegmentSize = %d, want %d", cfg.SegmentSize, tt.wantDimT)
			}
			if cfg.Overlap != tt.wantOverlap {
				t.Errorf("Overlap = %v, want %v", cfg.Overlap, tt.wantOverlap)
			}
		})
	}
}

func TestWriteModelConfigJSON(t *testing.T) {
	root := setTestRoot(t, "download-hf-json-")
	_ = root

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		ChunkSize:   120,
		BatchSize:   1,
		Device:      "cuda",
		Shifts:      1,
	}
	if err := writeModelConfigJSON("TestModel", cfg); err != nil {
		t.Fatalf("writeModelConfigJSON failed: %v", err)
	}

	path := uvrModelConfigJSONPath("TestModel")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written JSON: %v", err)
	}
	if !strings.Contains(string(data), `"segment_size": 256`) {
		t.Errorf("JSON missing expected segment_size: %s", string(data))
	}
	if !strings.Contains(string(data), `"dim_t": 256`) {
		t.Errorf("JSON missing expected dim_t: %s", string(data))
	}
}

func TestDownloadHFEndpoint_AcceptsJob(t *testing.T) {
	root := setTestRoot(t, "download-hf-accept-")
	_ = root

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/models/download-hf", s.handleModelsDownloadHF)
	srv := httptest.NewServer(s.mux)
	defer srv.Close()

	req := DownloadHFRequest{
		Repo:   "test/repo",
		Nombre: "TestModel",
		Tipo:   "bs_roformer",
		Candidato: ResolveHFCandidate{
			Peso: ResolveHFFile{
				Path: "model.ckpt",
				Size: 1024,
				URL:  srv.URL + "/model.ckpt",
			},
			Config: &ResolveHFFile{
				Path: "model.yaml",
				Size: 64,
				URL:  srv.URL + "/model.yaml",
			},
		},
	}

	body, _ := json.Marshal(req)
	resp, err := http.Post(srv.URL+"/api/models/download-hf", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}

	var status DownloadStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if status.Status != "downloading" {
		t.Errorf("status = %q, want downloading", status.Status)
	}
	if status.Repo != "test/repo" {
		t.Errorf("repo = %q, want test/repo", status.Repo)
	}
}

// TestDownloadHFEndpoint_RealKimVocal2 downloads a real small ONNX model from
// HuggingFace and verifies it lands in the correct MDX_Net_Models folder, is
// discoverable by listModels, and has a generated JSON config.
func TestDownloadHFEndpoint_RealKimVocal2(t *testing.T) {
	root := setTestRoot(t, "download-hf-real-")
	_ = root

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/models/download-hf", s.handleModelsDownloadHF)
	s.mux.HandleFunc("GET /api/models/download/status", s.handleModelsDownloadStatus)
	s.mux.HandleFunc("GET /api/models/list", s.handleModelsList)
	srv := httptest.NewServer(s.mux)
	defer srv.Close()

	repo := "seanghay/uvr_models"
	weightURL := "https://huggingface.co/seanghay/uvr_models/resolve/main/Kim_Vocal_2.onnx"
	configURL := "https://huggingface.co/seanghay/uvr_models/resolve/main/htdemucs_ft.yaml"

	req := DownloadHFRequest{
		Repo:   repo,
		Branch: "main",
		Nombre: "Kim_Vocal_2",
		Tipo:   "mdx23c",
		Candidato: ResolveHFCandidate{
			Peso: ResolveHFFile{
				Path: "Kim_Vocal_2.onnx",
				Size: 66759214,
				URL:  weightURL,
			},
			Config: &ResolveHFFile{
				Path: "htdemucs_ft.yaml",
				Size: 149,
				URL:  configURL,
			},
		},
	}

	body, _ := json.Marshal(req)
	resp, err := http.Post(srv.URL+"/api/models/download-hf", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("download not accepted: status=%d", resp.StatusCode)
	}

	// Poll until the download finishes or times out.
	key := hfDownloadKey(repo, req.Candidato.Peso.Path)
	var finalStatus *DownloadStatus
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		resp, err := http.Get(srv.URL + "/api/models/download/status?repo=" + key)
		if err != nil {
			t.Fatalf("status request failed: %v", err)
		}
		var st DownloadStatus
		if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
			resp.Body.Close()
			t.Fatalf("failed to decode status: %v", err)
		}
		resp.Body.Close()
		finalStatus = &st
		if st.Status == "done" || st.Status == "error" {
			break
		}
	}

	if finalStatus == nil || finalStatus.Status != "done" {
		msg := ""
		if finalStatus != nil {
			msg = finalStatus.Error
		}
		t.Fatalf("download did not finish successfully: status=%+v error=%s", finalStatus, msg)
	}
	if finalStatus.SpeedBytesPerSec <= 0 {
		t.Logf("warning: final speed was %f bytes/s", finalStatus.SpeedBytesPerSec)
	}

	// Verify weight landed in MDX_Net_Models/Kim_Vocal_2/Kim_Vocal_2.onnx
	weightPath := filepath.Join(modelsBasePath(), "MDX_Net_Models", "Kim_Vocal_2", "Kim_Vocal_2.onnx")
	info, err := os.Stat(weightPath)
	if err != nil {
		t.Fatalf("weight not found at %s: %v", weightPath, err)
	}
	if info.Size() == 0 {
		t.Fatalf("weight file is empty")
	}
	t.Logf("real download: name=Kim_Vocal_2 size=%d bytes speed=%.0f bytes/s path=%s", info.Size(), finalStatus.SpeedBytesPerSec, weightPath)

	// Verify model appears in the listing without restart.
	listResp, err := http.Get(srv.URL + "/api/models/list")
	if err != nil {
		t.Fatalf("list request failed: %v", err)
	}
	var list ModelsListResponse
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		listResp.Body.Close()
		t.Fatalf("failed to decode list: %v", err)
	}
	listResp.Body.Close()

	found := false
	for _, m := range list.Models {
		if m.Name == "Kim_Vocal_2" {
			found = true
			if m.Category != "MDXNet" {
				t.Errorf("category = %q, want MDXNet", m.Category)
			}
			break
		}
	}
	if !found {
		t.Fatalf("Kim_Vocal_2 not found in model list")
	}

	// Verify JSON config was created.
	jsonPath := uvrModelConfigJSONPath("Kim_Vocal_2")
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("model config JSON not found at %s: %v", jsonPath, err)
	}
}
