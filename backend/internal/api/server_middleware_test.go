package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupMiddlewareTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(".", "middleware-test-")
	if err != nil {
		t.Fatalf("failed to create test root: %v", err)
	}
	t.Setenv("ONDA_ROOT", root)
	t.Cleanup(func() { os.RemoveAll(root) })

	for _, dir := range []string{"input", "output", "input_rubberband", "daw-data", "frontend", "frontend/dist"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	// Provide a minimal static file so the catch-all file server doesn't 500.
	if err := os.WriteFile(filepath.Join(root, "frontend", "dist", "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatalf("failed to write index.html: %v", err)
	}
	return root
}

func TestMiddleware404_DistinguishesAPIAndStatic(t *testing.T) {
	setupMiddlewareTestRoot(t)
	resetLogBuffer()

	server := NewServer(":0")
	defer server.Close()

	// Missing API route should be logged as "404 api".
	reqAPI := httptest.NewRequest(http.MethodGet, "/api/nonexistent", nil)
	reqAPI.Header.Set("User-Agent", "test-agent")
	rrAPI := httptest.NewRecorder()
	server.Handler.ServeHTTP(rrAPI, reqAPI)
	if rrAPI.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing api route, got %d", rrAPI.Code)
	}

	// Missing static asset should be logged as "404 static".
	reqStatic := httptest.NewRequest(http.MethodGet, "/missing-asset.js", nil)
	reqStatic.Header.Set("User-Agent", "test-agent")
	rrStatic := httptest.NewRecorder()
	server.Handler.ServeHTTP(rrStatic, reqStatic)
	if rrStatic.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing static asset, got %d", rrStatic.Code)
	}

	logBufferMu.RLock()
	defer logBufferMu.RUnlock()

	var apiFound, staticFound bool
	for _, entry := range logBuffer {
		if !strings.Contains(entry.Message, "404 ") {
			continue
		}
		if strings.Contains(entry.Message, "404 api: GET /api/nonexistent") {
			apiFound = true
		}
		if strings.Contains(entry.Message, "404 static: GET /missing-asset.js") {
			staticFound = true
		}
	}

	if !apiFound {
		t.Errorf("expected a '404 api' log entry, got: %v", logBuffer)
	}
	if !staticFound {
		t.Errorf("expected a '404 static' log entry, got: %v", logBuffer)
	}
}
