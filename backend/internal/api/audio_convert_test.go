package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupAudioConvertTestRoot creates a temporary project root and sets ONDA_ROOT
// so findProjectRoot() resolves to it during the test.
func setupAudioConvertTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(".", "audio-convert-test-")
	if err != nil {
		t.Fatalf("failed to create test root: %v", err)
	}
	t.Setenv("ONDA_ROOT", root)
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	for _, dir := range []string{"input", "daw-data", "daw-data/tmp"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	return root
}

func TestDecodeAudioToPCMWav_PCMWAVUsesOriginal(t *testing.T) {
	root := setupAudioConvertTestRoot(t)
	wavPath := filepath.Join(root, "daw-data", "test.wav")
	writeSynthWAV(t, wavPath, 0.5)

	tmpPath, cleanup, err := decodeAudioToPCMWav(wavPath, "test")
	if err != nil {
		t.Fatalf("unexpected error for WAV PCM: %v", err)
	}
	defer cleanup()

	if tmpPath != wavPath {
		t.Fatalf("expected original path %q, got %q", wavPath, tmpPath)
	}

	// Cleanup must be a no-op for an original WAV file.
	cleanup()
	if _, err := os.Stat(wavPath); err != nil {
		t.Fatalf("cleanup removed the original WAV file: %v", err)
	}
}

func TestDecodeAudioToPCMWav_NonWAVReturnsError(t *testing.T) {
	root := setupAudioConvertTestRoot(t)
	fakePath := filepath.Join(root, "daw-data", "fake.mp3")
	if err := os.WriteFile(fakePath, []byte("not an audio file"), 0o644); err != nil {
		t.Fatalf("failed to write fake mp3: %v", err)
	}

	_, cleanup, err := decodeAudioToPCMWav(fakePath, "test")
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("expected error for non-WAV file, got nil")
	}

	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "ffmpeg") && !strings.Contains(msg, "decode") && !strings.Contains(msg, "convert") {
		t.Fatalf("expected controlled conversion error, got: %v", err)
	}
}

func TestCodecAndArgsForExt(t *testing.T) {
	cases := []struct {
		ext       string
		wantCodec string
		wantArgs  []string
	}{
		{".mp3", "libmp3lame", []string{"-b:a", "192k"}},
		{".flac", "flac", []string{"-compression_level", "5"}},
		{".ogg", "pcm_s16le", nil},
	}
	for _, tc := range cases {
		codec, args := codecAndArgsForExt(tc.ext)
		if codec != tc.wantCodec {
			t.Errorf("codecAndArgsForExt(%q) codec = %q, want %q", tc.ext, codec, tc.wantCodec)
		}
		if len(args) != len(tc.wantArgs) {
			t.Errorf("codecAndArgsForExt(%q) args = %v, want %v", tc.ext, args, tc.wantArgs)
			continue
		}
		for i := range args {
			if args[i] != tc.wantArgs[i] {
				t.Errorf("codecAndArgsForExt(%q) args[%d] = %q, want %q", tc.ext, i, args[i], tc.wantArgs[i])
			}
		}
	}
}
