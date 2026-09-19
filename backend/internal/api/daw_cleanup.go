package api

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// autoCleanTmpMinAge is the minimum age a temporary file must have before the
// automatic cleanup is allowed to remove it. It is a package-level variable so
// tests can shorten it to verify the age gate without waiting.
var autoCleanTmpMinAge = 5 * time.Minute

// autoCleanTmpFiles removes stale temporary files from daw-data/<song>/tmp.
// It is invoked automatically at server startup and after every queued job
// finishes, regardless of whether the job succeeded or failed.
//
// Safety rules (both must hold):
//   - The song must not have any job in progress (waiting, processing or
//     blocked_no_gpu). This prevents deleting temporaries that an active
//     pipeline may still be reading or writing.
//   - The file must be at least autoCleanTmpMinAge old. This avoids racing
//     with recently-created files used by asynchronous DAW operations.
//
// Only files strictly inside daw-data/<song>/tmp/ are considered; every other
// directory (original, imports, edits, output, input, etc.) is left untouched.
// If a song directory cannot be resolved safely, it is skipped and a warning
// is logged.
func (s *Server) autoCleanTmpFiles(trigger string) {
	root := dataRoot()
	dawRoot := filepath.Join(root, dawDataDirName)

	entries, err := os.ReadDir(dawRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		Log("backend", "warn", fmt.Sprintf("Temp cleanup aborted: cannot read daw-data root: %v", err))
		return
	}

	activeSongs := s.activeJobSongs()

	var totalFiles, totalBytes int64
	var cleanedSongs int

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		song := entry.Name()
		songDir, err := safeDAWSongDir(root, song)
		if err != nil {
			Log("backend", "warn", fmt.Sprintf("Temp cleanup skipped invalid song dir %q: %v", song, err))
			continue
		}
		if activeSongs[song] {
			continue
		}

		tmpDir := filepath.Join(songDir, dawTmpSubdir)
		files, bytes := removeTmpFiles(tmpDir, root, trigger)
		if files > 0 {
			totalFiles += files
			totalBytes += bytes
			cleanedSongs++
		}
	}

	if totalFiles > 0 {
		Log("backend", "info", fmt.Sprintf(
			"Temp cleanup summary: trigger=%s songs=%d files=%d bytes=%d (%.2f MB)",
			trigger, cleanedSongs, totalFiles, totalBytes, float64(totalBytes)/(1024*1024),
		))
	}
}

// activeJobSongs returns the set of songs that currently have a job in flight.
// A song is considered active if its job status is waiting, processing or
// blocked_no_gpu. Done and error jobs are intentionally excluded.
func (s *Server) activeJobSongs() map[string]bool {
	active := make(map[string]bool)
	s.jobsMu.RLock()
	defer s.jobsMu.RUnlock()
	for song, job := range s.jobs {
		switch job.Status {
		case "waiting", "processing", "blocked_no_gpu":
			active[song] = true
		}
	}
	return active
}

// removeTmpFiles deletes regular files inside tmpDir that are older than
// autoCleanTmpMinAge. It skips symlinks and subdirectories and returns the
// number of files removed and bytes freed. Each deletion is logged using the
// same Deletion: prefix used by the manual deletion endpoints.
func removeTmpFiles(tmpDir, root, trigger string) (int64, int64) {
	info, err := os.Lstat(tmpDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, 0
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return 0, 0
	}

	var files, bytes int64
	cutoff := time.Now().Add(-autoCleanTmpMinAge)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		einfo, err := entry.Info()
		if err != nil {
			continue
		}
		if einfo.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if einfo.ModTime().After(cutoff) {
			continue
		}

		path := filepath.Join(tmpDir, entry.Name())
		size := einfo.Size()
		if err := os.Remove(path); err != nil {
			Log("backend", "warn", fmt.Sprintf("Temp cleanup failed to remove %s: %v", path, err))
			continue
		}

		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		logAutoDeletion("auto-tmp", rel, 1, size, trigger)
		files++
		bytes += size
	}

	return files, bytes
}

// logAutoDeletion writes an audit entry with the same Deletion: prefix and
// fields used by manual deletion endpoints. Because automatic cleanup has no
// associated HTTP request, the trigger (startup / job-finish) is recorded
// instead of an IP and User-Agent.
func logAutoDeletion(kind, name string, files, bytes int64, trigger string) {
	Log("backend", "info", fmt.Sprintf(
		`Deletion: kind=%s name=%q files=%d bytes=%d trigger=%s`,
		kind, name, files, bytes, trigger,
	))
}
