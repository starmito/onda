package pipeline

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/starmito/onda/internal/cli"
)

func TestNew(t *testing.T) {
	flags := &cli.PipelineFlags{
		Preset:       "balance",
		VocalModel:   "polarformer",
		VocalOverlap: 4,
		VocalKeep:    "both",
		StemModel:    "htdemucs_ft",
		Input:        "/tmp/song.wav",
	}

	p := New(flags)
	if p == nil {
		t.Fatal("New() returned nil")
	}

	if p.song != "song" {
		t.Errorf("expected song 'song', got %q", p.song)
	}
}

func TestNewWithOutput(t *testing.T) {
	flags := &cli.PipelineFlags{
		Preset:       "turbo",
		VocalModel:   "melband_kj",
		VocalOverlap: 2,
		VocalKeep:    "both",
		StemModel:    "htdemucs_ft",
		Input:        "/data/music/track.flac",
		Output:       "/output/track",
	}

	p := New(flags)
	if p.song != "track" {
		t.Errorf("expected song 'track', got %q", p.song)
	}
	if p.outputDir != "/output/track" {
		t.Errorf("expected outputDir '/output/track', got %q", p.outputDir)
	}
	if p.dockerInput != "/input/track.flac" {
		t.Errorf("expected dockerInput '/input/track.flac', got %q", p.dockerInput)
	}
	if p.dockerOutput != "/output/track" {
		t.Errorf("expected dockerOutput '/output/track', got %q", p.dockerOutput)
	}
}

func TestNewWithoutOutput(t *testing.T) {
	flags := &cli.PipelineFlags{
		Preset:       "balance",
		VocalModel:   "polarformer",
		VocalOverlap: 4,
		VocalKeep:    "both",
		Input:        "/tmp/song.wav",
	}

	p := New(flags)
	expectedOutput := "output/song"
	if p.outputDir != expectedOutput {
		t.Errorf("expected outputDir %q, got %q", expectedOutput, p.outputDir)
	}
}

func TestStatusFile(t *testing.T) {
	path := StatusFile()
	if path == "" {
		t.Fatal("StatusFile() returned empty string")
	}
	if !strings.HasSuffix(path, "onda_pipeline_status.json") {
		t.Errorf("expected path ending in 'onda_pipeline_status.json', got %q", path)
	}
}

