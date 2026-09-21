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
)

func TestParseHFURL(t *testing.T) {
	tests := []struct {
		name            string
		url             string
		wantRepo        string
		wantBranch      string
		wantFile        string
		wantErrContains string
	}{
		{
			name:       "repo only",
			url:        "https://huggingface.co/KimberleyJSN/melbandroformer",
			wantRepo:   "KimberleyJSN/melbandroformer",
			wantBranch: "main",
		},
		{
			name:       "repo with trailing slash",
			url:        "https://huggingface.co/KimberleyJSN/melbandroformer/",
			wantRepo:   "KimberleyJSN/melbandroformer",
			wantBranch: "main",
		},
		{
			name:       "blob file",
			url:        "https://huggingface.co/KimberleyJSN/melbandroformer/blob/main/MelBandRoformer.ckpt",
			wantRepo:   "KimberleyJSN/melbandroformer",
			wantBranch: "main",
			wantFile:   "MelBandRoformer.ckpt",
		},
		{
			name:       "resolve file with query",
			url:        "https://huggingface.co/KimberleyJSN/melbandroformer/resolve/main/MelBandRoformer.ckpt?download=true",
			wantRepo:   "KimberleyJSN/melbandroformer",
			wantBranch: "main",
			wantFile:   "MelBandRoformer.ckpt",
		},
		{
			name:       "tree path",
			url:        "https://huggingface.co/org/repo/tree/dev/models/",
			wantRepo:   "org/repo",
			wantBranch: "dev",
			wantFile:   "models",
		},
		{
			name:            "empty url",
			url:             "",
			wantErrContains: "enlace vacío",
		},
		{
			name:            "other domain",
			url:             "https://example.com/foo/bar",
			wantErrContains: "no pertenece a HuggingFace",
		},
		{
			name:            "incomplete url",
			url:             "https://huggingface.co/org/",
			wantErrContains: "incompleto",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, branch, filePath, err := parseHFURL(tt.url)
			if tt.wantErrContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Fatalf("parseHFURL(%q) error = %v, want containing %q", tt.url, err, tt.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseHFURL(%q) unexpected error: %v", tt.url, err)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
			if branch != tt.wantBranch {
				t.Errorf("branch = %q, want %q", branch, tt.wantBranch)
			}
			if filePath != tt.wantFile {
				t.Errorf("filePath = %q, want %q", filePath, tt.wantFile)
			}
		})
	}
}

func TestDetectModelTypeByName(t *testing.T) {
	tests := []struct {
		filename string
		repo     string
		wantType string
	}{
		{"MelBandRoformer.ckpt", "KimberleyJSN/melbandroformer", "mel_band_roformer"},
		{"model.ckpt", "user/scnet-vocal", "scnet"},
		{"MDX23C-DrumSep.ckpt", "repo", "mdx23c"},
		{"htdemucs_ft.th", "facebook/demucs", "htdemucs"},
		{"BS_Roformer_Viperx.ckpt", "repo", "bs_roformer"},
		{"Kim_Vocal_2.onnx", "repo", "mdx23c"},
	}
	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got, ok := detectModelTypeByName(tt.repo, tt.filename)
			if !ok {
				t.Fatalf("detectModelTypeByName(%q, %q) = false, want true", tt.repo, tt.filename)
			}
			if got != tt.wantType {
				t.Errorf("detectModelTypeByName = %q, want %q", got, tt.wantType)
			}
		})
	}
}

func TestDetectModelTypeFromConfig(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		wantType   string
		wantReason string
	}{
		{
			name: "training.model_type",
			yaml: `
training:
  model_type: mel_band_roformer
`,
			wantType:   "mel_band_roformer",
			wantReason: "config",
		},
		{
			name: "bs_roformer via freqs_per_bands",
			yaml: `
model:
  freqs_per_bands: [1, 2, 3]
`,
			wantType:   "bs_roformer",
			wantReason: "claves_del_modelo",
		},
		{
			name: "mel_band via num_bands",
			yaml: `
model:
  num_bands: 60
`,
			wantType:   "mel_band_roformer",
			wantReason: "claves_del_modelo",
		},
		{
			name: "scnet via band_SR",
			yaml: `
model:
  band_SR: [0.2, 0.4]
`,
			wantType:   "scnet",
			wantReason: "claves_del_modelo",
		},
		{
			name: "mdx23c via num_scales",
			yaml: `
model:
  num_scales: 4
`,
			wantType:   "mdx23c",
			wantReason: "claves_del_modelo",
		},
		{
			name: "pope variant stays bs_roformer",
			yaml: `
model:
  use_pope: true
`,
			wantType:   "bs_roformer",
			wantReason: "claves_del_modelo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason, ok := detectModelTypeFromConfig([]byte(tt.yaml))
			if !ok {
				t.Fatal("expected detection")
			}
			if got != tt.wantType || reason != tt.wantReason {
				t.Errorf("got %q/%q, want %q/%q", got, reason, tt.wantType, tt.wantReason)
			}
		})
	}
}

