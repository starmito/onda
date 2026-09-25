package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureVRAMTestClip(t *testing.T) {
	setTestRoot(t, "vram-clip-")

	path, err := ensureVRAMTestClip()
	if err != nil {
		t.Fatalf("ensureVRAMTestClip failed: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty clip path")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("clip file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("clip file is empty")
	}

	// Calling again should return the existing file without error.
	path2, err := ensureVRAMTestClip()
	if err != nil {
		t.Fatalf("second ensureVRAMTestClip failed: %v", err)
	}
	if path2 != path {
		t.Fatalf("expected same path on second call, got %q vs %q", path2, path)
	}

	// Verify it lives under the configured data root.
	root := dataRoot()
	if !filepath.IsAbs(path) {
		t.Fatalf("clip path must be absolute: %q", path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatalf("clip path is not under data root: %v", err)
	}
	if rel == "" || rel == "." {
		t.Fatalf("clip path must be inside data root, got %q", rel)
	}
}
