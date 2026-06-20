package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
)

// setupFase10TestRoot creates a temporary project root and sets ONDA_ROOT so
// findProjectRoot() resolves to it during the test.
func setupFase10TestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(".", "fase10-test-")
	if err != nil {
		t.Fatalf("failed to create test root: %v", err)
	}
	t.Setenv("ONDA_ROOT", root)
	t.Cleanup(func() { os.RemoveAll(root) })

	for _, dir := range []string{"input", "output", "input_rubberband", "daw-data"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	return root
}

// newFase10TestServer registers all Fase 10 handlers on a fresh mux and starts
// an httptest server for them.
func newFase10TestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/audio/tempo-per-bar", s.handleTempoPerBar)
	s.mux.HandleFunc("POST /api/audio/trim", s.handleTrim)
	s.mux.HandleFunc("POST /api/audio/fade", s.handleFade)
	s.mux.HandleFunc("POST /api/audio/export", s.handleExport)
	s.mux.HandleFunc("GET /api/daw/stems", s.handleListStems)
	s.mux.HandleFunc("POST /api/daw/import", s.handleImportStem)
	s.mux.HandleFunc("POST /api/daw/upload", s.handleUploadAudio)
	s.mux.HandleFunc("GET /api/audio/tempo-grid", s.handleTempoGrid)

	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)
	return srv
}

// writeTestFile is reused from daw_test.go to drop arbitrary content into the
// temporary project tree.
func writeFase10TestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create parent dir: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
}

// writeSynthWAV writes a synthetic mono 16-bit WAV file with a simple beat-like
// pulse train. The generated file is valid for aubio/ffprobe/rubberband while
// remaining small and deterministic.
func writeSynthWAV(t *testing.T, path string, durationSec float64) {
	t.Helper()

	const (
		sampleRate  = 44100
		numChannels = 1
		bitDepth    = 16
		bpm         = 120.0
	)
	beatInterval := 60.0 / bpm
	numSamples := int(durationSec * sampleRate)
	data := make([]int, numSamples*numChannels)

	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		beatPos := t / beatInterval
		frac := beatPos - float64(int(beatPos))
		if frac*beatInterval < 0.05 {
			data[i] = 10000
		} else {
			data[i] = 0
		}
	}

	buf := &audio.IntBuffer{
		Data: data,
		Format: &audio.Format{
			SampleRate:  sampleRate,
			NumChannels: numChannels,
		},
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create wav dir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create wav: %v", err)
	}
	defer f.Close()

	enc := wav.NewEncoder(f, sampleRate, bitDepth, numChannels, 1)
	if err := enc.Write(buf); err != nil {
		t.Fatalf("failed to write wav: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("failed to close wav encoder: %v", err)
	}
}

func TestHandleTempoPerBar(t *testing.T) {
	root := setupFase10TestRoot(t)
	writeSynthWAV(t, filepath.Join(root, "input", "beat.wav"), 8.0)

	srv := newFase10TestServer(t)
	body := `{"file":"beat.wav","bars":[{"bar":1,"ratio":1.1}]}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/audio/tempo-per-bar", strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var tr TempoPerBarResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !strings.HasPrefix(tr.File, "tempo_per_bar_") {
		t.Fatalf("expected output file to start with tempo_per_bar_, got %s", tr.File)
	}
	if len(tr.Bars) != 1 || tr.Bars[0] != 1 {
		t.Fatalf("expected bars [1], got %v", tr.Bars)
	}
	if len(tr.Ratios) != 1 || tr.Ratios[0] != 1.1 {
		t.Fatalf("expected ratios [1.1], got %v", tr.Ratios)
	}

	outPath := filepath.Join(root, "daw-data", tr.File)
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected output file to exist: %v", err)
	}
}

func TestHandleTrim(t *testing.T) {
	root := setupFase10TestRoot(t)
	writeSynthWAV(t, filepath.Join(root, "input", "source.wav"), 2.0)

	srv := newFase10TestServer(t)
	body := `{"file":"source.wav","start":0.2,"end":0.8}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/audio/trim", strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var tr TrimResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if tr.File != "trim_source.wav" {
		t.Fatalf("expected trim_source.wav, got %s", tr.File)
	}

	outPath := filepath.Join(root, "daw-data", tr.File)
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected output file to exist: %v", err)
	}
}

