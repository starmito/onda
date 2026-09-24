package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestExportIndex_HidesRealExportNames verifies that files whose names follow
// the configurable export template (song + one or more parenthesised groups +
// audio extension) are not listed as available stems or pitch subgroups.
// Before the export-index fix this test is RED: the export file leaks into
// the listings. After the fix it turns GREEN.
func TestExportIndex_HidesRealExportNames(t *testing.T) {
	root := setTestRoot(t, "export-index-")
	song := "Mi Canción"
	songDir := filepath.Join(root, "output", song)
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}

	// Reset the lazy index so it is seeded against this fresh test root.
	resetExportIndexForTests()

	// A normal separated stem must remain visible.
	writeTestFile(t, filepath.Join(songDir, "vocals.wav"), []byte("vocals"))

	// A file that matches the real export name template: it should be hidden.
	realExportName := song + " (0) (mezcla).flac"
	writeTestFile(t, filepath.Join(songDir, realExportName), []byte("export"))

	// Also create a pitch subgroup with both a normal stem and an export.
	pitchDir := filepath.Join(songDir, song+"_pitch+1")
	if err := os.MkdirAll(pitchDir, 0o755); err != nil {
		t.Fatalf("failed to create pitch dir: %v", err)
	}
	writeTestFile(t, filepath.Join(pitchDir, "bass_pitch+1.wav"), []byte("bass"))
	pitchedExportName := song + " (+1) (mezcla).flac"
	writeTestFile(t, filepath.Join(pitchDir, pitchedExportName), []byte("pitched-export"))

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/daw/stems", s.handleListStems)
	s.mux.HandleFunc("GET /api/pitch/{song}", s.handleListPitchSubgroups)

	// ---- GET /api/daw/stems ----
	req := httptest.NewRequest(http.MethodGet, "/api/daw/stems", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("daw/stems expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var stemsResp StemsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &stemsResp); err != nil {
		t.Fatalf("failed to decode stems response: %v", err)
	}

	baseStems, ok := stemsResp.Output[song]
	if !ok {
		t.Fatalf("expected song %q in stems output, got %v", song, stemsResp.Output)
	}
	if slices.Contains(baseStems, realExportName) {
		t.Errorf("base group lists export %q; it should be hidden", realExportName)
	}
	if !slices.Contains(baseStems, "vocals.wav") {
		t.Errorf("base group should still list normal stem vocals.wav, got %v", baseStems)
	}

	pitchKey := song + " (pitch +1)"
	pitchStems, ok := stemsResp.Output[pitchKey]
	if !ok {
		t.Fatalf("expected pitch key %q in stems output, got %v", pitchKey, stemsResp.Output)
	}
	if slices.Contains(pitchStems, pitchedExportName) {
		t.Errorf("pitch group lists export %q; it should be hidden", pitchedExportName)
	}
	if !slices.Contains(pitchStems, "bass_pitch+1.wav") {
		t.Errorf("pitch group should still list normal stem bass_pitch+1.wav, got %v", pitchStems)
	}

	// ---- GET /api/pitch/{song} ----
	req = httptest.NewRequest(http.MethodGet, "/api/pitch/Mi%20Canci%C3%B3n", nil)
	rr = httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("pitch expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var pitchResp []PitchSubgroup
	if err := json.Unmarshal(rr.Body.Bytes(), &pitchResp); err != nil {
		t.Fatalf("failed to decode pitch response: %v", err)
	}
	if len(pitchResp) != 1 {
		t.Fatalf("expected one pitch subgroup, got %v", pitchResp)
	}
	for _, f := range pitchResp[0].Files {
		if f.Name == pitchedExportName {
			t.Errorf("pitch subgroup lists export %q; it should be hidden", pitchedExportName)
		}
	}
}

// TestExportIndex_RegistersNewExport verifies that an export written by the
// merge handler is recorded in the export index and therefore does not show up
// in the stem listing, even when its name follows the real export template.
func TestExportIndex_RegistersNewExport(t *testing.T) {
	skipIfMissingBinary(t, "ffmpeg")

	root := setTestRoot(t, "export-index-")
	song := "Mi Canción"
	songDir := filepath.Join(root, "output", song)
	if err := os.MkdirAll(songDir, 0o755); err != nil {
		t.Fatalf("failed to create song dir: %v", err)
	}

	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "0.1", "-acodec", "pcm_s16le", filepath.Join(songDir, "vocals.wav"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create test wav: %v\n%s", err, string(out))
	}

	// Reset the lazy index so it is re-seeded against this fresh test root.
	resetExportIndexForTests()

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/stems/merge", s.handleStemsMerge)
	s.mux.HandleFunc("GET /api/daw/stems", s.handleListStems)

	exportName := song + " (0) (mezcla).flac"
	reqBody := MergeRequest{
		Song:       song,
		Stems:      []string{"vocals.wav"},
		Format:     "flac",
		OutputName: exportName,
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/stems/merge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("merge expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var mergeResp MergeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &mergeResp); err != nil {
		t.Fatalf("failed to decode merge response: %v", err)
	}
	if mergeResp.File != exportName {
		t.Fatalf("expected exported file %q, got %q", exportName, mergeResp.File)
	}

	// The index must know about the newly written export.
	if !isRegisteredExport(song, exportName) {
		t.Errorf("export %q was not registered for song %q", exportName, song)
	}

	// And the stem listing must hide it.
	req = httptest.NewRequest(http.MethodGet, "/api/daw/stems", nil)
	rr = httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("daw/stems expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var stemsResp StemsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &stemsResp); err != nil {
		t.Fatalf("failed to decode stems response: %v", err)
	}
	for _, name := range stemsResp.Output[song] {
		if name == exportName {
			t.Errorf("new export %q still appears in stem listing", exportName)
		}
	}

	// Sanity: the normal stem is still there.
	if !slices.Contains(stemsResp.Output[song], "vocals.wav") {
		t.Errorf("normal stem vocals.wav missing from listing: %v", stemsResp.Output[song])
	}

	// The configured export directory sits outside output/, so the file must
	// be reachable through its API URL.
	wantURL := "/api/export/files/" + url.PathEscape(exportName)
	if !strings.HasPrefix(mergeResp.URL, wantURL) {
		t.Errorf("expected download URL %q, got %q", wantURL, mergeResp.URL)
	}
}
