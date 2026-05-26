package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthEndpoint(t *testing.T) {
	srv := NewServer(":0")
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatalf("failed to GET /api/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var health map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	// Check top-level fields
	if v, ok := health["version"].(string); !ok || v == "" {
		t.Error("expected non-empty version in health response")
	}
	if _, ok := health["status"]; !ok {
		t.Error("expected status field in health response")
	}

	// Verify nested objects exist
	for _, key := range []string{"backend", "gpu", "disk", "docker"} {
		if _, ok := health[key]; !ok {
			t.Errorf("expected %q sub-object in health response", key)
		}
	}
}

func TestCORSHeaders(t *testing.T) {
	srv := NewServer(":0")
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodOptions, ts.URL+"/api/health", nil)
	if err != nil {
		t.Fatalf("failed to create OPTIONS request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to send OPTIONS /api/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected status 204 for OPTIONS, got %d", resp.StatusCode)
	}

	acao := resp.Header.Get("Access-Control-Allow-Origin")
	if acao != "*" {
		t.Errorf("expected Access-Control-Allow-Origin: *, got %s", acao)
	}

	acam := resp.Header.Get("Access-Control-Allow-Methods")
	if acam != "GET, POST, OPTIONS" {
		t.Errorf("expected Access-Control-Allow-Methods: GET, POST, OPTIONS, got %s", acam)
	}

	acah := resp.Header.Get("Access-Control-Allow-Headers")
	if acah != "Content-Type" {
		t.Errorf("expected Access-Control-Allow-Headers: Content-Type, got %s", acah)
	}
}

func TestStatusEndpoint_NoPipeline(t *testing.T) {
	srv := NewServer(":0")
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/status")
	if err != nil {
		t.Fatalf("failed to GET /api/status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestHealthMethodNotAllowed(t *testing.T) {
	srv := NewServer(":0")
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/health", nil)
	if err != nil {
		t.Fatalf("failed to create POST request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to send POST /api/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405 for POST, got %d", resp.StatusCode)
	}
}

func TestGPUEndpoint(t *testing.T) {
	srv := NewServer(":0")
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/gpu")
	if err != nil {
		t.Fatalf("failed to GET /api/gpu: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var gpuResp struct {
		Available bool   `json:"available"`
		Info      string `json:"info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gpuResp); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
}

func TestModelsEndpoint(t *testing.T) {
	srv := NewServer(":0")
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/models")
	if err != nil {
		t.Fatalf("failed to GET /api/models: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var models map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if len(models) == 0 {
		t.Error("expected non-empty models map")
	}

	expectedPresets := []string{"turbo", "balance", "master", "ultimate"}
	for _, name := range expectedPresets {
		if _, ok := models[name]; !ok {
			t.Errorf("expected preset %q in models response", name)
		}
	}
}

func TestSeparateEndpoint_InvalidJSON(t *testing.T) {
	srv := NewServer(":0")
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/separate", "application/json",
		strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("failed to POST /api/separate: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid JSON, got %d", resp.StatusCode)
	}
}

func TestSeparateEndpoint_InvalidPreset(t *testing.T) {
	srv := NewServer(":0")
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	body := `{"preset": "nonexistent", "input": "/tmp/test.wav"}`
	resp, err := http.Post(ts.URL+"/api/separate", "application/json",
		strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to POST /api/separate: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid preset, got %d", resp.StatusCode)
	}
}

func TestSeparateEndpoint_ValidInput(t *testing.T) {
	srv := NewServer(":0")
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	body := `{"preset": "turbo", "input": "/tmp/test_song.flac", "output": "/tmp/output/", "vocal_model": "melband_kj", "pitch": 0}`
	resp, err := http.Post(ts.URL+"/api/separate", "application/json",
		strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to POST /api/separate: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected status 202, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if result["status"] != "started" {
		t.Errorf("expected status 'started', got %q", result["status"])
	}
	if result["song"] != "test_song" {
		t.Errorf("expected song 'test_song', got %q", result["song"])
	}
}

func TestEventsEndpoint_SSEHeaders(t *testing.T) {
	srv := NewServer(":0")
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to GET /api/events: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", ct)
	}

	cc := resp.Header.Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("expected Cache-Control no-cache, got %s", cc)
	}
}
