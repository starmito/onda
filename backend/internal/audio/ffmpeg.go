package audio

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ConvertToWav convierte cualquier archivo de audio a WAV usando ffmpeg.
// sampleRate: frecuencia de muestreo en Hz (0 para mantener la original)
func ConvertToWav(input, output string, sampleRate int) error {
	args := []string{"-y", "-i", input}
	if sampleRate > 0 {
		args = append(args, "-ar", strconv.Itoa(sampleRate))
	}
	args = append(args, output)

	cmd := exec.Command("ffmpeg", args...)
	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg ConvertToWav failed: %w\nOutput: %s", err, string(outputBytes))
	}
	return nil
}

// GetDuration obtiene la duración de un archivo de audio en segundos usando ffprobe.
func GetDuration(input string) (float64, error) {
	cmd := exec.Command(
		"ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		input,
	)
	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("ffprobe GetDuration failed: %w\nOutput: %s", err, string(outputBytes))
	}

	// Parse the JSON output to extract duration
	// Simple approach: find "duration" field in JSON
	content := string(outputBytes)
	prefix := `"duration": "`
	start := strings.Index(content, prefix)
	if start == -1 {
		// Try without quotes (some versions)
		prefix = `"duration": `
		start = strings.Index(content, prefix)
		if start == -1 {
			return 0, fmt.Errorf("ffprobe GetDuration: duration not found in output")
		}
		start += len(prefix)
	} else {
		start += len(prefix)
	}

	end := strings.Index(content[start:], `"`)
	if end == -1 {
		// For unquoted, find comma or newline
		for i, c := range content[start:] {
			if c == ',' || c == '\n' || c == '}' {
				end = i
				break
			}
		}
	}

	duration, err := strconv.ParseFloat(content[start:start+end], 64)
	if err != nil {
		return 0, fmt.Errorf("ffprobe GetDuration: failed to parse duration: %w", err)
	}

	return duration, nil
}

// IsFfmpegInstalled verifica si ffmpeg está disponible en el PATH
func IsFfmpegInstalled() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}
