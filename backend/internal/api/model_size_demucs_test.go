package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHFHubCacheDir_WithHFHome(t *testing.T) {
	// When HF_HOME points inside the data root, the Hub cache lives in the
	 // hub/ sub-directory (standard HuggingFace layout). This makes the model
	 // weights persistent across container recreations.
	 t.Setenv("HF_HUB_CACHE", "")
	 t.Setenv("HUGGINGFACE_HUB_CACHE", "")
	 t.Setenv("HF_HOME", "/app/data/.cache/huggingface")

	 got := hfHubCacheDir()
	 want := "/app/data/.cache/huggingface/hub"
	 if got != want {
		 t.Errorf("hfHubCacheDir() = %q, want %q", got, want)
	 }
}

func TestHFHubCacheDir_Fallback(t *testing.T) {
	// With no HF variable set, the function keeps its original fallback to
	// $HOME/.cache/huggingface/hub.
	t.Setenv("HF_HUB_CACHE", "")
	t.Setenv("HUGGINGFACE_HUB_CACHE", "")
	t.Setenv("HF_HOME", "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine home dir: %v", err)
	}
	want := filepath.Join(home, ".cache", "huggingface", "hub")
	if got := hfHubCacheDir(); got != want {
		t.Errorf("hfHubCacheDir() = %q, want %q", got, want)
	}
}
