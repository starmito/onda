package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDAWAudioSource_LegacyPaths(t *testing.T) {
	root := setupDAWTestRoot(t)

	// Create a file in the current valid location: data/daw-data/mi_cancion/original.wav
	writeSynthWAV(t, filepath.Join(root, "daw-data", "mi_cancion", "original", "original.wav"), 1.0)

	cases := []struct {
		name     string
		file     string
		wantErr  bool
		wantSong string
	}{
		{"relative daw-data path", "daw-data/mi_cancion/original/original.wav", false, "mi_cancion"},
		{"absolute /daw-data path", "/daw-data/mi_cancion/original/original.wav", false, "mi_cancion"},
		{"absolute /app/data/daw-data path", "/app/data/daw-data/mi_cancion/original/original.wav", false, "mi_cancion"},
		{"absolute /app/daw-data path", "/app/daw-data/mi_cancion/original/original.wav", false, "mi_cancion"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			absPath, safeName, song, _, err := resolveDAWAudioSource(tc.file)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got path %q song %q", absPath, song)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if song != tc.wantSong {
				t.Fatalf("expected song %q, got %q", tc.wantSong, song)
			}
			if _, err := os.Stat(absPath); err != nil {
				t.Fatalf("resolved path does not exist: %q (safeName=%q) err=%v", absPath, safeName, err)
			}
		})
	}
}

func TestResolveDAWAudioSource_LegacyInputPaths(t *testing.T) {
	root := setupDAWTestRoot(t)

	// Create a file in the current valid location: data/input/mi_cancion.wav
	writeSynthWAV(t, filepath.Join(root, "input", "mi_cancion.wav"), 1.0)

	cases := []struct {
		name    string
		file    string
		wantErr bool
	}{
		{"bare name", "mi_cancion.wav", false},
		{"legacy /input/ absolute", "/input/mi_cancion.wav", false},
		{"legacy /app/input/ absolute", "/app/input/mi_cancion.wav", false},
		{"current /app/data/input/ absolute", "/app/data/input/mi_cancion.wav", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			absPath, _, _, _, err := resolveDAWAudioSource(tc.file)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got path %q", absPath)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.file, err)
			}
			if _, err := os.Stat(absPath); err != nil {
				t.Fatalf("resolved path does not exist: %q err=%v", absPath, err)
			}
		})
	}
}
