package api

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
)

// Constants for the built-in synthetic VRAM test clip.
const (
	vramTestClipDirName     = "test_clip"
	vramTestClipFileName    = "onda_vram_test.wav"
	vramTestClipSampleRate  = 44100
	vramTestClipChannels    = 2
	vramTestClipBitDepth    = 16
	vramTestClipDurationSec = 45
	vramTestClipAmplitude   = 0.5
)

// vramTestClipFreqs are the sine-wave frequencies for the left and right
// channels. They are simple, license-free synthetic tones.
var vramTestClipFreqs = []float64{440.0, 880.0}

// vramTestClipPath returns the absolute path where the built-in test clip is
// stored under the current data root.
func vramTestClipPath() string {
	return filepath.Join(dataRoot(), vramTestClipDirName, vramTestClipFileName)
}

// ensureVRAMTestClip makes sure the synthetic 45-second stereo test clip exists
// on disk, creating it when necessary. The clip is generated with pure tones so
// it carries no licensing burden and does not depend on external tools.
func ensureVRAMTestClip() (string, error) {
	path := vramTestClipPath()
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("failed to create test clip dir: %w", err)
	}

	numSamples := vramTestClipDurationSec * vramTestClipSampleRate
	data := make([]int, numSamples*vramTestClipChannels)
	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(vramTestClipSampleRate)
		for c := 0; c < vramTestClipChannels; c++ {
			sample := vramTestClipAmplitude * math.Sin(2*math.Pi*vramTestClipFreqs[c]*t)
			val := int(math.Round(sample * 32767))
			if val > 32767 {
				val = 32767
			}
			if val < -32768 {
				val = -32768
			}
			data[i*vramTestClipChannels+c] = val
		}
	}

	buf := &audio.IntBuffer{
		Data: data,
		Format: &audio.Format{
			SampleRate:  vramTestClipSampleRate,
			NumChannels: vramTestClipChannels,
		},
	}

	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("failed to create test clip: %w", err)
	}
	defer f.Close()

	enc := wav.NewEncoder(f, vramTestClipSampleRate, vramTestClipBitDepth, vramTestClipChannels, 1)
	if err := enc.Write(buf); err != nil {
		return "", fmt.Errorf("failed to write test clip: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("failed to finalize test clip: %w", err)
	}
	return path, nil
}
