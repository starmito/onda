package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func resetLogBuffer() {
	logBufferMu.Lock()
	defer logBufferMu.Unlock()
	logBuffer = nil
}

// setupTestLogStore swaps the global log store with a temporary one inside the
// repo (daw-data/tmp) so persistence tests never touch /tmp or /app/logs.
func setupTestLogStore(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..", "..", "daw-data", "tmp", t.Name())
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("failed to clean test log dir: %v", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("failed to create test log dir: %v", err)
	}
	path := filepath.Join(root, "onda.log")
	old := defaultLogStore
	defaultLogStore = newServiceLogStore(path)
	t.Cleanup(func() {
		defaultLogStore = old
		_ = os.RemoveAll(root)
	})
	return path
}

func TestHandleGetServiceLogs_ReturnsOndaServiceLogs(t *testing.T) {
	resetLogBuffer()
	Log("backend", "info", "backend message")
	Log("pipeline", "info", "pipeline message")
	Log("nginx", "error", "legacy nginx log")
	Log("legacy", "error", "legacy service log")

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/logs/services", s.handleGetServiceLogs)

	req := httptest.NewRequest(http.MethodGet, "/api/logs/services", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var logs []LogEntry
	if err := json.Unmarshal(rr.Body.Bytes(), &logs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(logs) != 2 {
		t.Fatalf("expected 2 service logs, got %d", len(logs))
	}

	seen := make(map[string]bool)
	for _, l := range logs {
		seen[l.Service] = true
	}
	if !seen["backend"] || !seen["pipeline"] {
		t.Errorf("expected backend and pipeline logs, got %v", seen)
	}
	if seen["nginx"] || seen["legacy"] {
		t.Errorf("legacy services must be excluded, got %v", seen)
	}
}

func TestHandleGetServiceLogs_LimitParameter(t *testing.T) {
	resetLogBuffer()
	for i := 0; i < 10; i++ {
		Log("backend", "info", "msg")
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/logs/services", s.handleGetServiceLogs)

	req := httptest.NewRequest(http.MethodGet, "/api/logs/services?limit=3", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	var logs []LogEntry
	if err := json.Unmarshal(rr.Body.Bytes(), &logs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(logs) != 3 {
		t.Errorf("expected 3 logs, got %d", len(logs))
	}
}

func TestHandleGetServiceLogs_PersistedSurvivesBufferReset(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()

	Log("backend", "info", "persisted backend message")
	Log("pipeline", "info", "persisted pipeline message")

	// Simulate a container restart: the in-memory ring buffer is gone but the
	// persisted file must still contain the entries.
	resetLogBuffer()

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/logs/services", s.handleGetServiceLogs)

	req := httptest.NewRequest(http.MethodGet, "/api/logs/services", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var logs []LogEntry
	if err := json.Unmarshal(rr.Body.Bytes(), &logs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(logs) != 2 {
		t.Fatalf("expected 2 service logs after buffer reset, got %d", len(logs))
	}

	seen := make(map[string]bool)
	for _, l := range logs {
		seen[l.Service] = true
	}
	if !seen["backend"] || !seen["pipeline"] {
		t.Errorf("expected backend and pipeline logs, got %v", seen)
	}
}

func TestHandleGetServiceLogs_LimitAcrossPersistedAndBuffer(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()

	// Persist 5 older entries and wipe the buffer.
	for i := 0; i < 5; i++ {
		Log("backend", "info", fmt.Sprintf("persisted %d", i))
	}
	resetLogBuffer()

	// Add 3 newer entries that only live in memory.
	for i := 0; i < 3; i++ {
		Log("backend", "info", fmt.Sprintf("buffer %d", i))
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/logs/services", s.handleGetServiceLogs)

	req := httptest.NewRequest(http.MethodGet, "/api/logs/services?limit=4", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	var logs []LogEntry
	if err := json.Unmarshal(rr.Body.Bytes(), &logs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(logs) != 4 {
		t.Fatalf("expected 4 logs, got %d", len(logs))
	}

	// The 3 most recent entries are the in-memory ones.
	for i := 0; i < 3; i++ {
		want := fmt.Sprintf("buffer %d", 2-i) // most recent first
		if logs[i].Message != want {
			t.Errorf("log[%d].Message = %q, want %q", i, logs[i].Message, want)
		}
	}
}

func TestServiceLogStore_Rotation(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()

	// Force rotation after roughly two entries.
	defaultLogStore.maxSize = 120

	Log("backend", "info", "first")
	Log("backend", "info", "second")
	// The second write makes the file exceed maxSize, so it is rotated to
	// onda.log.1 and a new onda.log is started.
	Log("backend", "info", "third")

	entries := defaultLogStore.readAll()
	if len(entries) < 3 {
		t.Fatalf("expected at least 3 persisted entries after rotation, got %d", len(entries))
	}

	seen := make(map[string]bool)
	for _, e := range entries {
		seen[e.Message] = true
	}
	for _, msg := range []string{"first", "second", "third"} {
		if !seen[msg] {
			t.Errorf("missing rotated log entry %q", msg)
		}
	}

	if _, err := os.Stat(defaultLogStore.path + ".1"); err != nil {
		t.Errorf("rotated log file %q should exist: %v", defaultLogStore.path+".1", err)
	}
}
