package audio

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
)

// writeSineWAV writes a mono 16-bit WAV with a sine tone at the given frequency.
func writeSineWAV(t *testing.T, path string, freq float64, durationSec float64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	const sampleRate = 44100
	numSamples := int(durationSec * sampleRate)
	data := make([]int, numSamples)
	for i := 0; i < numSamples; i++ {
		t := float64(i) / sampleRate
		sample := math.Sin(2*math.Pi*freq*t) * 0.5 * 32767
		data[i] = int(sample)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create wav: %v", err)
	}
	defer f.Close()
	buf := &audio.IntBuffer{Data: data, Format: &audio.Format{SampleRate: sampleRate, NumChannels: 1}}
	enc := wav.NewEncoder(f, sampleRate, 16, 1, 1)
	if err := enc.Write(buf); err != nil {
		t.Fatalf("failed to write wav: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("failed to close wav: %v", err)
	}
}

// writeSilentWAV writes a mono silent WAV of the requested duration.
func writeSilentWAV(t *testing.T, path string, durationSec float64) {
	t.Helper()
	const sampleRate = 44100
	numSamples := int(durationSec * sampleRate)
	data := make([]int, numSamples)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create wav: %v", err)
	}
	defer f.Close()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	buf := &audio.IntBuffer{Data: data, Format: &audio.Format{SampleRate: sampleRate, NumChannels: 1}}
	enc := wav.NewEncoder(f, sampleRate, 16, 1, 1)
	if err := enc.Write(buf); err != nil {
		t.Fatalf("failed to write wav: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("failed to close wav: %v", err)
	}
}

// rms computes the root-mean-square amplitude of a PCM buffer normalized to [-1,1].
func rms(data []int) float64 {
	if len(data) == 0 {
		return 0
	}
	var sum float64
	for _, s := range data {
		n := float64(s) / 32768
		sum += n * n
	}
	return math.Sqrt(sum / float64(len(data)))
}

// readWAVInt16 reads a mono 16-bit WAV file into an int slice.
func readWAVInt16(t *testing.T, path string) []int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open wav: %v", err)
	}
	defer f.Close()
	dec := wav.NewDecoder(f)
	buf, err := dec.FullPCMBuffer()
	if err != nil {
		t.Fatalf("failed to decode wav: %v", err)
	}
	return buf.Data
}

func TestConvertToWav_AlreadyWav(t *testing.T) {
	if !IsFfmpegInstalled() {
		t.Skip("ffmpeg not installed")
	}
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "tone.wav")
	writeSineWAV(t, srcPath, 440, 0.5)

	dstPath := filepath.Join(dstDir, "out.wav")
	if err := ConvertToWav(srcPath, dstPath, 0); err != nil {
		t.Fatalf("ConvertToWav failed: %v", err)
	}
	if _, err := os.Stat(dstPath); err != nil {
		t.Fatalf("output file missing: %v", err)
	}
}

func TestConvertToWav_Resamples(t *testing.T) {
	if !IsFfmpegInstalled() {
		t.Skip("ffmpeg not installed")
	}
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "tone.wav")
	writeSineWAV(t, srcPath, 440, 0.5)

	dstPath := filepath.Join(dstDir, "out.wav")
	if err := ConvertToWav(srcPath, dstPath, 22050); err != nil {
		t.Fatalf("ConvertToWav failed: %v", err)
	}

	dur, err := GetDuration(dstPath)
	if err != nil {
		t.Fatalf("GetDuration failed: %v", err)
	}
	if math.Abs(dur-0.5) > 0.05 {
		t.Errorf("duration = %f, want ~0.5", dur)
	}
}

func TestGetDuration_SyntheticWav(t *testing.T) {
	if _, err := os.Stat("/usr/bin/ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "silent.wav")
	writeSilentWAV(t, path, 1.25)

	dur, err := GetDuration(path)
	if err != nil {
		t.Fatalf("GetDuration failed: %v", err)
	}
	if math.Abs(dur-1.25) > 0.05 {
		t.Errorf("duration = %f, want ~1.25", dur)
	}
}

func TestRubberbandPitch_ZeroCopiesFile(t *testing.T) {
	if _, err := exec.LookPath("rubberband"); err != nil {
		t.Skip("rubberband not installed")
	}
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "tone.wav")
	writeSineWAV(t, srcPath, 440, 0.5)

	dstPath := filepath.Join(dstDir, "copy.wav")
	if err := RubberbandPitch(0, srcPath, dstPath); err != nil {
		t.Fatalf("RubberbandPitch(0) failed: %v", err)
	}

	srcData, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("failed to read source: %v", err)
	}
	dstData, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read destination: %v", err)
	}
	if string(srcData) != string(dstData) {
		t.Error("RubberbandPitch(0) did not produce a byte-identical copy")
	}
}

