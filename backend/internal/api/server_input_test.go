package api

import (
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
			want:  "/app/input/fiesta_pagana.flac",
		},
		{
			name:  "full container input path is preserved",
			input: "/app/input/fiesta_pagana.flac",
			want:  "/app/input/fiesta_pagana.flac",
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
	req := &SeparateRequest{
		Input:  "fiesta_pagana.flac",
		Viperx: true,
		Demucs: true,
	}
	song, args, steps := buildPipelineArgs(req)
	if song != "fiesta_pagana" {
		t.Errorf("expected song fiesta_pagana, got %q", song)
	}
	if len(steps) != 0 {
		t.Errorf("old format should not return steps, got %d", len(steps))
	}
	if !contains(args, "/app/input/fiesta_pagana.flac") {
		t.Errorf("expected args to contain normalized input path, got %v", args)
	}
	if req.Input != "/app/input/fiesta_pagana.flac" {
		t.Errorf("expected request input to be normalized, got %q", req.Input)
	}
}

func TestBuildPipelineArgs_KeepsContainerInput(t *testing.T) {
	req := &SeparateRequest{
		Input:  "/app/input/fiesta_pagana.flac",
		Viperx: true,
	}
	song, args, steps := buildPipelineArgs(req)
	if song != "fiesta_pagana" {
		t.Errorf("expected song fiesta_pagana, got %q", song)
	}
	if len(steps) != 0 {
		t.Errorf("old format should not return steps, got %d", len(steps))
	}
	last := args[len(args)-1]
	if last != "/app/input/fiesta_pagana.flac" {
		t.Errorf("expected last arg to be /app/input/fiesta_pagana.flac, got %q", last)
	}
}

func TestBuildPipelineArgs_KeepsOtherAbsoluteInput(t *testing.T) {
	req := &SeparateRequest{
		Input:  "/home/user/music/fiesta_pagana.flac",
		Viperx: true,
	}
	song, args, steps := buildPipelineArgs(req)
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
	req := &SeparateRequest{
		Input: "fiesta_pagana.flac",
		Steps: []cli.PipelineStep{
			{ID: "vocal", Type: "vocal", Enabled: true, Model: "BS_Roformer_Viperx"},
		},
		Device: "cuda",
	}
	_, args, _ := buildPipelineArgs(req)
	if !contains(args, "/app/input/fiesta_pagana.flac") {
		t.Errorf("expected multi-step args to contain normalized input path, got %v", args)
	}
	if req.Input != "/app/input/fiesta_pagana.flac" {
		t.Errorf("expected request input to be normalized for multi-step, got %q", req.Input)
	}
}
