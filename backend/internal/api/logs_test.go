package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func resetLogBuffer() {
	logBufferMu.Lock()
	defer logBufferMu.Unlock()
	logBuffer = nil
}

func TestHandleGetServiceLogs_ReturnsOndaServiceLogs(t *testing.T) {
	resetLogBuffer()
	Log("backend", "info", "backend message")
	Log("pipeline", "info", "pipeline message")
	Log("nginx", "error", "legacy nginx log")
	Log("onda-gui", "error", "legacy onda-gui log")

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
	if seen["nginx"] || seen["onda-gui"] {
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
