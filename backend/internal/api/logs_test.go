package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
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
	// onda.log.YYYYMMDD-HHMMSS and a new onda.log is started.
	Log("backend", "info", "third")

	entries := defaultLogStore.readRecent(0)
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

	matches, err := filepath.Glob(defaultLogStore.path + ".[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9][0-9][0-9]*")
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(matches) == 0 {
		t.Errorf("expected a rotated log file with timestamp suffix, got none")
	}
}

func TestServiceLogStore_PurgeByAge(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()

	// Force small rotations so each write creates a new generation.
	defaultLogStore.maxSize = 70

	// Create several generations.
	for i := 0; i < 5; i++ {
		Log("backend", "info", fmt.Sprintf("entry %d", i))
	}

	dir := filepath.Dir(defaultLogStore.path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read log dir: %v", err)
	}

	oldCutoff := time.Now().Add(-8 * 24 * time.Hour)
	recentCutoff := time.Now().Add(-2 * 24 * time.Hour)

	oldMarked := false
	for _, e := range entries {
		name := e.Name()
		if name == filepath.Base(defaultLogStore.path) {
			continue
		}
		path := filepath.Join(dir, name)
		if !oldMarked {
			_ = os.Chtimes(path, oldCutoff, oldCutoff)
			oldMarked = true
		} else {
			_ = os.Chtimes(path, recentCutoff, recentCutoff)
		}
	}
	if !oldMarked {
		t.Fatal("expected at least one rotated file to mark as old")
	}

	// Trigger a write to invoke purge.
	Log("backend", "info", "trigger")

	// Verify the old file is gone and recent ones remain.
	entries, err = os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read log dir after purge: %v", err)
	}
	activeBase := filepath.Base(defaultLogStore.path)
	for _, e := range entries {
		name := e.Name()
		if name == activeBase {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if info.ModTime().Before(time.Now().Add(-7 * 24 * time.Hour)) {
			t.Errorf("old rotated file %q should have been purged", name)
		}
	}
}

func TestServiceLogStore_NoGenerationLimitWithinWindow(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()

	// Small size to force multiple rotations.
	defaultLogStore.maxSize = 60

	// Generate enough entries to create 5 rotated files.
	for i := 0; i < 7; i++ {
		Log("backend", "info", fmt.Sprintf("entry %d", i))
	}

	dir := filepath.Dir(defaultLogStore.path)
	activeBase := filepath.Base(defaultLogStore.path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read log dir: %v", err)
	}
	rotated := 0
	for _, e := range entries {
		if e.Name() != activeBase {
			rotated++
		}
	}
	if rotated < 5 {
		t.Fatalf("expected at least 5 rotated files within retention window, got %d", rotated)
	}

	// Ensure all recent files are still readable.
	all := defaultLogStore.readRecent(0)
	if len(all) < 7 {
		t.Errorf("expected at least 7 persisted entries, got %d", len(all))
	}
}

func TestHandleGetServiceLogs_RecentFirstAfterRotations(t *testing.T) {
	setupTestLogStore(t)
	resetLogBuffer()

	// Force frequent rotations.
	defaultLogStore.maxSize = 90

	// Write entries with explicit increasing timestamps.
	for i := 0; i < 6; i++ {
		LogWithNano("backend", "info", fmt.Sprintf("msg %d", i), int64(i+1)*1e9)
	}

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/logs/services", s.handleGetServiceLogs)

	req := httptest.NewRequest(http.MethodGet, "/api/logs/services?limit=4", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var logs []LogEntry
	if err := json.Unmarshal(rr.Body.Bytes(), &logs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(logs) != 4 {
		t.Fatalf("expected 4 logs, got %d", len(logs))
	}

	// Most recent first.
	for i, l := range logs {
		want := fmt.Sprintf("msg %d", 5-i)
		if l.Message != want {
			t.Errorf("log[%d].Message = %q, want %q", i, l.Message, want)
		}
	}
}