// newResolveTestServer returns a minimal server with only the resolve handler.
func newResolveTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/models/resolve", s.handleModelsResolve)
	return s, httptest.NewServer(s.mux)
}

func postResolve(t *testing.T, srv *httptest.Server, url string) (*ResolveHFResponse, *http.Response) {
	t.Helper()
	body, _ := json.Marshal(ResolveHFRequest{URL: url})
	resp, err := http.Post(srv.URL+"/api/models/resolve", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	var result ResolveHFResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return &result, resp
}

func TestResolveHFEndpoint_KimberleyJSN(t *testing.T) {
	setTestRoot(t, "resolve-hf-")
	_, srv := newResolveTestServer(t)
	defer srv.Close()

	result, resp := postResolve(t, srv, "https://huggingface.co/KimberleyJSN/melbandroformer")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; error = %s", resp.StatusCode, result.Error)
	}
	if result.Repo != "KimberleyJSN/melbandroformer" {
		t.Errorf("repo = %q, want KimberleyJSN/melbandroformer", result.Repo)
	}
	if result.Tipo != "mel_band_roformer" {
		t.Errorf("tipo = %q, want mel_band_roformer", result.Tipo)
	}
	if result.Razon != "nombre" {
		t.Errorf("razon = %q, want nombre", result.Razon)
	}
	if len(result.Candidatos) == 0 {
		t.Fatal("expected at least one candidate")
	}
	if result.Candidatos[0].Peso.Path != "MelBandRoformer.ckpt" {
		t.Errorf("candidate weight = %q, want MelBandRoformer.ckpt", result.Candidatos[0].Peso.Path)
	}
}

func TestResolveHFEndpoint_ViperxDeleted(t *testing.T) {
	setTestRoot(t, "resolve-hf-")
	_, srv := newResolveTestServer(t)
	defer srv.Close()

	result, resp := postResolve(t, srv, "https://huggingface.co/viperx/BS-Roformer-Viperx")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if result.Error == "" {
		t.Fatal("expected an error message")
	}
	lower := strings.ToLower(result.Error)
	if !strings.Contains(lower, "no encontrado") && !strings.Contains(lower, "no accesible") {
		t.Errorf("error message should mention not found or not accessible, got %q", result.Error)
	}
}

func TestResolveHFEndpoint_AkhaliqDemucs(t *testing.T) {
	setTestRoot(t, "resolve-hf-")
	_, srv := newResolveTestServer(t)
	defer srv.Close()

	result, resp := postResolve(t, srv, "https://huggingface.co/akhaliq/Demucs")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; error = %s", resp.StatusCode, result.Error)
	}
	if result.Tipo != "htdemucs" {
		t.Errorf("tipo = %q, want htdemucs", result.Tipo)
	}
	if len(result.Candidatos) == 0 {
		t.Fatal("expected candidates")
	}
}

func TestResolveHFEndpoint_AnameSCNet(t *testing.T) {
	setTestRoot(t, "resolve-hf-")
	_, srv := newResolveTestServer(t)
	defer srv.Close()

	result, resp := postResolve(t, srv, "https://huggingface.co/Aname-Tommy/Huge-SCNet-4stems")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; error = %s", resp.StatusCode, result.Error)
	}
	if result.Tipo != "scnet" {
		t.Errorf("tipo = %q, want scnet", result.Tipo)
	}
	if result.Razon != "claves_del_modelo" {
		t.Errorf("razon = %q, want claves_del_modelo", result.Razon)
	}
	if len(result.Candidatos) == 0 {
		t.Fatal("expected candidates")
	}
	if result.Candidatos[0].Config == nil {
		t.Fatal("expected config for SCNet candidate")
	}
}

func TestResolveHFEndpoint_InvalidDomain(t *testing.T) {
	setTestRoot(t, "resolve-hf-")
	_, srv := newResolveTestServer(t)
	defer srv.Close()

	result, resp := postResolve(t, srv, "https://example.com/foo/bar")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if result.Error == "" {
		t.Fatal("expected error")
	}
}

func TestResolveHFEndpoint_EmptyURL(t *testing.T) {
	setTestRoot(t, "resolve-hf-")
	_, srv := newResolveTestServer(t)
	defer srv.Close()

	result, resp := postResolve(t, srv, "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if !strings.Contains(strings.ToLower(result.Error), "enlace vacío") {
		t.Errorf("expected empty link error, got %q", result.Error)
	}
}

func TestResolveHFEndpoint_AlreadyDownloaded(t *testing.T) {
	root := setTestRoot(t, "resolve-hf-")
	_, srv := newResolveTestServer(t)
	defer srv.Close()

	// Pre-create the weight file in the expected VR_Models directory.
	modelDir := filepath.Join(root, "models", "VR_Models")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "MelBandRoformer.ckpt"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("failed to create dummy weight: %v", err)
	}

	result, resp := postResolve(t, srv, "https://huggingface.co/KimberleyJSN/melbandroformer")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; error = %s", resp.StatusCode, result.Error)
	}
	if !result.YaDescargado {
		t.Error("ya_descargado = false, want true")
	}
}
