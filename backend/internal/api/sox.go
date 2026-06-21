package api

import (
	"fmt"
	"os/exec"
	"strings"
)

// SoxEffect represents a single SoX effect with its parameters.
type SoxEffect struct {
	Name   string
	Params []string
}

// ApplySox runs `sox inputPath outputPath <effects...>` and returns an error
// that includes stderr output when SoX fails.
func ApplySox(inputPath, outputPath string, effects []SoxEffect) error {
	args := []string{inputPath, outputPath}
	for _, e := range effects {
		args = append(args, e.Name)
		args = append(args, e.Params...)
	}

	cmd := exec.Command("sox", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sox failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
