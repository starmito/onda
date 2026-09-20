package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"testing"
)

func TestHandleModelsCatalogDemucs(t *testing.T) {
	setTestRoot(t, "demucs-catalog-")

	origProvider := demucsListProvider
	t.Cleanup(func() { demucsListProvider = origProvider })
	demucsListProvider = func() ([]string, bool, error) {
		return []string{"htdemucs", "htdemucs_ft", "mdx_extra"}, false, nil
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

func TestDemucsModelNameFromRepo(t *testing.T) {
	tests := []struct {
		repo string
		want string
		ok   bool
	}{
		{"adefossez/HTDemucs", "htdemucs", true},
		{"adefossez/HTDemucs-ft", "htdemucs_ft", true},
		{"adefossez/HTDemucs-6s", "htdemucs_6s", true},
		{"adefossez/Demucs-mdx", "mdx", true},
		{"adefossez/Demucs-mdx_extra", "mdx_extra", true},
		{"adefossez/Demucs-mdx_extra_q", "mdx_extra_q", true},
		{"adefossez/Demucs-hdemucs_mmi", "hdemucs_mmi", true},
		{"adefossez/Demucs-repro_mdx_a", "repro_mdx_a", true},
		{"adefossez/Unknown", "", false},
		{"facebook/demucs", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			got, ok := demucsModelNameFromRepo(tt.repo)
			if got != tt.want || ok != tt.ok {
				t.Errorf("demucsModelNameFromRepo(%q) = (%q, %v), want (%q, %v)", tt.repo, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestParseDemucsHFModelNames(t *testing.T) {
	body := []byte(`[
		{"id":"adefossez/HTDemucs"},
		{"id":"adefossez/HTDemucs-ft"},
		{"id":"adefossez/HTDemucs-6s"},
		{"id":"adefossez/Demucs-mdx"},
		{"id":"adefossez/Demucs-mdx_extra"},
		{"id":"adefossez/Demucs-mdx_q"},
		{"id":"adefossez/Demucs-mdx_extra_q"},
		{"id":"adefossez/Demucs-hdemucs_mmi"},
		{"id":"adefossez/Demucs-repro_mdx_a"},
		{"id":"adefossez/Demucs-repro_mdx_a_hybrid_only"},
		{"id":"adefossez/Demucs-repro_mdx_a_time_only"},
		{"id":"adefossez/HTDemucs"}
	]`)

	names, err := parseDemucsHFModelNames(body)
	if err != nil {
		t.Fatalf("parseDemucsHFModelNames failed: %v", err)
	}

	want := []string{
		"hdemucs_mmi",
		"htdemucs",
		"htdemucs_6s",
		"htdemucs_ft",
		"mdx",
		"mdx_extra",
		"mdx_extra_q",
		"mdx_q",
		"repro_mdx_a",
		"repro_mdx_a_hybrid_only",
		"repro_mdx_a_time_only",
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}

	hashRe := regexp.MustCompile("^[0-9a-f]{8}$")
	for _, n := range names {
		if hashRe.MatchString(n) {
			t.Errorf("catalog contains hash-like entry %q", n)
		}
	}
}

func TestQueryDemucsModelNames_HF(t *testing.T) {
	body := `[{"id":"adefossez/HTDemucs"},{"id":"adefossez/HTDemucs-ft"},{"id":"adefossez/Demucs-mdx"}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	defer server.Close()

	origURL := demucsHFCatalogURL
	t.Cleanup(func() { demucsHFCatalogURL = origURL })
	demucsHFCatalogURL = server.URL

	names, offline, err := queryDemucsModelNames()
	if err != nil {
		t.Fatalf("queryDemucsModelNames failed: %v", err)
	}
	if offline {
		t.Error("expected online catalog, got offline fallback")
	}

	want := []string{"htdemucs", "htdemucs_ft", "mdx"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

func TestQueryDemucsModelNames_Fallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	origURL := demucsHFCatalogURL
	t.Cleanup(func() { demucsHFCatalogURL = origURL })
	demucsHFCatalogURL = server.URL

	names, offline, err := queryDemucsModelNames()
	if err != nil {
		t.Fatalf("queryDemucsModelNames failed: %v", err)
	}
	if !offline {
		t.Error("expected offline fallback")
	}

	want := make([]string, len(demucsOfflineModelNames))
	copy(want, demucsOfflineModelNames)
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Errorf("fallback names = %v, want %v", names, want)
	}
}

func TestHandleModelsCatalogDemucs_ProviderError(t *testing.T) {
	setTestRoot(t, "demucs-catalog-err-")

	origProvider := demucsListProvider
	t.Cleanup(func() { demucsListProvider = origProvider })
	demucsListProvider = func() ([]string, bool, error) {
		return nil, false, errTestDemucsUnavailable
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
