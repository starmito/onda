package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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

// demucsHFCatalogURL is the HuggingFace Hub public API endpoint for models
// published by the official Demucs author.
var demucsHFCatalogURL = "https://huggingface.co/api/models?author=adefossez&limit=100"

// demucsOfflineModelNames is the fallback catalog when the HuggingFace Hub API
// is unreachable. It matches the model names accepted by the demucs CLI.
var demucsOfflineModelNames = []string{
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

// demucsListProvider returns the list of official Demucs model names exposed
// by the installed demucs package and a flag indicating whether the offline
// fallback was used. It is a variable so tests can substitute a mock
// implementation.
var demucsListProvider = queryDemucsModelNames

// queryDemucsModelNames asks the HuggingFace Hub for the official adefossez
// Demucs repos and maps them back to the model names accepted by the demucs
// CLI. If the Hub is unreachable it returns the curated offline list.
func queryDemucsModelNames() ([]string, bool, error) {
	names, err := fetchDemucsHFModelNames()
	if err != nil || len(names) == 0 {
		log.Printf("[models] demucs HF catalog unavailable (%v), using offline fallback", err)
		return demucsOfflineModelNames, true, nil
	}
	return names, false, nil
}

// fetchDemucsHFModelNames retrieves and parses the adefossez model list from
// the HuggingFace Hub public API.
func fetchDemucsHFModelNames() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, demucsHFCatalogURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("huggingface returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseDemucsHFModelNames(body)
}

// parseDemucsHFModelNames extracts usable Demucs model names from a HuggingFace
// Hub API response. It ignores unknown repos and deduplicates the result.
func parseDemucsHFModelNames(body []byte) ([]string, error) {
	var items []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	names := make([]string, 0, len(items))
	for _, it := range items {
		name, ok := demucsModelNameFromRepo(it.ID)
		if !ok {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	sort.Strings(names)
	return names, nil
}

// demucsModelNameFromRepo maps an adefossez/HuggingFace repo name to the model
// name accepted by the demucs CLI. It mirrors the mapping in demucs.hf.hf_repo_name.
func demucsModelNameFromRepo(repo string) (string, bool) {
	if !strings.HasPrefix(repo, "adefossez/") {
		return "", false
	}
	name := strings.TrimPrefix(repo, "adefossez/")
	switch {
	case name == "HTDemucs":
		return "htdemucs", true
	case strings.HasPrefix(name, "HTDemucs-"):
		return "htdemucs_" + strings.ToLower(name[len("HTDemucs-"):]), true
	case strings.HasPrefix(name, "Demucs-"):
		return strings.ToLower(name[len("Demucs-"):]), true
	}
	return "", false
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

	names, offline, err := demucsListProvider()
	if err != nil {
		log.Printf("[models] demucs catalog query failed: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "demucs catalog not available: " + err.Error(),
		})
		return
	}
	if offline {
		w.Header().Set("X-Demucs-Offline", "true")
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
