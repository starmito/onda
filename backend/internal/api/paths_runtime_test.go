package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestModelsFollowDataRoot verifies that the model manager resolves and writes
// under the current data root even after the root is changed at runtime.
func TestModelsFollowDataRoot(t *testing.T) {
	assertTestRoot(t)

	rootA := newTestRoot(t, "models-root-a-")
	rootB := newTestRoot(t, "models-root-b-")

	t.Setenv("ONDA_SERVICE_LOG_PATH", "")
	t.Setenv("ONDA_DATA_DIR", rootA)

	modelA := filepath.Join(rootA, "models", "VR_Models", "test.pth")
	writeTestFile(t, modelA, []byte("model-a"))

	respA := listModels()
	if len(respA.Models) == 0 {
		t.Fatalf("expected model in root A, got none")
	}
	if !strings.HasPrefix(respA.Models[0].Path, filepath.Join(rootA, "models")) {
		t.Errorf("model path %q does not start with root A models dir", respA.Models[0].Path)
	}

	// Switch data root at runtime.
	t.Setenv("ONDA_DATA_DIR", rootB)

	modelB := filepath.Join(rootB, "models", "VR_Models", "test.pth")
	writeTestFile(t, modelB, []byte("model-b"))

	respB := listModels()
	if len(respB.Models) == 0 {
		t.Fatalf("expected model in root B, got none")
	}
	if !strings.HasPrefix(respB.Models[0].Path, filepath.Join(rootB, "models")) {
		t.Errorf("model path %q does not start with root B models dir", respB.Models[0].Path)
	}
	for _, m := range respB.Models {
		if strings.HasPrefix(m.Path, filepath.Join(rootA, "models")) {
			t.Errorf("model %q still resolves to root A after switch", m.Path)
		}
	}
}

// TestLogsFollowDataRoot verifies that the default service log store writes to
// the current data root even after the root is changed at runtime.
func TestLogsFollowDataRoot(t *testing.T) {
	assertTestRoot(t)

	rootA := newTestRoot(t, "logs-root-a-")
	rootB := newTestRoot(t, "logs-root-b-")

	t.Setenv("ONDA_SERVICE_LOG_PATH", "")
	t.Setenv("ONDA_DATA_DIR", rootA)

	Log("backend", "info", "message from root A")

	logA := filepath.Join(rootA, "logs", "onda.log")
	dataA, err := os.ReadFile(logA)
	if err != nil {
		t.Fatalf("failed to read log A at %s: %v", logA, err)
	}
	if !strings.Contains(string(dataA), "message from root A") {
		t.Errorf("log A does not contain expected entry: %s", string(dataA))
	}

	// Switch data root at runtime.
	t.Setenv("ONDA_DATA_DIR", rootB)

	Log("backend", "info", "message from root B")

	logB := filepath.Join(rootB, "logs", "onda.log")
	dataB, err := os.ReadFile(logB)
	if err != nil {
		t.Fatalf("failed to read log B at %s: %v", logB, err)
	}
	if !strings.Contains(string(dataB), "message from root B") {
		t.Errorf("log B does not contain expected entry: %s", string(dataB))
	}

	// The old log file must not have received the new entry.
	dataA2, err := os.ReadFile(logA)
	if err != nil {
		t.Fatalf("failed to re-read log A: %v", err)
	}
	if strings.Contains(string(dataA2), "message from root B") {
		t.Errorf("log A received an entry after the root switched away")
	}
}
