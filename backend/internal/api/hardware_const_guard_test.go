package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// allowedHardwareRelatedConsts lists non-test constants whose value happens to
// be expressed in MiB and whose name contains VRAM/RAM. They are *model*
// consumption estimates or reference measurements, not fixed hardware capacity.
// Any new capacity-like constant must be reviewed and either removed or added
// here with a written justification.
var allowedHardwareRelatedConsts = map[string]bool{
	"defaultVRAMMB":                true, // analytical fallback for unknown models
	"scnetWholeSongReferenceMB":    true, // measured peak for a specific SCNet run
	"scnetWholeSongReferenceDuration": true, // seconds, not MiB
}

// TestNoFixedHardwareCapacityConstants guards against re-introducing hardcoded
// hardware capacity numbers (VRAM/RAM) in the backend. Measured model peaks are
// allowed because they describe software behaviour, not the host hardware.
func TestNoFixedHardwareCapacityConstants(t *testing.T) {
	const root = "."
	// Match "const Name = 1234" where Name smells like VRAM/RAM/capacity/fallback.
	constPat := regexp.MustCompile(`(?m)^\s*const\s+([A-Za-z0-9_]*(?:VRAM|RAM|Available|Fallback|Capacity|Total)[A-Za-z0-9_]*)\s*=\s*(\d+)`)

	var found []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range constPat.FindAllStringSubmatch(string(data), -1) {
			name := m[1]
			if allowedHardwareRelatedConsts[name] {
				continue
			}
			found = append(found, filepath.ToSlash(path)+": "+m[0])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking backend: %v", err)
	}
	if len(found) > 0 {
		t.Fatalf("hardcoded hardware capacity constants found (add justification or remove):\n%s", strings.Join(found, "\n"))
	}
}
