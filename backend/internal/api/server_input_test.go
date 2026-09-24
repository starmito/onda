package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/starmito/onda/internal/cli"
)

func TestNormalizeContainerInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "relative filename becomes container input path",
			input: "fiesta_pagana.flac",
			want:  filepath.Join(mustSub("input"), "fiesta_pagana.flac"),
		},
		{
			name:  "legacy container input path is normalized to data root",
			input: "/app/input/fiesta_pagana.flac",
			want:  filepath.Join(mustSub("input"), "fiesta_pagana.flac"),
		},
		{
			name:  "other absolute path is preserved",
			input: "/home/user/music/fiesta_pagana.flac",
			want:  "/home/user/music/fiesta_pagana.flac",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeContainerInput(tt.input)
			if got != tt.want {
				t.Errorf("normalizeContainerInput(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildPipelineArgs_NormalizesRelativeInput(t *testing.T) {
	expectedInput := filepath.Join(mustSub("input"), "fiesta_pagana.flac")
	req := &SeparateRequest{
		Input: "fiesta_pagana.flac",
	}
	song, args, steps, _, _ := buildPipelineArgs(req)
	if song != "fiesta_pagana" {
		t.Errorf("expected song fiesta_pagana, got %q", song)
	}
	if len(steps) != 0 {
		t.Errorf("old format should not return steps, got %d", len(steps))
	}
	if !contains(args, expectedInput) {
		t.Errorf("expected args to contain normalized input path, got %v", args)
	}
	if req.Input != expectedInput {
		t.Errorf("expected request input to be normalized, got %q", req.Input)
	}
}

func TestBuildPipelineArgs_NormalizesContainerInput(t *testing.T) {
	expectedInput := filepath.Join(mustSub("input"), "fiesta_pagana.flac")
	req := &SeparateRequest{
		Input: "/app/input/fiesta_pagana.flac",
	}
	song, args, steps, _, _ := buildPipelineArgs(req)
	if song != "fiesta_pagana" {
		t.Errorf("expected song fiesta_pagana, got %q", song)
	}
	if len(steps) != 0 {
		t.Errorf("old format should not return steps, got %d", len(steps))
	}
	if !contains(args, expectedInput) {
		t.Errorf("expected args to contain normalized input path %q, got %v", expectedInput, args)
	}
	if req.Input != expectedInput {
		t.Errorf("expected request input to be normalized, got %q", req.Input)
	}
}

func TestBuildPipelineArgs_KeepsOtherAbsoluteInput(t *testing.T) {
	req := &SeparateRequest{
		Input: "/home/user/music/fiesta_pagana.flac",
	}
	song, args, steps, _, _ := buildPipelineArgs(req)
	if song != "fiesta_pagana" {
		t.Errorf("expected song fiesta_pagana, got %q", song)
	}
	if len(steps) != 0 {
		t.Errorf("old format should not return steps, got %d", len(steps))
	}
	last := args[len(args)-1]
	if last != "/home/user/music/fiesta_pagana.flac" {
		t.Errorf("expected last arg to be /home/user/music/fiesta_pagana.flac, got %q", last)
	}
}

func TestBuildPipelineArgs_MultiStepNormalizesInput(t *testing.T) {
	root := setTestRoot(t, "input-test-")
	modelDir := filepath.Join(root, "models", "VR_Models", "BS_Roformer_Viperx")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "BS_Roformer_Viperx.ckpt"), []byte("fake"), 0o644); err != nil {
		t.Fatalf("failed to create dummy checkpoint: %v", err)
	}

	expectedInput := filepath.Join(mustSub("input"), "fiesta_pagana.flac")
	req := &SeparateRequest{
		Input: "fiesta_pagana.flac",
		Steps: []cli.PipelineStep{
			{ID: "vocal", Type: "vocal", Enabled: true, Model: "BS_Roformer_Viperx"},
		},
	}
	_, args, _, _, _ := buildPipelineArgs(req)
	if !contains(args, expectedInput) {
		t.Errorf("expected multi-step args to contain normalized input path, got %v", args)
	}
	if req.Input != expectedInput {
		t.Errorf("expected request input to be normalized for multi-step, got %q", req.Input)
	}
}

func TestBuildPipelineArgs_UsesOutputOverride(t *testing.T) {
	req := &SeparateRequest{
		Input:  "/app/input/fiesta_pagana.flac",
		Output: "fiesta_pagana (copia01)",
	}
	song, args, _, _, _ := buildPipelineArgs(req)
	if song != "fiesta_pagana" {
		t.Errorf("expected song fiesta_pagana, got %q", song)
	}
	expectedOutput := filepath.Join(mustSub("output"), "fiesta_pagana (copia01)")
	if !contains(args, "--output") {
		t.Errorf("expected args to contain --output flag, got %v", args)
	}
	if !contains(args, expectedOutput) {
		t.Errorf("expected args to contain output override %q, got %v", expectedOutput, args)
	}
}

func TestBuildPipelineArgs_IgnoresTraversalOutputOverride(t *testing.T) {
	req := &SeparateRequest{
		Input:  "/app/input/fiesta_pagana.flac",
		Output: "../outside",
	}
	song, args, _, _, _ := buildPipelineArgs(req)
	if song != "fiesta_pagana" {
		t.Errorf("expected song fiesta_pagana, got %q", song)
	}
	expectedOutput := filepath.Join(mustSub("output"), "fiesta_pagana")
	if !contains(args, expectedOutput) {
		t.Errorf("expected traversal override to be ignored, want output %q, got %v", expectedOutput, args)
	}
}
