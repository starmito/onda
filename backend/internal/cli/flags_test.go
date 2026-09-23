package cli

import (
	"strings"
	"testing"
)

func TestParsePipelineFlags_Valid(t *testing.T) {
	args := []string{
		"--input", "/app/data/input/song.wav",
		"--vocal-model", "BS_Roformer_Viperx",
		"--vocal-keep", "both",
		"--stem-keep", "drums,bass,vocals",
		"--pitch", "-2",
		"--output", "/app/data/output",
	}
	flags, err := ParsePipelineFlags(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Input != "/app/data/input/song.wav" {
		t.Errorf("Input = %q, want song.wav path", flags.Input)
	}
	if flags.VocalModel != "BS_Roformer_Viperx" {
		t.Errorf("VocalModel = %q, want BS_Roformer_Viperx", flags.VocalModel)
	}
	if flags.VocalKeep != "both" {
		t.Errorf("VocalKeep = %q, want both", flags.VocalKeep)
	}
	wantStemKeep := []string{"drums", "bass", "vocals"}
	if len(flags.StemKeep) != len(wantStemKeep) {
		t.Fatalf("StemKeep = %v, want %v", flags.StemKeep, wantStemKeep)
	}
	for i, v := range wantStemKeep {
		if flags.StemKeep[i] != v {
			t.Errorf("StemKeep[%d] = %q, want %q", i, flags.StemKeep[i], v)
		}
	}
	if flags.Pitch != -2 {
		t.Errorf("Pitch = %d, want -2", flags.Pitch)
	}
}

func TestParsePipelineFlags_MissingInput(t *testing.T) {
	args := []string{"--vocal-model", "BS_Roformer_Viperx"}
	_, err := ParsePipelineFlags(args)
	if err == nil {
		t.Fatal("expected error for missing input")
	}
	if !strings.Contains(err.Error(), "input") {
		t.Errorf("error = %q, want input mention", err.Error())
	}
}

func TestParsePipelineFlags_MissingVocalModel(t *testing.T) {
	args := []string{"--input", "/app/data/input/song.wav"}
	_, err := ParsePipelineFlags(args)
	if err == nil {
		t.Fatal("expected error for missing vocal model")
	}
	if !strings.Contains(err.Error(), "vocal model") {
		t.Errorf("error = %q, want vocal model mention", err.Error())
	}
}

func TestParsePipelineFlags_InvalidVocalKeep(t *testing.T) {
	args := []string{
		"--input", "/app/data/input/song.wav",
		"--vocal-model", "BS_Roformer_Viperx",
		"--vocal-keep", "all",
	}
	_, err := ParsePipelineFlags(args)
	if err == nil {
		t.Fatal("expected error for invalid vocal-keep")
	}
	if !strings.Contains(err.Error(), "vocal-keep") {
		t.Errorf("error = %q, want vocal-keep mention", err.Error())
	}
}

func TestParsePipelineFlags_InvalidStemKeep(t *testing.T) {
	args := []string{
		"--input", "/app/data/input/song.wav",
		"--vocal-model", "BS_Roformer_Viperx",
		"--stem-keep", "drums,keyboard",
	}
	_, err := ParsePipelineFlags(args)
	if err == nil {
		t.Fatal("expected error for invalid stem-keep")
	}
	if !strings.Contains(err.Error(), "stem-keep") {
		t.Errorf("error = %q, want stem-keep mention", err.Error())
	}
}

func TestParsePipelineFlags_StemKeepAll(t *testing.T) {
	args := []string{
		"--input", "/app/data/input/song.wav",
		"--vocal-model", "BS_Roformer_Viperx",
		"--stem-keep", "all",
	}
	flags, err := ParsePipelineFlags(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(flags.StemKeep) != 1 || flags.StemKeep[0] != "all" {
		t.Errorf("StemKeep = %v, want [all]", flags.StemKeep)
	}
}

func TestParsePipelineFlags_WhitespaceInStemKeep(t *testing.T) {
	args := []string{
		"--input", "/app/data/input/song.wav",
		"--vocal-model", "BS_Roformer_Viperx",
		"--stem-keep", " drums , bass ",
	}
	flags, err := ParsePipelineFlags(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"drums", "bass"}
	if len(flags.StemKeep) != len(want) {
		t.Fatalf("StemKeep = %v, want %v", flags.StemKeep, want)
	}
	for i, v := range want {
		if flags.StemKeep[i] != v {
			t.Errorf("StemKeep[%d] = %q, want %q", i, flags.StemKeep[i], v)
		}
	}
}

func TestParsePipelineFlags_ExplicitZeroPitch(t *testing.T) {
	args := []string{
		"--input", "/app/data/input/song.wav",
		"--vocal-model", "BS_Roformer_Viperx",
		"--pitch", "0",
	}
	flags, err := ParsePipelineFlags(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags.Pitch != 0 {
		t.Errorf("Pitch = %d, want 0", flags.Pitch)
	}
}

func TestParsePipelineFlags_InvalidFlag(t *testing.T) {
	args := []string{
		"--input", "/app/data/input/song.wav",
		"--vocal-model", "BS_Roformer_Viperx",
		"--unknown-flag", "value",
	}
	_, err := ParsePipelineFlags(args)
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestHelpContainsFlags(t *testing.T) {
	text := Help()
	for _, want := range []string{"--input", "--vocal-model", "--stem-keep", "--pitch"} {
		if !strings.Contains(text, want) {
			t.Errorf("help text missing %q", want)
		}
	}
}

func TestValidate_Valid(t *testing.T) {
	f := &PipelineFlags{
		Input:      "/app/data/input/song.wav",
		VocalModel: "BS_Roformer_Viperx",
		VocalKeep:  "instrumental",
		StemKeep:   []string{"drums", "bass"},
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_EmptyInput(t *testing.T) {
	f := &PipelineFlags{
		VocalModel: "BS_Roformer_Viperx",
	}
	if err := f.Validate(); err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestValidate_EmptyVocalModel(t *testing.T) {
	f := &PipelineFlags{
		Input: "/app/data/input/song.wav",
	}
	if err := f.Validate(); err == nil {
		t.Fatal("expected error for empty vocal model")
	}
}

func TestValidate_StemKeepWithSpaces(t *testing.T) {
	f := &PipelineFlags{
		Input:      "/app/data/input/song.wav",
		VocalModel: "BS_Roformer_Viperx",
		StemKeep:   []string{"drums", " guitar"},
	}
	if err := f.Validate(); err == nil {
		t.Fatal("expected error for invalid stem keep value")
	}
}
