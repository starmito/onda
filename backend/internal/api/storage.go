package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// folderUsage holds the file count and byte size for a project folder.
type folderUsage struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

// cleanResult is returned by POST /api/storage/clean.
type cleanResult struct {
	Action string `json:"action"`
	Files  int64  `json:"files"`
	Bytes  int64  `json:"bytes"`
}

// storageFolders is the ordered list of project folders reported by usage.
var storageFolders = []string{"input", "input_rubberband", "daw-data", "output", "models", "logs"}

// dirUsage recursively counts regular files and their sizes under path.
// It does not follow symbolic links and is tolerant with missing folders.
func dirUsage(path string) (files int64, bytes int64) {
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		files++
		bytes += info.Size()
		return nil
	})
	return files, bytes
}

// freeDiskSpace returns the available bytes on the filesystem holding the
// data root output directory. It falls back to 0 if Statfs fails.
func freeDiskSpace(root string) int64 {
	outputDir := mustSub("output")
	_ = os.MkdirAll(outputDir, 0o755)

	var stat syscall.Statfs_t
	if err := syscall.Statfs(outputDir, &stat); err != nil {
		return 0
	}
	return int64(stat.Bavail * uint64(stat.Bsize))
}

// cacheUsage counts regular files and bytes under the data-root cache directory
// (<dataRoot>/.cache). This is separate from the models directory so the
// HuggingFace Hub cache (where Demucs keeps its downloaded weights) is reported
// without double-counting anything already covered by modelUsage().
func cacheUsage() (files int64, bytes int64) {
	cacheDir := filepath.Join(dataRoot(), ".cache")
	return dirUsage(cacheDir)
}

// handleStorageUsage reports per-folder file count/bytes and free disk space.
// GET /api/storage/usage
func (s *Server) handleStorageUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	root := dataRoot()
	folders := make(map[string]folderUsage, len(storageFolders))
	for _, name := range storageFolders {
		path := mustSub(name)
		files, bytes := dirUsage(path)
		folders[name] = folderUsage{Files: files, Bytes: bytes}
	}

	modelEntries, modelBytes := modelUsage()
	cacheFiles, cacheBytes := cacheUsage()

	resp := map[string]interface{}{
		"folders":    folders,
		"free_bytes": freeDiskSpace(root),
		"models": map[string]int64{
			"entries": modelEntries,
			"bytes":   modelBytes,
		},
		"cache": map[string]int64{
			"files": cacheFiles,
			"bytes": cacheBytes,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// handleStorageClean performs one of the allowed manual cleanup actions.
// POST /api/storage/clean
func (s *Server) handleStorageClean(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	root := dataRoot()
	var result cleanResult
	var err error

	switch req.Action {
	case "tmp":
		result, err = cleanTmpFiles(root)
	case "orphan-edits":
		result, err = cleanOrphanEdits(root)
	case "all-edits":
		result, err = cleanAllEdits(root)
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("unknown action %q: expected tmp, orphan-edits or all-edits", req.Action),
		})
		return
	}

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	logDeletion(r, "storage-clean", result.Action, result.Files, result.Bytes)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

// removeFilesInDir deletes regular files inside dir and returns count/bytes.
// It ignores symlinks and subdirectories.
func removeFilesInDir(dir string) (int64, int64, error) {
	var files, bytes int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.Remove(path); err != nil {
			continue
		}
		files++
		bytes += info.Size()
	}
	return files, bytes, nil
}

// cleanTmpFiles removes files inside daw-data/{song}/tmp for every song.
func cleanTmpFiles(root string) (cleanResult, error) {
	result := cleanResult{Action: "tmp"}
	dawRoot := mustSub(dawDataDirName)
	entries, err := os.ReadDir(dawRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		songDir, err := safeDAWSongDir(root, entry.Name())
		if err != nil {
			continue
		}
		files, bytes, _ := removeFilesInDir(filepath.Join(songDir, dawTmpSubdir))
		result.Files += files
		result.Bytes += bytes
	}
	return result, nil
}

// cleanAllEdits removes files inside daw-data/{song}/edits for every song,
// keeping original.* and imports/ untouched.
func cleanAllEdits(root string) (cleanResult, error) {
	result := cleanResult{Action: "all-edits"}
	dawRoot := mustSub(dawDataDirName)
	entries, err := os.ReadDir(dawRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		songDir, err := safeDAWSongDir(root, entry.Name())
		if err != nil {
			continue
		}
		files, bytes, _ := removeFilesInDir(filepath.Join(songDir, dawEditsSubdir))
		result.Files += files
		result.Bytes += bytes
	}
	return result, nil
}

// cleanOrphanEdits removes files inside daw-data/{song}/edits only for songs
// that no longer have any original.* file in their original/ subdirectory.
func cleanOrphanEdits(root string) (cleanResult, error) {
	result := cleanResult{Action: "orphan-edits"}
	dawRoot := mustSub(dawDataDirName)
	entries, err := os.ReadDir(dawRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		songDir, err := safeDAWSongDir(root, entry.Name())
		if err != nil {
			continue
		}

		hasOriginal := false
		originalDir := filepath.Join(songDir, dawOriginalSubdir)
		if orgEntries, err := os.ReadDir(originalDir); err == nil {
			for _, e := range orgEntries {
				if e.IsDir() {
					continue
				}
				if strings.HasPrefix(strings.ToLower(e.Name()), "original.") {
					hasOriginal = true
					break
				}
			}
		}
		if hasOriginal {
			continue
		}

		files, bytes, _ := removeFilesInDir(filepath.Join(songDir, dawEditsSubdir))
		result.Files += files
		result.Bytes += bytes
	}
	return result, nil
}
