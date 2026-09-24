package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeContainerInput_LegacyPathsResolveToExistingFile(t *testing.T) {
	root := setupDAWTestRoot(t)

	// Create a file at the current valid location under the test data root.
	writeSynthWAV(t, filepath.Join(root, "input", "song.wav"), 1.0)

	cases := []struct {
		name  string
		input string
	}{
		{"bare name", "song.wav"},
		{"legacy /input/ absolute", "/input/song.wav"},
		{"legacy /app/input/ absolute", "/app/input/song.wav"},
		{"current /app/data/input/ absolute", "/app/data/input/song.wav"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeContainerInput(tc.input)
			if _, err := os.Stat(got); err != nil {
				t.Fatalf("normalized path %q does not exist: %v (input was %q)", got, err, tc.input)
			}
		})
	}
}
