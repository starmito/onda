package api

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-audio/audio"
)

// decodeAudioToPCMWav decodes an audio file to a WAV PCM file readable by
// readWAV. If the file is already a readable WAV PCM, it returns the original
// path and a no-op cleanup function. Otherwise it decodes the file with ffmpeg
// to a temporary WAV inside daw-data/{song}/tmp/ and returns a cleanup function
// that removes that temporary file.
func decodeAudioToPCMWav(path string, song string) (tmpPath string, cleanup func(), err error) {
	nopCleanup := func() {}

	// First try to read the file directly as a WAV PCM.
	if _, _, err := readWAV(path); err == nil {
		return path, nopCleanup, nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".wav" {
		return "", nopCleanup, fmt.Errorf("input WAV is not a readable PCM file")
	}

	if _, lookErr := exec.LookPath("ffmpeg"); lookErr != nil {
		return "", nopCleanup, fmt.Errorf("input format %q requires ffmpeg, which is not installed", ext)
	}

	projectRoot := findProjectRoot()
	tmpDir := songTempDir(projectRoot, song)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", nopCleanup, fmt.Errorf("failed to create tmp dir: %w", err)
	}

	base := filepath.Base(path)
	tmpPath = filepath.Join(tmpDir, fmt.Sprintf("decode_%d_%s.wav", time.Now().UnixNano(), base))

	args := []string{
		"-y",
		"-i", path,
		"-acodec", "pcm_s16le",
		tmpPath,
	}
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", nopCleanup, fmt.Errorf("ffmpeg decode failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}

	cleanup = func() {
		_ = os.Remove(tmpPath)
	}
	return tmpPath, cleanup, nil
}

// writeAudioFile writes PCM data to path, preserving the format implied by the
// file extension. WAV files are written directly; FLAC and MP3 are produced by
// encoding a temporary WAV with ffmpeg.
func writeAudioFile(path string, buf *audio.IntBuffer, wf *wavFormat, song string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".wav" {
		return writeWAV(path, buf, wf)
	}
	return writeWAVAndEncode(path, buf, wf, song)
}

// writeWAVAndEncode writes the buffer to a temporary WAV and converts it to the
// target format with ffmpeg.
func writeWAVAndEncode(outputPath string, buf *audio.IntBuffer, wf *wavFormat, song string) error {
	projectRoot := findProjectRoot()
	tmpDir := songTempDir(projectRoot, song)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("failed to create tmp dir: %w", err)
	}

	tmpWav := filepath.Join(tmpDir, fmt.Sprintf("encode_%d_%s.wav", time.Now().UnixNano(), filepath.Base(outputPath)))
	if err := writeWAV(tmpWav, buf, wf); err != nil {
		return fmt.Errorf("failed to write intermediate WAV: %w", err)
	}
	defer func() { _ = os.Remove(tmpWav) }()

	codec, extraArgs := codecAndArgsForExt(filepath.Ext(outputPath))
	args := []string{
		"-y",
		"-i", tmpWav,
		"-codec:a", codec,
	}
	args = append(args, extraArgs...)
	args = append(args, outputPath)

	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg encode failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// codecAndArgsForExt returns the ffmpeg audio codec and any extra arguments for
// the given file extension.
func codecAndArgsForExt(ext string) (string, []string) {
	switch strings.ToLower(ext) {
	case ".mp3":
		return "libmp3lame", []string{"-b:a", "192k"}
	case ".flac":
		return "flac", []string{"-compression_level", "5"}
	default:
		return "pcm_s16le", nil
	}
}
