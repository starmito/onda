package api

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/starmito/onda/internal/cli"
)

func TestSeparadorCompletoPresetRouting(t *testing.T) {
	preset, ok := cli.Presets["Separador Completo"]
	if !ok {
		t.Fatal("Separador Completo preset not found")
	}
	if len(preset.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(preset.Steps))
	}

	vocal := preset.Steps[0]
	if vocal.ID != "vocal" {
		t.Errorf("expected first step id vocal, got %q", vocal.ID)
	}
	if vocal.Stems["vocals"] != (cli.StemRoute{Action: cli.StemSave, Target: "result"}) {
		t.Errorf("expected vocals save-to-result, got %+v", vocal.Stems["vocals"])
	}
	if vocal.Stems["instrumental"] != (cli.StemRoute{Action: cli.ActionRoute, Target: "step:demucs"}) {
		t.Errorf("expected instrumental route-to-demucs, got %+v", vocal.Stems["instrumental"])
	}

	demucs := preset.Steps[1]
	if demucs.ID != "demucs" {
		t.Errorf("expected second step id demucs, got %q", demucs.ID)
	}
	for _, stem := range []string{"drums", "bass", "other"} {
		if demucs.Stems[stem] != (cli.StemRoute{Action: cli.StemSave, Target: "result"}) {
			t.Errorf("expected %s save-to-result, got %+v", stem, demucs.Stems[stem])
		}
	}
	if demucs.Stems["vocals"] != (cli.StemRoute{Action: cli.StemDiscard}) {
		t.Errorf("expected demucs vocals discard, got %+v", demucs.Stems["vocals"])
	}
}

func TestBuildStepPipelineArgs_SeparadorCompletoDemucsKeepsOnlyInstruments(t *testing.T) {
	setTestRoot(t, "pipeline-complete-")

	demucs := cli.PipelineStep{
		ID:      "demucs",
		Type:    "demucs",
		Model:   "htdemucs_ft",
		Enabled: true,
		Stems: map[string]cli.StemRoute{
			"drums":  {Action: cli.StemSave, Target: "result"},
			"bass":   {Action: cli.StemSave, Target: "result"},
			"other":  {Action: cli.StemSave, Target: "result"},
			"vocals": {Action: cli.StemDiscard},
		},
	}

	args, _, _ := buildStepPipelineArgs(demucs, "/app/input/song.wav", "/app/output/song", "cuda")
	got := argValue(args, "--demucs-keep")
	want := "drums,bass,other"
	if got != want {
		t.Errorf("expected --demucs-keep %q, got %q", want, got)
	}
}

func TestFindChainedInput_PrefersInstrumental(t *testing.T) {
	dir := newTestRoot(t, "pipeline-complete-")
	for _, name := range []string{"vocals.wav", "instrumental.wav", "drums.wav"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fake"), 0o644); err != nil {
			t.Fatalf("failed to create %s: %v", name, err)
		}
	}

	step := cli.PipelineStep{
		ID:   "vocal",
		Type: "vocal",
		Stems: map[string]cli.StemRoute{
			"vocals":       {Action: cli.StemSave, Target: "result"},
			"instrumental": {Action: cli.ActionRoute, Target: "step:demucs"},
		},
	}

	got := findChainedInput(dir, step)
	want := filepath.Join(dir, "instrumental.wav")
	if got != want {
		t.Errorf("findChainedInput = %q, want %q", got, want)
	}
}

func TestCleanupIntermediateStems_KeepsOnlyFinalResult(t *testing.T) {
	dir := newTestRoot(t, "pipeline-complete-")
	files := []string{
		"drums.wav",
		"bass.wav",
		"other.wav",
		"vocals.wav",
		"instrumental.wav",
		"pipeline_status.json",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fake"), 0o644); err != nil {
			t.Fatalf("failed to create %s: %v", name, err)
		}
	}

	steps := []cli.PipelineStep{
		{
			ID:   "vocal",
			Type: "vocal",
			Stems: map[string]cli.StemRoute{
				"vocals":       {Action: cli.StemSave, Target: "result"},
				"instrumental": {Action: cli.ActionRoute, Target: "step:demucs"},
			},
		},
		{
			ID:   "demucs",
			Type: "demucs",
			Stems: map[string]cli.StemRoute{
				"drums":  {Action: cli.StemSave, Target: "result"},
				"bass":   {Action: cli.StemSave, Target: "result"},
				"other":  {Action: cli.StemSave, Target: "result"},
				"vocals": {Action: cli.StemDiscard},
			},
		},
	}

	cleanupIntermediateStems(dir, steps)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}
	var remaining []string
	for _, e := range entries {
		remaining = append(remaining, e.Name())
	}
	slices.Sort(remaining)

	want := []string{"bass.wav", "drums.wav", "other.wav", "pipeline_status.json", "vocals.wav"}
	if !slices.Equal(remaining, want) {
		t.Errorf("remaining files = %v, want %v", remaining, want)
	}

	// The intermediate instrumental stem must be gone.
	if _, err := os.Stat(filepath.Join(dir, "instrumental.wav")); err == nil {
		t.Error("instrumental.wav should have been removed")
	}
}
