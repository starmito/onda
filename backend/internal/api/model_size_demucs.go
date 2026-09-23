package api

import (
	"os"
	"path/filepath"
	"strings"
)

// hfHubCacheDir returns the HuggingFace Hub cache directory used by the
// demucs package when downloading official models. It honours the standard
// HF_HOME / HF_HUB_CACHE environment variables and falls back to
// $HOME/.cache/huggingface/hub.
func hfHubCacheDir() string {
	if d := os.Getenv("HF_HUB_CACHE"); d != "" {
		return d
	}
	if d := os.Getenv("HUGGINGFACE_HUB_CACHE"); d != "" {
		return d
	}
	if d := os.Getenv("HF_HOME"); d != "" {
		return filepath.Join(d, "hub")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".cache", "huggingface", "hub")
}

// demucsModelWeightBytes returns the total on-disk size of the actual weight
// files for an official Demucs PyTorch model. Demucs downloads the weights as
// .safetensors files from HuggingFace (one per stem) into the HF Hub cache, but
// the on-disk marker in Demucs_Models/ is often a 0-byte placeholder. This
// function resolves the real weights by looking at the active snapshot in the
// HF cache for the repo that matches the model name.
//
// The returned bool is true only when at least one .safetensors weight was
// found, so callers can fall back to the local file size when the model is not
// really installed in the cache.
func demucsModelWeightBytes(name string) (int64, bool) {
	repo := demucsHFRepoName(name)
	if repo == "" {
		return 0, false
	}

	cacheDir := hfHubCacheDir()
	if cacheDir == "" {
		return 0, false
	}

	repoDir := filepath.Join(cacheDir, "models--"+strings.ReplaceAll(repo, "/", "--"))
	refPath := filepath.Join(repoDir, "refs", "main")
	refBytes, err := os.ReadFile(refPath)
	if err != nil {
		return 0, false
	}
	snapshotDir := filepath.Join(repoDir, "snapshots", strings.TrimSpace(string(refBytes)))

	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		return 0, false
	}

	var total int64
	found := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".safetensors") {
			continue
		}
		info, err := os.Stat(filepath.Join(snapshotDir, entry.Name()))
		if err != nil {
			continue
		}
		total += info.Size()
		found = true
	}

	return total, found
}