func TestStatusJSON(t *testing.T) {
	// Test serialization
	original := Status{
		Status:   "running",
		Progress: 0.5,
		Step:     "vocals",
		Song:     "test_song",
		Elapsed:  42,
		ETA:      42,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Test deserialization
	var decoded Status
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.Status != original.Status {
		t.Errorf("Status: got %q, want %q", decoded.Status, original.Status)
	}
	if decoded.Progress != original.Progress {
		t.Errorf("Progress: got %f, want %f", decoded.Progress, original.Progress)
	}
	if decoded.Step != original.Step {
		t.Errorf("Step: got %q, want %q", decoded.Step, original.Step)
	}
	if decoded.Song != original.Song {
		t.Errorf("Song: got %q, want %q", decoded.Song, original.Song)
	}
	if decoded.Elapsed != original.Elapsed {
		t.Errorf("Elapsed: got %d, want %d", decoded.Elapsed, original.Elapsed)
	}
	if decoded.ETA != original.ETA {
		t.Errorf("ETA: got %d, want %d", decoded.ETA, original.ETA)
	}
}

func TestStatusJSONWithError(t *testing.T) {
	original := Status{
		Status:  "error",
		Step:    "error",
		Song:    "broken_song",
		Elapsed: 10,
		Error:   "something went wrong",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded Status
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.Status != "error" {
		t.Errorf("Status: got %q, want 'error'", decoded.Status)
	}
	if decoded.Error != "something went wrong" {
		t.Errorf("Error: got %q, want 'something went wrong'", decoded.Error)
	}
}

func TestStatusJSONOmitsEmptyError(t *testing.T) {
	status := Status{
		Status:   "done",
		Progress: 1.0,
		Step:     "done",
		Song:     "song",
	}

	data, _ := json.Marshal(status)
	jsonStr := string(data)
	// The omitempty tag should prevent "error" field from appearing
	if strings.Contains(jsonStr, `"error"`) {
		t.Errorf("JSON should not include 'error' field when empty: %s", jsonStr)
	}
}

// TestSkipStemSeparation checks the skip logic
func TestSkipStemSeparation(t *testing.T) {
	// With stem model set, should not skip
	flags := &cli.PipelineFlags{
		Preset:     "balance",
		StemModel:  "htdemucs_ft",
		Input:      "/tmp/test.wav",
		VocalModel: "polarformer",
	}
	p := New(flags)
	if p.skipStemSeparation() {
		t.Error("skipStemSeparation() should return false when StemModel is set")
	}

	// Without stem model, should skip
	flags2 := &cli.PipelineFlags{
		Preset:     "ultimate",
		StemModel:  "",
		Input:      "/tmp/test.wav",
		VocalModel: "polarformer",
	}
	p2 := New(flags2)
	if !p2.skipStemSeparation() {
		t.Error("skipStemSeparation() should return true when StemModel is empty")
	}
}

// TestSkipDedicatedStems checks the dedicated stems skip logic
func TestSkipDedicatedStems(t *testing.T) {
	// No dedicated models → skip
	flags := &cli.PipelineFlags{
		Preset:     "balance",
		Input:      "/tmp/test.wav",
		VocalModel: "polarformer",
	}
	p := New(flags)
	if !p.skipDedicatedStems() {
		t.Error("skipDedicatedStems() should return true when no dedicated models")
	}

	// With drums model → don't skip
	flags2 := &cli.PipelineFlags{
		Preset:      "ultimate",
		Input:       "/tmp/test.wav",
		VocalModel:  "polarformer",
		DrumsModel:  "htdemucs_drums",
		BassModel:   "htdemucs_bass",
		OtherModel:  "viperx_other",
	}
	p2 := New(flags2)
	if p2.skipDedicatedStems() {
		t.Error("skipDedicatedStems() should return false when dedicated models are set")
	}
}

// TestHasStem checks the hasStem helper
func TestHasStem(t *testing.T) {
	flags := &cli.PipelineFlags{
		StemKeep: []string{"drums", "bass"},
		Input:    "/tmp/test.wav",
		Preset:   "balance",
	}
	p := New(flags)

	if !p.hasStem("drums") {
		t.Error("hasStem('drums') should be true")
	}
	if !p.hasStem("bass") {
		t.Error("hasStem('bass') should be true")
	}
	if p.hasStem("other") {
		t.Error("hasStem('other') should be false")
	}
	if p.hasStem("vocals") {
		t.Error("hasStem('vocals') should be false")
	}
}

// TestHasStemAll checks that "all" returns true for every stem
func TestHasStemAll(t *testing.T) {
	flags := &cli.PipelineFlags{
		StemKeep: []string{"all"},
		Input:    "/tmp/test.wav",
		Preset:   "balance",
	}
	p := New(flags)

	for _, stem := range []string{"drums", "bass", "other", "vocals"} {
		if !p.hasStem(stem) {
			t.Errorf("hasStem(%q) should be true when StemKeep contains 'all'", stem)
		}
	}
}

// TestHasStemEmpty checks that empty StemKeep means keep everything
func TestHasStemEmpty(t *testing.T) {
	flags := &cli.PipelineFlags{
		Input:  "/tmp/test.wav",
		Preset: "balance",
	}
	p := New(flags)

	if !p.hasStem("drums") {
		t.Error("hasStem('drums') should be true when StemKeep is empty")
	}
}

// TestCollectPitchStems checks pitch stem collection logic
func TestCollectPitchStems(t *testing.T) {
	flags := &cli.PipelineFlags{
		Preset:       "balance",
		VocalModel:   "polarformer",
		VocalOverlap: 4,
		VocalKeep:    "both",
		StemModel:    "htdemucs_ft",
		Input:        "/tmp/test.wav",
	}
	p := New(flags)

	stems := p.collectPitchStems()
	// With vocal-keep=both and stem model set, should include vocals, bass, other
	if len(stems) == 0 {
		t.Fatal("collectPitchStems() should return at least vocals")
	}
	foundVocals := false
	for _, s := range stems {
		if s == "vocals.wav" {
			foundVocals = true
			break
		}
	}
	if !foundVocals {
		t.Error("collectPitchStems() should include vocals.wav")
	}
}

// TestRunDockerNotRunning tests that Run fails if Docker isn't reachable
func TestRunDockerNotRunning(t *testing.T) {
	// Check if Docker is available, skip if not
	cmd := exec.Command("docker", "info")
	if err := cmd.Run(); err != nil {
		t.Skip("Docker not available, skipping integration test")
	}

	// If Docker IS available, we still can't run the full pipeline
	// because it needs a container named "onda" with specific images.
	// We test that the pipeline at least starts and fails gracefully
	// when it tries to write the status file and then docker exec.
	flags := &cli.PipelineFlags{
		Preset:       "balance",
		VocalModel:   "polarformer",
		VocalOverlap: 4,
		VocalKeep:    "both",
		StemModel:    "htdemucs_ft",
		Input:        "/tmp/test_does_not_exist.wav",
	}

	err := Run(flags)
	if err == nil {
		t.Fatal("expected error when running pipeline without real input, got nil")
	}
	// The error should mention something about creating output directory or docker exec
	t.Logf("Got expected error: %v", err)
}

// TestWriteStatus tests the writeStatus method (requires temp dir)
func TestWriteStatus(t *testing.T) {
	flags := &cli.PipelineFlags{
		Preset:     "balance",
		Input:      "/tmp/test.wav",
		VocalModel: "polarformer",
	}
	p := New(flags)

	// Write a status
	p.writeStatus("running", 0.5, "vocals")

	// Read it back
	data, err := os.ReadFile(StatusFile())
	if err != nil {
		t.Fatalf("failed to read status file: %v", err)
	}

	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatalf("failed to unmarshal status: %v", err)
	}

	if status.Status != "running" {
		t.Errorf("expected status 'running', got %q", status.Status)
	}
	if status.Progress != 0.5 {
		t.Errorf("expected progress 0.5, got %f", status.Progress)
	}
	if status.Step != "vocals" {
		t.Errorf("expected step 'vocals', got %q", status.Step)
	}
	if status.Song != "test" {
		t.Errorf("expected song 'test', got %q", status.Song)
	}
}
