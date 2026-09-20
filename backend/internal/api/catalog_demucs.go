package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DemucsCatalogEntry describes an official Demucs model available in the
// installed demucs package.
type DemucsCatalogEntry struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Repo        string `json:"repo"`
	Downloaded  bool   `json:"downloaded"`
	Downloads   int64  `json:"downloads"`
	Likes       int64  `json:"likes"`
	Source      string `json:"source"`
}

// demucsListProvider returns the list of official Demucs model names exposed
// by the installed demucs package. It is a variable so tests can substitute a
// mock implementation.
var demucsListProvider = queryDemucsModelNames

// queryDemucsModelNames invokes the installed demucs API to obtain the list of
// single and bag model names. It returns a deduplicated, sorted slice.
func queryDemucsModelNames() ([]string, error) {
	script := `import json, sys
try:
    import demucs.api
    models = demucs.api.list_models()
    names = set(models.get("single", {}).keys())
    names.update(models.get("bag", {}).keys())
    print(json.dumps(sorted(names)))
except Exception as e:
    print(json.dumps({"error": str(e)}))
    sys.exit(1)
`
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("demucs model query failed: %v: %s", err, strings.TrimSpace(string(out)))
	}

	var raw []json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse demucs model list: %w", err)
	}

	// The script returns either a plain list of strings or {"error": ...}
	if len(raw) == 0 {
		return nil, fmt.Errorf("demucs returned empty model list")
	}
	var names []string
	for _, r := range raw {
		var s string
		if err := json.Unmarshal(r, &s); err == nil {
			names = append(names, s)
			continue
		}
		var obj map[string]string
		if err := json.Unmarshal(r, &obj); err == nil {
			if msg, ok := obj["error"]; ok {
				return nil, fmt.Errorf("demucs model query error: %s", msg)
			}
		}
		return nil, fmt.Errorf("unexpected entry in demucs model list: %s", string(r))
	}
	return names, nil
}

// demucsHFRepoName maps a demucs model name to its official HuggingFace repo,
// mirroring demucs.hf.hf_repo_name.
func demucsHFRepoName(name string) string {
	repo := ""
	switch {
	case name == "htdemucs":
		repo = "HTDemucs"
	case strings.HasPrefix(name, "htdemucs_"):
		repo = "HTDemucs-" + name[len("htdemucs_"):]
	default:
		repo = "Demucs-" + name
	}
	return "adefossez/" + repo
}

// demucsDisplayName returns a human-friendly name for a demucs model.
func demucsDisplayName(name string) string {
	switch name {
	case "htdemucs":
		return "HTDemucs"
	case "htdemucs_ft":
		return "HTDemucs FT"
	case "htdemucs_6s":
		return "HTDemucs 6s"
	case "hdemucs_mmi":
		return "Hybrid Demucs MMI"
	}
	n := strings.ReplaceAll(name, "_", " ")
	return strings.Title(n)
}

// isDemucsModelDownloaded reports whether the official Demucs model appears to
// be present in Onda's Demucs_Models directory. Demucs repos downloaded via the
// backend contain a YAML file named after the model.
func isDemucsModelDownloaded(name string) bool {
	yamlPath := filepath.Join(modelsBasePath(), "Demucs_Models", name+".yaml")
	if _, err := os.Stat(yamlPath); err == nil {
		return true
	}
	return false
}

// fetchDemucsHubStats queries the HuggingFace Hub public API for likes and
// downloads of a single repo. Errors are swallowed and zeros returned so that
// the catalog endpoint never fails because of HF API issues.
func fetchDemucsHubStats(repo string, timeout time.Duration) (downloads, likes int64) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	url := "https://huggingface.co/api/models/" + repo
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0
	}

	var info struct {
		Downloads int64 `json:"downloads"`
		Likes     int64 `json:"likes"`
	}
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &info)
	return info.Downloads, info.Likes
}

// handleModelsCatalogDemucs returns the official Demucs model catalog from the
// installed demucs package, augmented with download status and optional Hub
// statistics.
// GET /api/models/catalog/demucs
func (s *Server) handleModelsCatalogDemucs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	names, err := demucsListProvider()
	if err != nil {
		log.Printf("[models] demucs catalog query failed: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "demucs catalog not available: " + err.Error(),
		})
		return
	}

	// Fetch Hub stats concurrently with a tight timeout so the endpoint stays
	// fast even without network access.
	type stats struct {
		downloads int64
		likes     int64
	}
	statsMap := make(map[string]stats, len(names))
	var statsMu sync.Mutex
	var wg sync.WaitGroup
	for _, name := range names {
		repo := demucsHFRepoName(name)
		wg.Add(1)
		go func(n, r string) {
			defer wg.Done()
			d, l := fetchDemucsHubStats(r, 2*time.Second)
			statsMu.Lock()
			statsMap[n] = stats{downloads: d, likes: l}
			statsMu.Unlock()
		}(name, repo)
	}
	wg.Wait()

	entries := make([]DemucsCatalogEntry, 0, len(names))
	for _, name := range names {
		repo := demucsHFRepoName(name)
		s := statsMap[name]
		entries = append(entries, DemucsCatalogEntry{
			Name:        name,
			DisplayName: demucsDisplayName(name),
			Repo:        repo,
			Downloaded:  isDemucsModelDownloaded(name),
			Downloads:   s.downloads,
			Likes:       s.likes,
			Source:      "demucs",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(entries)
}