func TestRubberbandPitch_SilenceRemainsSilent(t *testing.T) {
	if _, err := exec.LookPath("rubberband"); err != nil {
		t.Skip("rubberband not installed")
	}
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "silent.wav")
	writeSilentWAV(t, srcPath, 0.5)

	dstPath := filepath.Join(dstDir, "pitched.wav")
	if err := RubberbandPitch(7, srcPath, dstPath); err != nil {
		t.Fatalf("RubberbandPitch(7) failed: %v", err)
	}

	data := readWAVInt16(t, dstPath)
	if rms(data) > 0.001 {
		t.Errorf("silence produced RMS %f after pitch shift", rms(data))
	}
}

func TestRubberbandPitch_RoundTripApproximate(t *testing.T) {
	if _, err := exec.LookPath("rubberband"); err != nil {
		t.Skip("rubberband not installed")
	}
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "tone.wav")
	writeSineWAV(t, srcPath, 440, 1.0)

	upPath := filepath.Join(dstDir, "up.wav")
	downPath := filepath.Join(dstDir, "down.wav")
	if err := RubberbandPitch(5, srcPath, upPath); err != nil {
		t.Fatalf("pitch up failed: %v", err)
	}
	if err := RubberbandPitch(-5, upPath, downPath); err != nil {
		t.Fatalf("pitch down failed: %v", err)
	}

	// The round-trip should produce a non-silent file with comparable duration.
	dur, err := GetDuration(downPath)
	if err != nil {
		t.Fatalf("GetDuration failed: %v", err)
	}
	if math.Abs(dur-1.0) > 0.15 {
		t.Errorf("round-trip duration = %f, want ~1.0", dur)
	}
	data := readWAVInt16(t, downPath)
	if rms(data) < 0.01 {
		t.Error("round-trip signal is too quiet")
	}
}

func TestRubberbandPitch_SemitoneLimits(t *testing.T) {
	if _, err := exec.LookPath("rubberband"); err != nil {
		t.Skip("rubberband not installed")
	}
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "tone.wav")
	writeSineWAV(t, srcPath, 440, 0.5)

	for _, semis := range []int{-12, 12} {
		dstPath := filepath.Join(dstDir, fmt.Sprintf("pitch_%d.wav", semis))
		if err := RubberbandPitch(semis, srcPath, dstPath); err != nil {
			t.Errorf("RubberbandPitch(%d) failed: %v", semis, err)
		}
	}
}

func TestIsAudioFileExtensions(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"song.wav", true},
		{"/a/b/song.WAV", true},
		{"song.mp3", true},
		{"song.flac", true},
		{"song.ogg", true},
		{"song.m4a", true},
		{"song.aiff", true},
		{"song.wma", true},
		{"song.txt", false},
		{"song.mp4", false},
		{"", false},
		{"song", false},
		{"song.WAV.backup", false},
	}
	for _, tc := range cases {
		if got := IsAudioFile(tc.path); got != tc.want {
			t.Errorf("IsAudioFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestCopyFileCreatesMissingDestinationDir(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "nested", "dir")
	srcPath := filepath.Join(srcDir, "source.txt")
	content := []byte("hello")
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	dstPath := filepath.Join(dstDir, "dest.txt")
	if err := CopyFile(srcPath, dstPath); err == nil {
		t.Fatal("expected error when destination directory does not exist")
	}
}

func TestCopyFileOverwritesAndSyncs(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "source.txt")
	if err := os.WriteFile(srcPath, []byte("new"), 0o644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}
	dstPath := filepath.Join(dstDir, "dest.txt")
	if err := os.WriteFile(dstPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("failed to write dest: %v", err)
	}

	if err := CopyFile(srcPath, dstPath); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}
	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("failed to read dest: %v", err)
	}
	if string(got) != "new" {
		t.Errorf("content = %q, want new", string(got))
	}
}
