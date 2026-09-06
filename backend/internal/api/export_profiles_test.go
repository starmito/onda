package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportProfilesDefaults(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	// Ensure the file does not exist so loadExportProfilesAt uses defaults.
	tmpPath := filepath.Join(root, "audio_export_profiles.json")

	var profiles AudioExportProfiles
	if err := loadExportProfilesAt(tmpPath, &profiles); err != nil {
		t.Fatalf("loadExportProfilesAt failed: %v", err)
	}

	if profiles.DefaultFormat != "flac" {
		t.Errorf("expected default format flac, got %q", profiles.DefaultFormat)
	}
	if profiles.NameTemplate != "{song} ({pitches}) ({suffix})" {
		t.Errorf("expected default name template, got %q", profiles.NameTemplate)
	}
	if profiles.Formats == nil {
		t.Fatal("expected formats map to be initialized")
	}

	want := map[string]*FormatProfile{
		"wav": {BitDepth: "32f", SampleRate: "source"},
		"flac": {Compression: 5, BitDepth: "24"},
		"mp3": {Bitrate: "320k", Mode: "cbr"},
	}

	for name, wantProfile := range want {
		got, ok := profiles.Formats[name]
		if !ok {
			t.Errorf("missing format profile %q", name)
			continue
		}
		if got.BitDepth != wantProfile.BitDepth {
			t.Errorf("%s bitDepth = %q, want %q", name, got.BitDepth, wantProfile.BitDepth)
		}
		if got.SampleRate != wantProfile.SampleRate {
			t.Errorf("%s sampleRate = %q, want %q", name, got.SampleRate, wantProfile.SampleRate)
		}
		if got.Compression != wantProfile.Compression {
			t.Errorf("%s compression = %d, want %d", name, got.Compression, wantProfile.Compression)
		}
		if got.Bitrate != wantProfile.Bitrate {
			t.Errorf("%s bitrate = %q, want %q", name, got.Bitrate, wantProfile.Bitrate)
		}
		if got.Mode != wantProfile.Mode {
			t.Errorf("%s mode = %q, want %q", name, got.Mode, wantProfile.Mode)
		}
	}
}

func TestExportProfilesRoundTrip(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	tmpPath := filepath.Join(root, "audio_export_profiles.json")

	want := AudioExportProfiles{
		DefaultFormat: "mp3",
		Formats: map[string]*FormatProfile{
			"wav":  {BitDepth: "24", SampleRate: "48000"},
			"flac": {Compression: 8, BitDepth: "16"},
			"mp3":  {Bitrate: "192k", Mode: "vbr"},
		},
	}

	if err := saveExportProfilesAt(tmpPath, &want); err != nil {
		t.Fatalf("saveExportProfilesAt failed: %v", err)
	}

	var got AudioExportProfiles
	if err := loadExportProfilesAt(tmpPath, &got); err != nil {
		t.Fatalf("loadExportProfilesAt failed: %v", err)
	}

	if got.DefaultFormat != want.DefaultFormat {
		t.Errorf("defaultFormat = %q, want %q", got.DefaultFormat, want.DefaultFormat)
	}
	for name, wantProfile := range want.Formats {
		gotProfile, ok := got.Formats[name]
		if !ok {
			t.Errorf("missing format profile %q", name)
			continue
		}
		if gotProfile.BitDepth != wantProfile.BitDepth {
			t.Errorf("%s bitDepth = %q, want %q", name, gotProfile.BitDepth, wantProfile.BitDepth)
		}
		if gotProfile.SampleRate != wantProfile.SampleRate {
			t.Errorf("%s sampleRate = %q, want %q", name, gotProfile.SampleRate, wantProfile.SampleRate)
		}
		if gotProfile.Compression != wantProfile.Compression {
			t.Errorf("%s compression = %d, want %d", name, gotProfile.Compression, wantProfile.Compression)
		}
		if gotProfile.Bitrate != wantProfile.Bitrate {
			t.Errorf("%s bitrate = %q, want %q", name, gotProfile.Bitrate, wantProfile.Bitrate)
		}
		if gotProfile.Mode != wantProfile.Mode {
			t.Errorf("%s mode = %q, want %q", name, gotProfile.Mode, wantProfile.Mode)
		}
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("failed to read persisted file: %v", err)
	}
	if !strings.Contains(string(data), `"defaultFormat"`) {
		t.Errorf("persisted JSON should contain defaultFormat key")
	}

	// Outgoing JSON from the HTTP handler must also use camelCase.
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/export/profiles", s.handleGetExportProfiles)
	req := httptest.NewRequest(http.MethodGet, "/api/export/profiles", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	if !strings.Contains(rr.Body.String(), `"defaultFormat"`) {
		t.Errorf("handler response should contain defaultFormat key, got %s", rr.Body.String())
	}
}

func TestHandleGetExportProfiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/export/profiles", s.handleGetExportProfiles)

	req := httptest.NewRequest(http.MethodGet, "/api/export/profiles", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp AudioExportProfiles
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.DefaultFormat != "flac" {
		t.Errorf("expected default format flac, got %q", resp.DefaultFormat)
	}
	if _, ok := resp.Formats["flac"]; !ok {
		t.Errorf("expected flac profile in response")
	}
}

func TestHandleSaveExportProfiles_InvalidJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ONDA_ROOT", root)

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/export/profiles", s.handleSaveExportProfiles)

	req := httptest.NewRequest(http.MethodPost, "/api/export/profiles", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}
