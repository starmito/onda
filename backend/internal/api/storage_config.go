package api

import (
	"encoding/json"
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
	CurrentRoot    string                         `json:"current_root"`
	Source         string                         `json:"source"`
	Exists         bool                           `json:"exists"`
	Writable       bool                           `json:"writable"`
	Folders        map[string]storageConfigFolder `json:"folders"`
	Mode           string                         `json:"mode"`
	Candidates     []string                       `json:"candidates"`
	Note           string                         `json:"note,omitempty"`
	ExportDir      string                         `json:"export_dir"`
	ExportSource   string                         `json:"export_source"`
	ExportExists   bool                           `json:"export_exists"`
	ExportWritable bool                           `json:"export_writable"`
}

// storageSettings is the on-disk representation of .onda-settings.json.
type storageSettings struct {
	DataRoot  string `json:"data_root"`
	ExportDir string `json:"export_dir"`
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

// persistedExportDir reads the export_dir value from the settings file. It is
// consulted by exportDir() after the environment variable and before the
// legacy fallback (empty, which keeps each export in its current location).
func persistedExportDir() string {
	s, err := loadStorageSettings()
	if err != nil {
		return ""
	}
	return s.ExportDir
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

// exportDir returns the configured export directory, or empty if the user has
// not configured one. An empty result means "use the legacy per-export
// locations", preserving today's default behaviour.
func exportDir() string {
	if dir := os.Getenv("ONDA_EXPORT_DIR"); dir != "" {
		return dir
	}
	if dir := persistedExportDir(); dir != "" {
		return dir
	}
	return ""
}

// exportDirWithSource returns the effective export directory and where it
// comes from according to the precedence rules. The default is an empty string
// and source "default".
func exportDirWithSource() (string, string) {
	if dir := os.Getenv("ONDA_EXPORT_DIR"); dir != "" {
		return dir, "env"
	}
	if dir := persistedExportDir(); dir != "" {
		return dir, "settings"
	}
	return "", "default"
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

// validateAbsoluteWritableDir enforces the common safety rules for a
// user-chosen absolute directory. The name argument is used only in error
// messages.
func validateAbsoluteWritableDir(root, name string) error {
	if root == "" {
		return fmt.Errorf("%s path is empty", name)
	}
	if strings.ContainsAny(root, "\\\x00") {
		return fmt.Errorf("%s path contains invalid characters", name)
	}
	for _, part := range strings.Split(filepath.ToSlash(root), "/") {
		if part == ".." {
			return fmt.Errorf("%s path must not contain parent references (..)", name)
		}
	}
	if !filepath.IsAbs(root) {
		return fmt.Errorf("%s path must be absolute", name)
	}

	cleaned := filepath.Clean(root)
	if isContainerMode() {
		if cleaned != "/app" && !strings.HasPrefix(cleaned, "/app/") {
			return fmt.Errorf("in container mode, %s path must be under /app", name)
		}
	}

	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("%s path does not exist or cannot be accessed: %w", name, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s path is not a directory", name)
	}
	if !isWritableDir(root) {
		return fmt.Errorf("%s path is not writable", name)
	}
	return nil
}

// validateStorageRoot enforces the safety rules for a user-chosen data root.
func validateStorageRoot(root string) error {
	return validateAbsoluteWritableDir(root, "root")
}

// validateExportDir enforces the safety rules for a user-chosen export directory.
func validateExportDir(dir string) error {
	return validateAbsoluteWritableDir(dir, "export dir")
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
	exportDir, exportSource := exportDirWithSource()

	exists := false
	writable := false
	if info, err := os.Stat(root); err == nil && info.IsDir() {
		exists = true
		writable = isWritableDir(root)
	}

	exportExists := false
	exportWritable := false
	if exportDir != "" {
		if info, err := os.Stat(exportDir); err == nil && info.IsDir() {
			exportExists = true
			exportWritable = isWritableDir(exportDir)
		}
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
		CurrentRoot:    root,
		Source:         source,
		Exists:         exists,
		Writable:       writable,
		Folders:        folders,
		Mode:           mode,
		Candidates:     listStorageCandidates(root),
		ExportDir:      exportDir,
		ExportSource:   exportSource,
		ExportExists:   exportExists,
		ExportWritable: exportWritable,
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

// handleStorageConfigPost validates and applies a new data root and/or export
// directory. Either field may be omitted; an empty string clears the persisted
// setting and unsets the corresponding environment variable.
// POST /api/storage/config
func (s *Server) handleStorageConfigPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}

	var req struct {
		Root      *string `json:"root,omitempty"`
		ExportDir *string `json:"export_dir,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.Root == nil && req.ExportDir == nil {
		writeErrorJSON(w, http.StatusBadRequest, "root or export_dir is required")
		return
	}

	settings, err := loadStorageSettings()
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "failed to load settings: "+err.Error())
		return
	}

	updated := []string{}
	if req.Root != nil {
		root := strings.TrimSpace(*req.Root)
		if root != "" {
			if err := validateStorageRoot(root); err != nil {
				writeErrorJSON(w, http.StatusBadRequest, err.Error())
				return
			}
			root = filepath.Clean(root)
			if err := os.Setenv("ONDA_DATA_DIR", root); err != nil {
				writeErrorJSON(w, http.StatusInternalServerError, "failed to apply data root: "+err.Error())
				return
			}
			settings.DataRoot = root
			updated = append(updated, "data root")
		} else {
			if err := os.Unsetenv("ONDA_DATA_DIR"); err != nil {
				writeErrorJSON(w, http.StatusInternalServerError, "failed to clear data root env: "+err.Error())
				return
			}
			settings.DataRoot = ""
			updated = append(updated, "data root cleared")
		}
	}

	if req.ExportDir != nil {
		dir := strings.TrimSpace(*req.ExportDir)
		if dir != "" {
			if err := validateExportDir(dir); err != nil {
				writeErrorJSON(w, http.StatusBadRequest, err.Error())
				return
			}
			dir = filepath.Clean(dir)
			if err := os.Setenv("ONDA_EXPORT_DIR", dir); err != nil {
				writeErrorJSON(w, http.StatusInternalServerError, "failed to apply export dir: "+err.Error())
				return
			}
			settings.ExportDir = dir
			updated = append(updated, "export dir")
		} else {
			if err := os.Unsetenv("ONDA_EXPORT_DIR"); err != nil {
				writeErrorJSON(w, http.StatusInternalServerError, "failed to clear export dir env: "+err.Error())
				return
			}
			settings.ExportDir = ""
			updated = append(updated, "export dir cleared")
		}
	}

	if err := saveStorageSettings(settings); err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "failed to persist settings: "+err.Error())
		return
	}

	if req.Root != nil {
		reloadDataRootConfig()
	}

	resp := buildStorageConfigResponse()
	if isContainerMode() {
		resp.Note = "Storage configuration updated. Remember: the Docker compose volume mount takes precedence when the container is recreated."
	} else {
		resp.Note = "Storage configuration updated and persisted."
	}
	if len(updated) > 0 {
		resp.Note += " Updated: " + strings.Join(updated, ", ") + "."
	}
	writeJSON(w, http.StatusOK, resp)
}

// reloadDataRootConfig reloads all in-memory configuration that is normally
// read once at startup from files under the current data root. It must be
// called after the data root has been changed at runtime so the app uses the
// new root's presets, default preset, UI settings and export profiles.
func reloadDataRootConfig() {
	loadUserPresets()
	loadDefaultPreset()
	if err := loadUISettings(); err != nil {
		Log("backend", "warn", "Failed to reload UI settings after data-root change: "+err.Error())
	}
	if err := loadExportProfiles(); err != nil {
		Log("backend", "warn", "Failed to reload export profiles after data-root change: "+err.Error())
	}
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
