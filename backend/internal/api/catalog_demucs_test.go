package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleModelsCatalogDemucs(t *testing.T) {
	setTestRoot(t, "demucs-catalog-")

	origProvider := demucsListProvider
	t.Cleanup(func() { demucsListProvider = origProvider })
	demucsListProvider = func() ([]string, error) {
		return []string{"htdemucs", "htdemucs_ft", "mdx_extra"}, nil
	}

	// Mark htdemucs_ft as downloaded by creating its YAML marker.
	demucsDir := filepath.Join(modelsBasePath(), "Demucs_Models")
	if err := os.MkdirAll(demucsDir, 0o755); err != nil {
		t.Fatalf("failed to create Demucs_Models dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(demucsDir, "htdemucs_ft.yaml"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("failed to write marker yaml: %v", err)
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/models/catalog/demucs", s.handleModelsCatalogDemucs)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/models/catalog/demucs")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var entries []DemucsCatalogEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("demucs catalog is empty")
	}
	for _, e := range entries {
		t.Logf("demucs model: name=%s repo=%s downloaded=%v", e.Name, e.Repo, e.Downloaded)
	}

	repos := make(map[string]string)
	downloaded := make(map[string]bool)
	for _, e := range entries {
		repos[e.Name] = e.Repo
		downloaded[e.Name] = e.Downloaded
		if e.Repo == "" {
			t.Errorf("model %q has empty repo", e.Name)
		}
	}

	if got, want := repos["htdemucs"], "adefossez/HTDemucs"; got != want {
		t.Errorf("htdemucs repo = %q, want %q", got, want)
	}
	if got, want := repos["htdemucs_ft"], "adefossez/HTDemucs-ft"; got != want {
		t.Errorf("htdemucs_ft repo = %q, want %q", got, want)
	}
	if got, want := repos["mdx_extra"], "adefossez/Demucs-mdx_extra"; got != want {
		t.Errorf("mdx_extra repo = %q, want %q", got, want)
	}

	if downloaded["htdemucs"] {
		t.Error("htdemucs should not be reported as downloaded")
	}
	if !downloaded["htdemucs_ft"] {
		t.Error("htdemucs_ft should be reported as downloaded")
	}
	if downloaded["mdx_extra"] {
		t.Error("mdx_extra should not be reported as downloaded")
	}
}

func TestDemucsHFRepoName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"htdemucs", "adefossez/HTDemucs"},
		{"htdemucs_ft", "adefossez/HTDemucs-ft"},
		{"htdemucs_6s", "adefossez/HTDemucs-6s"},
		{"mdx", "adefossez/Demucs-mdx"},
		{"mdx_extra", "adefossez/Demucs-mdx_extra"},
		{"mdx_extra_q", "adefossez/Demucs-mdx_extra_q"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := demucsHFRepoName(tt.name); got != tt.want {
				t.Errorf("demucsHFRepoName(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestHandleModelsCatalogDemucs_ProviderError(t *testing.T) {
	setTestRoot(t, "demucs-catalog-err-")

	origProvider := demucsListProvider
	t.Cleanup(func() { demucsListProvider = origProvider })
	demucsListProvider = func() ([]string, error) {
		return nil, errTestDemucsUnavailable
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/models/catalog/demucs", s.handleModelsCatalogDemucs)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/models/catalog/demucs")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

var errTestDemucsUnavailable = errDemucsUnavailable{}

type errDemucsUnavailable struct{}

func (errDemucsUnavailable) Error() string { return "demucs not installed in test environment" }
