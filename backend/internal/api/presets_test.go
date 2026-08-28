package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/starmito/onda/internal/cli"
)

func TestGetAllPresets_IncludesBuiltIns(t *testing.T) {
	presets := getAllPresets()
	for _, name := range []string{"Voces Total", "Eliminador de Voz", "Separador Completo", "Solo Instrumentos"} {
		p, ok := presets[name]
		if !ok {
			t.Errorf("missing built-in preset %q", name)
			continue
		}
		if !p.Locked {
			t.Errorf("built-in preset %q should be locked", name)
		}
		if len(p.Steps) == 0 {
			t.Errorf("built-in preset %q should have steps", name)
		}
	}
}

func TestMigratePreset_NewFormat(t *testing.T) {
	newFmt := cli.Preset{
		Name:        "New Preset",
		Description: "has steps",
		Steps: []cli.PipelineStep{
			{ID: "vocal", Type: "vocal", Enabled: true},
		},
	}
	raw, _ := json.Marshal(newFmt)
	got := migratePreset(raw)
	if got.Name != "New Preset" {
		t.Errorf("expected name New Preset, got %q", got.Name)
	}
	if len(got.Steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(got.Steps))
	}
}

func TestMigratePreset_OldFormat(t *testing.T) {
	old := map[string]interface{}{
		"name":          "Old Preset",
		"description":   "converted",
		"viperxEnabled": true,
		"vocalModel":    "BS_Roformer_Viperx",
		"viperxStems":   []string{"vocals"},
		"demucsEnabled": true,
		"stemModel":     "htdemucs_ft",
		"demucsStems":   []string{"drums", "bass"},
		"pitch":         2,
	}
	raw, _ := json.Marshal(old)
	got := migratePreset(raw)

	if got.Name != "Old Preset" {
		t.Errorf("expected name Old Preset, got %q", got.Name)
	}
	if got.Pitch != 2 {
		t.Errorf("expected pitch 2, got %d", got.Pitch)
	}
	if len(got.Steps) != 2 {
		t.Fatalf("expected 2 migrated steps, got %d", len(got.Steps))
	}
	if got.Steps[0].Type != "vocal" {
		t.Errorf("expected first step vocal, got %q", got.Steps[0].Type)
	}
	if got.Steps[1].Type != "demucs" {
		t.Errorf("expected second step demucs, got %q", got.Steps[1].Type)
	}
	if _, ok := got.Steps[0].Stems["vocals"]; !ok {
		t.Errorf("expected vocal step to keep vocals")
	}
	if _, ok := got.Steps[1].Stems["drums"]; !ok {
		t.Errorf("expected demucs step to keep drums")
	}
}

func TestBuildStepPipelineArgs_Vocal(t *testing.T) {
	step := cli.PipelineStep{
		ID:      "vocal",
		Type:    "vocal",
		Model:   "BS_Roformer_Viperx",
		Enabled: true,
		Stems: map[string]cli.StemRoute{
			"vocals":       {Action: cli.StemSave, Target: "result"},
			"instrumental": {Action: cli.StemSave, Target: "result"},
		},
	}
	args := buildStepPipelineArgs(step, "/app/input/song.wav", "/app/output/song", "cpu")
	joined := " " + joinArgs(args) + " "
	if !contains(args, "--vocal-model") {
		t.Error("expected --vocal-model flag")
	}
	if !contains(args, "--vocal-keep") {
		t.Error("expected --vocal-keep flag")
	}
	if !contains(args, "--device") {
		t.Error("expected --device flag")
	}
	if !contains(args, "--output") {
		t.Error("expected --output flag")
	}
	if !contains(args, "both") {
		t.Errorf("expected keep value 'both' in args: %v", args)
	}
	_ = joined
}

func TestBuildStepPipelineArgs_Demucs(t *testing.T) {
	step := cli.PipelineStep{
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
	args := buildStepPipelineArgs(step, "/app/input/song.wav", "/app/output/song", "cuda")
	if !contains(args, "--stem-model") {
		t.Error("expected --stem-model flag")
	}
	if !contains(args, "--demucs-keep") {
		t.Error("expected --demucs-keep flag")
	}
	if contains(args, "--device") {
		t.Error("cuda is the default; should not emit --device")
	}
	keepIdx := indexOf(args, "--demucs-keep")
	if keepIdx < 0 || keepIdx+1 >= len(args) {
		t.Fatal("missing --demucs-keep value")
	}
	keep := args[keepIdx+1]
	if keep != "drums,bass,other" {
		t.Errorf("expected keep drums,bass,other, got %q", keep)
	}
}

func TestBuildPipelineArgs_OldFormat(t *testing.T) {
	req := SeparateRequest{
		Input:       "/app/input/song.wav",
		Viperx:      true,
		ViperxKeep:  "both",
		Demucs:      true,
		DemucsKeep:  []string{"drums", "bass"},
		Pitch:       2,
		VocalModel:  "BS_Roformer_Viperx",
		StemModel:   "htdemucs_ft",
		Device:      "cpu",
	}
	song, args, steps := buildPipelineArgs(req)
	if song != "song" {
		t.Errorf("expected song name song, got %q", song)
	}
	if len(steps) != 0 {
		t.Errorf("old format should not return steps, got %d", len(steps))
	}
	if !contains(args, "--viperx-model") {
		t.Error("expected --viperx-model flag")
	}
	if !contains(args, "--stem-model") {
		t.Error("expected --stem-model flag")
	}
	if !contains(args, "--pitch") {
		t.Error("expected --pitch flag")
	}
	if !contains(args, "--device") {
		t.Error("expected --device flag for non-cuda")
	}
}

func TestBuildPipelineArgs_MultiStepPreset(t *testing.T) {
	req := SeparateRequest{
		Input: "/app/input/song.wav",
		Steps: []cli.PipelineStep{
			{ID: "vocal", Type: "vocal", Enabled: true, Model: "BS_Roformer_Viperx"},
		},
		Device: "cuda",
	}
	song, args, steps := buildPipelineArgs(req)
	if song != "song" {
		t.Errorf("expected song name song, got %q", song)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if contains(args, "--device") {
		t.Error("cuda default should not emit --device")
	}
}

func TestFindChainedInput_RouteTargets(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "vocals.wav"), []byte("vocals"), 0o644); err != nil {
		t.Fatalf("failed to write vocals: %v", err)
	}
	step := cli.PipelineStep{
		Stems: map[string]cli.StemRoute{
			"vocals": {Action: cli.ActionRoute},
		},
	}
	got := findChainedInput(root, step)
	if !strings.HasSuffix(got, "vocals.wav") {
		t.Errorf("expected vocals.wav, got %q", got)
	}
}

func TestToInternalContainerPath(t *testing.T) {
	root := t.TempDir()
	// Override resolveProjectRoot via ONDA_ROOT env var.
	t.Setenv("ONDA_ROOT", root)

	cases := []struct {
		input string
		want  string
	}{
		{filepath.Join(root, "output", "song", "vocals.wav"), "/app/output/song/vocals.wav"},
		{filepath.Join(root, "input", "song.wav"), "/app/input/song.wav"},
		{"/some/random/path", "/some/random/path"},
	}
	for _, c := range cases {
		got := toInternalContainerPath(c.input)
		if got != c.want {
			t.Errorf("toInternalContainerPath(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// helpers
func contains(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

func indexOf(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}

func joinArgs(args []string) string {
	return strings.Join(args, " ")
}
