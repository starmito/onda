package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxFailedDiagnosticsDirs is the number of _failed_* directories to keep per
// song. Older directories are removed so diagnostic logs do not grow without
// bound.
const maxFailedDiagnosticsDirs = 5

// FailureDiagnostics exposes the real reason a pipeline step failed so the UI
// can show something more useful than a bare "error" status. It is read from
// the pipeline_status.json and the persisted _failed_<step>/stderr.log written
// by pipeline.sh.
type FailureDiagnostics struct {
	Step      string `json:"step"`
	ExitCode  int    `json:"exit_code"`
	Error     string `json:"error"`
	Stderr    string `json:"stderr"`
	FailedDir string `json:"failed_dir"`
}

// readFailureDiagnostics reads the failure reason for a single song from the
// per-song output/<song>/pipeline_status.json and the song's _failed_* directory.
// It returns nil when the pipeline status is not failed.
func readFailureDiagnostics(outputRoot, song string) *FailureDiagnostics {
	statusPath := filepath.Join(outputRoot, song, "pipeline_status.json")
	data, err := os.ReadFile(statusPath)
	if err != nil {
		return nil
	}
	var status struct {
		Status   string `json:"status"`
		Step     string `json:"step"`
		ExitCode int    `json:"exit_code"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(data, &status); err != nil {
		return nil
	}
	if status.Status != "failed" {
		return nil
	}

	diag := &FailureDiagnostics{
		Step:     status.Step,
		ExitCode: status.ExitCode,
		Error:    status.Error,
	}

	songDir := filepath.Join(outputRoot, song)
	failedDir := findFailedDir(songDir, status.Step)
	if failedDir != "" {
		diag.FailedDir = filepath.Base(failedDir)
		stderrPath := filepath.Join(failedDir, "stderr.log")
		if stderr, err := os.ReadFile(stderrPath); err == nil {
			diag.Stderr = tailString(string(stderr), 20, 8192)
		}
	}

	return diag
}

// findFailedDir locates the most relevant _failed_* directory for the failed
// step. It prefers the directory whose suffix matches the failed step, and
// falls back to the most recently modified _failed_* directory.
func findFailedDir(outputDir, step string) string {
	dirs, err := listFailedDirs(outputDir)
	if err != nil || len(dirs) == 0 {
		return ""
	}
	if step != "" {
		for _, d := range dirs {
			if filepath.Base(d) == "_failed_"+step {
				return d
			}
		}
	}
	return dirs[0]
}

// listFailedDirs returns all _failed_* directories under outputDir sorted by
// modification time (newest first).
func listFailedDirs(outputDir string) ([]string, error) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return nil, err
	}
	type failedDir struct {
		path    string
		modTime int64
	}
	var dirs []failedDir
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "_failed_") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		dirs = append(dirs, failedDir{path: filepath.Join(outputDir, e.Name()), modTime: info.ModTime().Unix()})
	}
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].modTime > dirs[j].modTime
	})
	result := make([]string, len(dirs))
	for i, d := range dirs {
		result[i] = d.path
	}
	return result, nil
}

// cleanupOldFailedDirs keeps only the most recent keep _failed_* directories in
// outputDir. If excludeStep is non-empty, the directory for that step is never
// removed, which protects the diagnostics of a job that is still running or
// has just failed.
func cleanupOldFailedDirs(outputDir string, keep int, excludeStep string) error {
	if keep <= 0 {
		return nil
	}
	dirs, err := listFailedDirs(outputDir)
	if err != nil {
		return err
	}
	if len(dirs) <= keep {
		return nil
	}
	excludedName := ""
	if excludeStep != "" {
		excludedName = "_failed_" + excludeStep
	}
	for i := keep; i < len(dirs); i++ {
		if excludedName != "" && filepath.Base(dirs[i]) == excludedName {
			continue
		}
		if err := os.RemoveAll(dirs[i]); err != nil {
			Log("backend", "warn", "Failed to remove old diagnostics dir "+dirs[i]+": "+err.Error())
		}
	}
	return nil
}

// tailString returns the last maxLines of s, limited to roughly maxBytes.
func tailString(s string, maxLines, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 8192
	}
	if len(s) > maxBytes {
		s = s[len(s)-maxBytes:]
		if idx := strings.IndexAny(s, "\r\n"); idx != -1 {
			s = s[idx:]
		}
	}
	lines := strings.Split(strings.TrimRight(s, "\r\n"), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}
