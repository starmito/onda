package cli

import (
	"strings"
	"testing"
)

// Helper to create flags with just the validated fields set.
func validFlags() *PipelineFlags {
	return &PipelineFlags{
		Preset:       "balance",
		VocalModel:   "polarformer",
		VocalOverlap: 4,
		VocalKeep:    "both",
		StemModel:    "htdemucs_ft",
		Input:        "/tmp/test.wav",
	}
}

func TestParsePipelineFlagsDefault(t *testing.T) {
	// No args — should use "balance" preset defaults
	flags, err := ParsePipelineFlags([]string{"--input", "/tmp/test.wav"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Preset != "balance" {
		t.Errorf("expected preset 'balance', got %q", flags.Preset)
	}
	if flags.VocalModel != "polarformer" {
		t.Errorf("expected vocal model 'polarformer', got %q", flags.VocalModel)
	}
	if flags.VocalOverlap != 4 {
		t.Errorf("expected overlap 4, got %d", flags.VocalOverlap)
	}
	if flags.StemModel != "htdemucs_ft" {
		t.Errorf("expected stem model 'htdemucs_ft', got %q", flags.StemModel)
	}
	if flags.Input != "/tmp/test.wav" {
		t.Errorf("expected input '/tmp/test.wav', got %q", flags.Input)
	}
}

func TestParsePipelineFlagsTurbo(t *testing.T) {
	flags, err := ParsePipelineFlags([]string{"--preset", "turbo", "--input", "/tmp/test.wav"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Preset != "turbo" {
		t.Errorf("expected preset 'turbo', got %q", flags.Preset)
	}
	if flags.VocalModel != "melband_kj" {
		t.Errorf("expected vocal model 'melband_kj', got %q", flags.VocalModel)
	}
	if flags.VocalOverlap != 2 {
		t.Errorf("expected overlap 2, got %d", flags.VocalOverlap)
	}
	if flags.StemModel != "htdemucs_ft" {
		t.Errorf("expected stem model 'htdemucs_ft', got %q", flags.StemModel)
	}
}

func TestParsePipelineFlagsMaster(t *testing.T) {
	flags, err := ParsePipelineFlags([]string{"--preset", "master", "--input", "/tmp/test.wav"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Preset != "master" {
		t.Errorf("expected preset 'master', got %q", flags.Preset)
	}
	if flags.VocalModel != "polarformer" {
		t.Errorf("expected vocal model 'polarformer', got %q", flags.VocalModel)
	}
	if flags.VocalOverlap != 8 {
		t.Errorf("expected overlap 8, got %d", flags.VocalOverlap)
	}
	if flags.StemModel != "htdemucs_ft" {
		t.Errorf("expected stem model 'htdemucs_ft', got %q", flags.StemModel)
	}
	if flags.BassModel != "htdemucs_bass" {
		t.Errorf("expected bass model 'htdemucs_bass', got %q", flags.BassModel)
	}
}

func TestParsePipelineFlagsUltimate(t *testing.T) {
	flags, err := ParsePipelineFlags([]string{"--preset", "ultimate", "--input", "/tmp/test.wav"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Preset != "ultimate" {
		t.Errorf("expected preset 'ultimate', got %q", flags.Preset)
	}
	if flags.VocalModel != "polarformer" {
		t.Errorf("expected vocal model 'polarformer', got %q", flags.VocalModel)
	}
	if flags.VocalOverlap != 8 {
		t.Errorf("expected overlap 8, got %d", flags.VocalOverlap)
	}
	if flags.DrumsModel != "htdemucs_drums" {
		t.Errorf("expected drums model 'htdemucs_drums', got %q", flags.DrumsModel)
	}
	if flags.BassModel != "htdemucs_bass" {
		t.Errorf("expected bass model 'htdemucs_bass', got %q", flags.BassModel)
	}
	if flags.OtherModel != "viperx_other" {
		t.Errorf("expected other model 'viperx_other', got %q", flags.OtherModel)
	}
}

func TestParsePipelineFlagsOverride(t *testing.T) {
	// Override vocal model on turbo preset
	flags, err := ParsePipelineFlags([]string{
		"--preset", "turbo",
		"--vocal-model", "viperx",
		"--input", "/tmp/test.wav",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Preset != "turbo" {
		t.Errorf("expected preset 'turbo', got %q", flags.Preset)
	}
	if flags.VocalModel != "viperx" {
		t.Errorf("expected vocal model 'viperx' (overridden), got %q", flags.VocalModel)
	}
	if flags.VocalOverlap != 2 {
		t.Errorf("expected overlap 2 (from preset, not overridden), got %d", flags.VocalOverlap)
	}
}

func TestParsePipelineFlagsLegacy(t *testing.T) {
	// Use legacy --viperx and --demucs flags
	flags, err := ParsePipelineFlags([]string{
		"--viperx",
		"--demucs",
		"--input", "/tmp/test.wav",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Preset != "balance" {
		t.Errorf("expected preset 'balance' (default), got %q", flags.Preset)
	}
	if flags.VocalModel != "viperx" {
		t.Errorf("expected vocal model 'viperx' (from --viperx), got %q", flags.VocalModel)
	}
	if flags.StemModel != "htdemucs_ft" {
		t.Errorf("expected stem model 'htdemucs_ft' (from --demucs), got %q", flags.StemModel)
	}
}

func TestParsePipelineFlagsLegacyWithOverride(t *testing.T) {
	// --viperx should be overridden by explicit --vocal-model
	flags, err := ParsePipelineFlags([]string{
		"--viperx",
		"--vocal-model", "melband_kj",
		"--input", "/tmp/test.wav",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.VocalModel != "melband_kj" {
		t.Errorf("expected 'melband_kj' (explicit override wins), got %q", flags.VocalModel)
	}
}

func TestValidateMissingInput(t *testing.T) {
	// PipelineFlags without input should fail Validate
	flags := &PipelineFlags{
		Preset:       "balance",
		VocalModel:   "polarformer",
		VocalOverlap: 4,
		VocalKeep:    "both",
	}
	err := flags.Validate()
	if err == nil {
		t.Fatal("expected error for missing --input, got nil")
	}
	if !strings.Contains(err.Error(), "--input is required") {
		t.Errorf("expected error about --input, got: %v", err)
	}
}

func TestValidateInvalidPreset(t *testing.T) {
	flags := &PipelineFlags{
		Preset:       "nonexistent",
		VocalModel:   "polarformer",
		VocalKeep:    "both",
		Input:        "/tmp/test.wav",
	}
	err := flags.Validate()
	if err == nil {
		t.Fatal("expected error for invalid preset, got nil")
	}
	if !strings.Contains(err.Error(), "unknown preset") {
		t.Errorf("expected error about unknown preset, got: %v", err)
	}
}

func TestValidateInvalidVocalKeep(t *testing.T) {
	flags := &PipelineFlags{
		Preset:       "balance",
		VocalModel:   "polarformer",
		VocalOverlap: 4,
		VocalKeep:    "invalid",
		Input:        "/tmp/test.wav",
	}
	err := flags.Validate()
	if err == nil {
		t.Fatal("expected error for invalid --vocal-keep, got nil")
	}
	if !strings.Contains(err.Error(), "invalid") || !strings.Contains(err.Error(), "--vocal-keep") {
		t.Errorf("expected error about invalid --vocal-keep, got: %v", err)
	}
}

func TestValidateInvalidStemKeep(t *testing.T) {
	flags := &PipelineFlags{
		Preset:       "balance",
		VocalModel:   "polarformer",
		VocalOverlap: 4,
		VocalKeep:    "both",
		StemKeep:     []string{"invalid_stem"},
		Input:        "/tmp/test.wav",
	}
	err := flags.Validate()
	if err == nil {
		t.Fatal("expected error for invalid stem in --stem-keep, got nil")
	}
	if !strings.Contains(err.Error(), "invalid stem") {
		t.Errorf("expected error about invalid stem, got: %v", err)
	}
}

func TestValidateEmptyVocalModel(t *testing.T) {
	flags := &PipelineFlags{
		Preset:       "balance",
		VocalModel:   "",
		VocalOverlap: 4,
		VocalKeep:    "both",
		Input:        "/tmp/test.wav",
	}
	err := flags.Validate()
	if err == nil {
		t.Fatal("expected error for empty vocal model, got nil")
	}
	if !strings.Contains(err.Error(), "vocal model could not be resolved") {
		t.Errorf("expected error about vocal model resolution, got: %v", err)
	}
}

func TestResolvePresetTurbo(t *testing.T) {
	flags := &PipelineFlags{Preset: "turbo", Input: "/tmp/test.wav"}
	if err := flags.ResolvePreset(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.VocalModel != "melband_kj" {
		t.Errorf("expected 'melband_kj', got %q", flags.VocalModel)
	}
}

func TestResolvePresetInvalid(t *testing.T) {
	flags := &PipelineFlags{Preset: "foobar"}
	err := flags.ResolvePreset()
	if err == nil {
		t.Fatal("expected error for invalid preset, got nil")
	}
	if !strings.Contains(err.Error(), "unknown preset") {
		t.Errorf("expected 'unknown preset' in error, got: %v", err)
	}
}

func TestHelp(t *testing.T) {
	help := Help()
	// Should mention preset names
	for _, name := range []string{"turbo", "balance", "master", "ultimate"} {
		if !strings.Contains(help, name) {
			t.Errorf("Help() should mention preset %q", name)
		}
	}
	// Should mention modern flags
	for _, flag := range []string{"--preset", "--vocal-model", "--vocal-keep", "--input", "--output"} {
		if !strings.Contains(help, flag) {
			t.Errorf("Help() should mention flag %q", flag)
		}
	}
	// Should mention legacy flags
	for _, flag := range []string{"--viperx", "--demucs", "--rubberband"} {
		if !strings.Contains(help, flag) {
			t.Errorf("Help() should mention legacy flag %q", flag)
		}
	}
}

func TestParsePipelineFlagsOutput(t *testing.T) {
	flags, err := ParsePipelineFlags([]string{"--input", "/tmp/test.wav", "--output", "/custom/output"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Output != "/custom/output" {
		t.Errorf("expected output '/custom/output', got %q", flags.Output)
	}
}

func TestParsePipelineFlagsStemKeep(t *testing.T) {
	flags, err := ParsePipelineFlags([]string{
		"--input", "/tmp/test.wav",
		"--stem-keep", "drums,bass",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(flags.StemKeep) != 2 {
		t.Fatalf("expected 2 stem-keep items, got %v", flags.StemKeep)
	}
	if flags.StemKeep[0] != "drums" {
		t.Errorf("expected first stem 'drums', got %q", flags.StemKeep[0])
	}
	if flags.StemKeep[1] != "bass" {
		t.Errorf("expected second stem 'bass', got %q", flags.StemKeep[1])
	}
}

func TestParsePipelineFlagsPitch(t *testing.T) {
	flags, err := ParsePipelineFlags([]string{
		"--input", "/tmp/test.wav",
		"--pitch", "3",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Pitch != 3 {
		t.Errorf("expected pitch 3, got %d", flags.Pitch)
	}
}
