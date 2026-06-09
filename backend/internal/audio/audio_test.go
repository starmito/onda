package audio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupportedExtensions(t *testing.T) {
	exts := SupportedExtensions()
	expected := map[string]bool{
		".wav":  true,
		".mp3":  true,
		".flac": true,
		".ogg":  true,
		".m4a":  true,
		".aiff": true,
		".wma":  true,
	}
	for _, ext := range exts {
		if !expected[ext] {
			t.Errorf("unexpected extension %q in SupportedExtensions", ext)
		}
		delete(expected, ext)
	}
	if len(expected) > 0 {
		missing := make([]string, 0, len(expected))
		for ext := range expected {
			missing = append(missing, ext)
		}
		t.Errorf("SupportedExtensions missing: %s", strings.Join(missing, ", "))
	}
}

func TestSupportedExtensionsContainsWav(t *testing.T) {
	exts := SupportedExtensions()
	found := false
	for _, ext := range exts {
		if ext == ".wav" {
			found = true
			break
		}
	}
	if !found {
		t.Error("SupportedExtensions should contain .wav")
	}
}

func TestSupportedExtensionsContainsMp3(t *testing.T) {
	exts := SupportedExtensions()
	found := false
	for _, ext := range exts {
		if ext == ".mp3" {
			found = true
			break
		}
	}
	if !found {
		t.Error("SupportedExtensions should contain .mp3")
	}
}

func TestIsAudioFileWav(t *testing.T) {
	if !IsAudioFile("song.wav") {
		t.Error("IsAudioFile('song.wav') should return true")
	}
	if !IsAudioFile("/path/to/song.WAV") {
		t.Error("IsAudioFile('song.WAV') should return true (case insensitive)")
	}
}

func TestIsAudioFileMp3(t *testing.T) {
	if !IsAudioFile("song.mp3") {
		t.Error("IsAudioFile('song.mp3') should return true")
	}
}

func TestIsAudioFileTxt(t *testing.T) {
	if IsAudioFile("notes.txt") {
		t.Error("IsAudioFile('notes.txt') should return false")
	}
}

func TestIsAudioFileNoExt(t *testing.T) {
	if IsAudioFile("README") {
		t.Error("IsAudioFile('README') should return false")
	}
}

func TestIsAudioFileUnknownExt(t *testing.T) {
	if IsAudioFile("video.mp4") {
		t.Error("IsAudioFile('video.mp4') should return false")
	}
}

func TestCopyFile(t *testing.T) {
	// Create a temporary source file
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "source.txt")
	content := []byte("hello, onda test data!")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	dstPath := filepath.Join(dstDir, "destination.txt")

	// Copy
	if err := CopyFile(srcPath, dstPath); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	// Verify content
	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read destination: %v", err)
	}
	if string(dstContent) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", string(dstContent), string(content))
	}
}

func TestCopyFileOverwrite(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "source.txt")
	if err := os.WriteFile(srcPath, []byte("source data"), 0644); err != nil {
		t.Fatalf("failed to create source: %v", err)
	}

	dstPath := filepath.Join(dstDir, "existing.txt")
	if err := os.WriteFile(dstPath, []byte("old data"), 0644); err != nil {
		t.Fatalf("failed to create destination: %v", err)
	}

	// Overwrite
	if err := CopyFile(srcPath, dstPath); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read destination: %v", err)
	}
	if string(dstContent) != "source data" {
		t.Errorf("expected 'source data', got %q", string(dstContent))
	}
}

func TestCopyFileSourceNotExists(t *testing.T) {
	dstDir := t.TempDir()
	err := CopyFile("/nonexistent/file.wav", filepath.Join(dstDir, "out.wav"))
	if err == nil {
		t.Fatal("expected error for nonexistent source, got nil")
	}
}
