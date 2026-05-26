package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if health.Version == "" {
		t.Error("expected non-empty version in health response")
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