func TestHandleFade(t *testing.T) {
	root := setupFase10TestRoot(t)
	writeSynthWAV(t, filepath.Join(root, "input", "source.wav"), 2.0)

	srv := newFase10TestServer(t)
	body := `{"file":"source.wav","type":"in","start":0,"duration":0.5}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/audio/fade", strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var fr FadeResponse
	if err := json.NewDecoder(resp.Body).Decode(&fr); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if fr.File != "fade_in_source.wav" {
		t.Fatalf("expected fade_in_source.wav, got %s", fr.File)
	}

	outPath := filepath.Join(root, "daw-data", fr.File)
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected output file to exist: %v", err)
	}
}

func TestHandleExport(t *testing.T) {
	root := setupFase10TestRoot(t)
	content := []byte("exported-audio-content")
	writeFase10TestFile(t, filepath.Join(root, "daw-data", "mix.wav"), content)

	srv := newFase10TestServer(t)
	body := `{"file":"mix.wav","format":"wav"}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/audio/export", strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var er ExportResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if er.File != "mix.wav" {
		t.Fatalf("expected mix.wav, got %s", er.File)
	}
	if er.Format != "wav" {
		t.Fatalf("expected format wav, got %s", er.Format)
	}
	if er.Size != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), er.Size)
	}
}

func TestHandleStems(t *testing.T) {
	root := setupFase10TestRoot(t)
	writeFase10TestFile(t, filepath.Join(root, "output", "cancion1", "vocals.wav"), []byte("vocals"))
	writeFase10TestFile(t, filepath.Join(root, "output", "cancion1", "instrumental.wav"), []byte("instrumental"))
	writeFase10TestFile(t, filepath.Join(root, "input_rubberband", "cancion1_pitch.wav"), []byte("pitch"))

	srv := newFase10TestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/api/daw/stems")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var sr StemsResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got, want := len(sr.Output), 1; got != want {
		t.Fatalf("expected %d output songs, got %d", want, got)
	}
	if got, want := sr.Output["cancion1"], []string{"instrumental.wav", "vocals.wav"}; !sliceEqual(got, want) {
		t.Fatalf("expected stems %v, got %v", want, got)
	}
	if got, want := sr.Pitch, []string{"cancion1_pitch.wav"}; !sliceEqual(got, want) {
		t.Fatalf("expected pitch %v, got %v", want, got)
	}
}

func TestHandleImport_Output(t *testing.T) {
	root := setupFase10TestRoot(t)
	srcContent := []byte("vocals-stem-content")
	writeFase10TestFile(t, filepath.Join(root, "output", "cancion1", "vocals.wav"), srcContent)

	srv := newFase10TestServer(t)
	body := `{"source":"output","song":"cancion1","stem":"vocals.wav"}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/daw/import", strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var ir ImportResponse
	if err := json.NewDecoder(resp.Body).Decode(&ir); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if ir.File != "import_cancion1_vocals.wav" {
		t.Fatalf("expected import_cancion1_vocals.wav, got %s", ir.File)
	}
	if ir.Size != int64(len(srcContent)) {
		t.Fatalf("expected size %d, got %d", len(srcContent), ir.Size)
	}
}

func TestHandleImport_Pitch(t *testing.T) {
	root := setupFase10TestRoot(t)
	srcContent := []byte("pitch-content")
	writeFase10TestFile(t, filepath.Join(root, "input_rubberband", "mi_pitch.wav"), srcContent)

	srv := newFase10TestServer(t)
	body := `{"source":"pitch","stem":"mi_pitch.wav"}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/daw/import", strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var ir ImportResponse
	if err := json.NewDecoder(resp.Body).Decode(&ir); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if ir.File != "import_mi_pitch.wav" {
		t.Fatalf("expected import_mi_pitch.wav, got %s", ir.File)
	}
	if ir.Size != int64(len(srcContent)) {
		t.Fatalf("expected size %d, got %d", len(srcContent), ir.Size)
	}
}

func TestHandleUpload(t *testing.T) {
	setupFase10TestRoot(t)
	srv := newFase10TestServer(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "mi_cancion.wav")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	content := []byte("uploaded-audio-content")
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("failed to write form file: %v", err)
	}
	mw.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/daw/upload", &buf)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var ur UploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&ur); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if ur.File != "upload_mi_cancion.wav" {
		t.Fatalf("expected upload_mi_cancion.wav, got %s", ur.File)
	}
	if ur.Size != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), ur.Size)
	}
}

func TestHandleTempoGrid(t *testing.T) {
	root := setupFase10TestRoot(t)
	writeSynthWAV(t, filepath.Join(root, "input", "beat.wav"), 8.0)

	srv := newFase10TestServer(t)
	resp, err := srv.Client().Get(srv.URL + "/api/audio/tempo-grid?file=beat.wav")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var gr TempoGridResponse
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if gr.BPM <= 0 {
		t.Fatalf("expected positive bpm, got %f", gr.BPM)
	}
	if len(gr.Beats) == 0 {
		t.Fatalf("expected non-empty beats")
	}
	if len(gr.Bars) == 0 {
		t.Fatalf("expected non-empty bars")
	}
	if gr.Duration <= 0 {
		t.Fatalf("expected positive duration, got %f", gr.Duration)
	}
}
