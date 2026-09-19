package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// allStorageFolders lists every subdirectory expected inside the data root.
var allStorageFolders = []string{"input", "output", "daw-data", "input_rubberband", "config", "logs", "models"}

// storageConfigFolder reports whether an expected folder exists and how many
// regular files it contains.
type storageConfigFolder struct {
	Exists bool  `json:"exists"`
	Files  int64 `json:"files"`
	Bytes  int64 `json:"bytes"`
}

// storageConfigResponse is returned by GET /api/storage/config and
// POST /api/storage/config.
type storageConfigResponse struct {
	CurrentRoot string                         `json:"current_root"`
	Source      string                         `json:"source"`
	Exists      bool                           `json:"exists"`
	Writable    bool                           `json:"writable"`
	Folders     map[string]storageConfigFolder `json:"folders"`
	Mode        string                         `json:"mode"`
	Candidates  []string                       `json:"candidates"`
	Note        string                         `json:"note,omitempty"`
}

// storageSettings is the on-disk representation of .onda-settings.json.
type storageSettings struct {
	DataRoot string `json:"data_root"`
}

// persistedDataRoot reads the data_root value from the settings file. It is
// consulted by dataRoot() after the environment variable and before the
// fallback project root.
func persistedDataRoot() string {
	s, err := loadStorageSettings()
	if err != nil {
		return ""
	}
	return s.DataRoot
}

// dataRootWithSource returns the effective data root and where it comes from
// according to the precedence rules.
func dataRootWithSource() (string, string) {
	if root := os.Getenv("ONDA_DATA_DIR"); root != "" {
		return root, "env"
	}
	if root := persistedDataRoot(); root != "" {
		return root, "settings"
	}
	return dataRoot(), "default"
}

func loadStorageSettings() (*storageSettings, error) {
	path := settingsFile()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &storageSettings{}, nil
		}
		return nil, err
	}
	var s storageSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func saveStorageSettings(s *storageSettings) error {
	path := settingsFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// settingsFile returns the path to the persistent settings file. It lives
// outside the data root so it survives data-root changes.
func settingsFile() string {
	if path := os.Getenv("ONDA_SETTINGS_FILE"); path != "" {
		return path
	}
	return filepath.Join(appDir(), ".onda-settings.json")
}

// validateStorageRoot enforces the safety rules for a user-chosen data root.
func validateStorageRoot(root string) error {
	if root == "" {
		return errors.New("root path is empty")
	}
	if strings.ContainsAny(root, "\\\x00") {
		return errors.New("root path contains invalid characters")
	}
	for _, part := range strings.Split(filepath.ToSlash(root), "/") {
		if part == ".." {
			return errors.New("root path must not contain parent references (..)")
		}
	}
	if !filepath.IsAbs(root) {
		return errors.New("root path must be absolute")
	}

	cleaned := filepath.Clean(root)
	if isContainerMode() {
		if cleaned != "/app" && !strings.HasPrefix(cleaned, "/app/") {
			return errors.New("in container mode, root path must be under /app")
		}
	}

	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("root path does not exist or cannot be accessed: %w", err)
	}
	if !info.IsDir() {
		return errors.New("root path is not a directory")
	}
	if !isWritableDir(root) {
		return errors.New("root path is not writable")
	}
	return nil
}

// isWritableDir checks write access by creating and removing a temporary file.
func isWritableDir(path string) bool {
	tmp := filepath.Join(path, ".onda-write-test-"+uniqueTempSuffix())
	f, err := os.Create(tmp)
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(tmp)
	return true
}

func uniqueTempSuffix() string {
	return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
}

// buildStorageConfigResponse gathers the current storage configuration.
func buildStorageConfigResponse() storageConfigResponse {
	root, source := dataRootWithSource()

	exists := false
	writable := false
	if info, err := os.Stat(root); err == nil && info.IsDir() {
		exists = true
		writable = isWritableDir(root)
	}

	folders := make(map[string]storageConfigFolder, len(allStorageFolders))
	for _, name := range allStorageFolders {
		path := filepath.Join(root, name)
		entry := storageConfigFolder{}
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			entry.Exists = true
			entry.Files, entry.Bytes = dirUsage(path)
		}
		folders[name] = entry
	}

	mode := "standalone"
	if isContainerMode() {
		mode = "container"
	}

	return storageConfigResponse{
		CurrentRoot: root,
		Source:      source,
		Exists:      exists,
		Writable:    writable,
		Folders:     folders,
		Mode:        mode,
		Candidates:  listStorageCandidates(root),
	}
}

// listStorageCandidates returns candidate directories the user may pick.
// In container mode the search is limited to /app and the current root.
func listStorageCandidates(currentRoot string) []string {
	seen := map[string]bool{}
	var candidates []string

	add := func(p string) {
		p = filepath.Clean(p)
		if seen[p] {
			return
		}
		seen[p] = true
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			candidates = append(candidates, p)
		}
	}

	if isContainerMode() {
		add("/app")
		add(currentRoot)
		entries, _ := os.ReadDir("/app")
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join("/app", e.Name())
			if isWritableDir(p) {
				add(p)
			}
		}
	} else {
		add(currentRoot)
	}

	return candidates
}

// handleStorageConfigGet returns the current storage configuration.
// GET /api/storage/config
func (s *Server) handleStorageConfigGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	writeJSON(w, http.StatusOK, buildStorageConfigResponse())
}

// handleStorageConfigPost validates and applies a new data root.
// POST /api/storage/config
func (s *Server) handleStorageConfigPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}

	var req struct {
		Root string `json:"root"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if err := validateStorageRoot(req.Root); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	root := filepath.Clean(req.Root)
	if err := os.Setenv("ONDA_DATA_DIR", root); err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "failed to apply data root: "+err.Error())
		return
	}
	if err := saveStorageSettings(&storageSettings{DataRoot: root}); err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "failed to persist data root: "+err.Error())
		return
	}

	resp := buildStorageConfigResponse()
	if isContainerMode() {
		resp.Note = "Data root updated. Remember: the Docker compose volume mount takes precedence when the container is recreated."
	} else {
		resp.Note = "Data root updated and persisted."
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeErrorJSON(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeErrorJSON(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s not allowed", r.Method))
}
