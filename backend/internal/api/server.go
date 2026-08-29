package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/starmito/onda/internal/audio"
	"github.com/starmito/onda/internal/cli"
	"gopkg.in/yaml.v3"
)

// resolveProjectRoot returns the project root directory. It first honours the
// ONDA_ROOT environment variable, then resolves the executable's location.
// Expected layout:
//   <project-root>/
//     backend/<binary>
//     frontend/dist/
func resolveProjectRoot() string {
	if root := os.Getenv("ONDA_ROOT"); root != "" {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			return root
		}
	}
	exe, err := os.Executable()
	if err != nil {
		Log("backend", "warn", "Cannot resolve executable path, using CWD: "+err.Error())
		wd, _ := os.Getwd()
		return wd
	}
	return filepath.Dir(filepath.Dir(exe))
}

// FileEntry describes a generated stem file.
type FileEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// UVRModelEntry represents a model entry from the UVR catalog (uvr_models.json).
type UVRModelEntry struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Category    string `json:"category"`
	Filename    string `json:"filename"`
	DownloadURL string `json:"download_url"`
	SizeMB      int64  `json:"size_mb"`
	Description string `json:"description,omitempty"`
	Downloaded  bool   `json:"downloaded"`
	Source      string `json:"source"`
}

// JobRequest represents a queued separation job.
type JobRequest struct {
	Song   string          `json:"song"`
	Args   []string        `json:"args"`
	Config SeparateRequest `json:"config"`
	// Steps for multi-step pipeline chaining (v2.8.0+)
	Steps     []cli.PipelineStep `json:"steps,omitempty"`
	StepIndex int                `json:"step_index"` // current step being executed (for multi-step)
}

// JobState tracks the status of a separation job.
type JobState struct {
	Song        string      `json:"song"`
	Status      string      `json:"status"` // waiting, processing, done, error
	Progress    int         `json:"progress"`
	Error       string      `json:"error,omitempty"`
	Files       []FileEntry `json:"files,omitempty"`
	Index       int         `json:"index"`
	CurrentStep int         `json:"current_step"`
	TotalSteps  int         `json:"total_steps"`
	StepName    string      `json:"step_name"`
	Device      string      `json:"device,omitempty"`
}

// Server wraps the HTTP server with routes, middleware, and a sequential job queue.
type Server struct {
	mux           *http.ServeMux
	jobQueue      chan JobRequest
	jobs          map[string]*JobState
	jobsMu        sync.RWMutex
	nextIndex     int
	currentCancel context.CancelFunc
	currentCmd    *exec.Cmd
	currentPID    int
}

// killProcess sends SIGTERM to a single process. It is a variable so tests can
// substitute a mock implementation and verify which PIDs are targeted.
var killProcess = func(pid int) error {
	if pid <= 0 {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

// NewServer creates a new http.Server with CORS middleware and routes registered.
func NewServer(addr string) *http.Server {
	s := &Server{
		mux:      http.NewServeMux(),
		jobQueue: make(chan JobRequest, 20),
		jobs:     make(map[string]*JobState),
	}

	// Load persisted UI settings (uses defaults if file doesn't exist)
	if err := loadUISettings(); err != nil {
		Log("backend", "warn", "Failed to load UI settings: "+err.Error())
	}
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/queue/status", s.handleQueueStatus)
	s.mux.HandleFunc("DELETE /api/queue", s.handleQueueClear)
	s.mux.HandleFunc("POST /api/queue/cancel", s.handleQueueCancel)
	s.mux.HandleFunc("GET /api/results", s.handleResults)
	s.mux.HandleFunc("GET /api/inputs", s.handleInputs)
	// Presets API (must be BEFORE /api/models catch-all)
	s.mux.HandleFunc("GET /api/presets", s.handleGetPresets)
	s.mux.HandleFunc("POST /api/presets", s.handleSavePreset)
	s.mux.HandleFunc("DELETE /api/presets/{name}", s.handleDeletePreset)
	s.mux.HandleFunc("GET /api/presets/default", s.handleGetDefaultPreset)
	s.mux.HandleFunc("POST /api/presets/default", s.handleSetDefaultPreset)
	// UI Settings API
	s.mux.HandleFunc("GET /api/settings/ui", s.handleGetUISettings)
	s.mux.HandleFunc("POST /api/settings/ui", s.handleSaveUISettings)
	s.mux.HandleFunc("GET /api/logs", s.handleGetLogs)
	s.mux.HandleFunc("GET /api/logs/services", s.handleGetServiceLogs)
	s.mux.HandleFunc("/api/models", s.handleModels)
	s.mux.HandleFunc("GET /api/models/list", s.handleModelsList)
	s.mux.HandleFunc("POST /api/models/download", s.handleModelsDownload)
	s.mux.HandleFunc("GET /api/models/download/status", s.handleModelsDownloadStatus)
	s.mux.HandleFunc("GET /api/models/{name}/config", s.handleModelsConfig)
	s.mux.HandleFunc("POST /api/models/{name}/config", s.handleModelsConfig)
	s.mux.HandleFunc("GET /api/models/catalog", s.handleModelsCatalog)
	s.mux.HandleFunc("GET /api/models/catalog/hf", s.handleModelsCatalogHF)
	s.mux.HandleFunc("/api/gpu", s.handleGPU)
	s.mux.HandleFunc("GET /api/gpu/info", s.handleGPUInfo)
	s.mux.HandleFunc("GET /api/gpu/vram-calculator", s.handleVRAMCalculator)
	s.mux.HandleFunc("/api/separate", s.handleSeparate)
	s.mux.HandleFunc("POST /api/pitch", s.handlePitchShift)
	s.mux.HandleFunc("GET /api/audio/tempo", s.handleTempo)
	s.mux.HandleFunc("GET /api/audio/tempo-grid", s.handleTempoGrid)
	s.mux.HandleFunc("POST /api/audio/tempo", s.handleTempoShift)
	s.mux.HandleFunc("POST /api/audio/tempo-per-bar", s.handleTempoPerBar)
	s.mux.HandleFunc("POST /api/audio/trim", s.handleTrim)
	s.mux.HandleFunc("POST /api/audio/fade", s.handleFade)
	s.mux.HandleFunc("POST /api/audio/export", s.handleExport)
	s.mux.HandleFunc("GET /api/daw/stems", s.handleListStems)
	s.mux.HandleFunc("POST /api/daw/import", s.handleImportStem)
	s.mux.HandleFunc("POST /api/daw/upload", s.handleUploadAudio)
	s.mux.HandleFunc("POST /api/daw/eq", s.handleEQ)
	s.mux.HandleFunc("POST /api/daw/compressor", s.handleCompressor)
	s.mux.HandleFunc("POST /api/daw/reverb", s.handleReverb)
	s.mux.HandleFunc("POST /api/daw/delay", s.handleDelay)
	s.mux.HandleFunc("POST /api/daw/chorus", s.handleChorus)
	s.mux.HandleFunc("POST /api/daw/flanger", s.handleFlanger)
	s.mux.HandleFunc("POST /api/daw/phaser", s.handlePhaser)
	s.mux.HandleFunc("POST /api/daw/tremolo", s.handleTremolo)
	s.mux.HandleFunc("POST /api/daw/noisegate", s.handleNoiseGate)
	s.mux.HandleFunc("POST /api/daw/midi/parse", s.handleMidiParse)
	s.mux.HandleFunc("POST /api/daw/midi/export", s.handleMidiExport)
	s.mux.HandleFunc("GET /api/daw/midi/devices", s.handleMidiDevices)
	s.mux.HandleFunc("GET /api/daw/audio", s.handleServeAudio)
	s.mux.HandleFunc("GET /api/pitch/{song}", s.handleListPitchSubgroups)
	s.mux.HandleFunc("DELETE /api/pitch/{song}/{pitch}", s.handleDeletePitchSubgroup)
	s.mux.HandleFunc("DELETE /api/pitch/{song}/{pitch}/{file}", s.handleDeletePitchStem)
	s.mux.HandleFunc("GET /api/pitch/files/{song}/{pitch}/{file}", s.handlePitchFileServe)
	s.mux.HandleFunc("POST /api/upload", s.handleUpload)
	s.mux.HandleFunc("POST /api/upload/pitch", s.handleUploadPitch)
	s.mux.HandleFunc("GET /api/files/{song}/{file}", s.handleFileServe)
	s.mux.HandleFunc("POST /api/backend/start", s.handleBackendStart)
	s.mux.HandleFunc("POST /api/backend/stop", s.handleBackendStop)
	s.mux.HandleFunc("POST /api/backend/restart", s.handleBackendRestart)
	s.mux.HandleFunc("DELETE /api/files/{song}", s.handleDeleteSong)
	s.mux.HandleFunc("DELETE /api/delete", s.handleDeleteFile)
	s.mux.HandleFunc("DELETE /api/models/{name}", s.handleDeleteModel)
	s.mux.HandleFunc("DELETE /api/inputs/{name}", s.handleDeleteInput)
	s.mux.HandleFunc("DELETE /api/uploads/pitch/{name}", s.handleDeletePitchUpload)

	// Servir archivos estaticos de audio (relativos al project root para no depender de rutas de contenedor)
	frontendDir := filepath.Join(resolveProjectRoot(), "frontend", "dist")
	projectRoot := resolveProjectRoot()
	outputDir := filepath.Join(projectRoot, "output")
	inputRubberbandDir := filepath.Join(projectRoot, "input_rubberband")
	dawDataDir := filepath.Join(projectRoot, "daw-data")
	os.MkdirAll(outputDir, 0o755)
	os.MkdirAll(inputRubberbandDir, 0o755)
	os.MkdirAll(dawDataDir, 0o755)
	s.mux.Handle("GET /output/", http.StripPrefix("/output/", http.FileServer(http.Dir(outputDir))))
	s.mux.Handle("GET /input_rubberband/", http.StripPrefix("/input_rubberband/", http.FileServer(http.Dir(inputRubberbandDir))))
	s.mux.Handle("GET /daw-data/", http.StripPrefix("/daw-data/", http.FileServer(http.Dir(dawDataDir))))

	// Servir frontend Svelte desde disco (catch-all — debe ir al final)
	s.mux.Handle("/", http.FileServer(http.Dir(frontendDir)))

	go s.worker()

	return &http.Server{
		Addr:    addr,
		Handler: s.corsMiddleware(s.mux),
	}
}

// corsOrigins returns the configured allowed CORS origins. If none are
// configured, it returns a slice containing "*" to preserve backward
// compatibility.
func corsOrigins() []string {
	env := strings.TrimSpace(os.Getenv("ONDA_CORS_ORIGINS"))
	if env == "" {
		return []string{"*"}
	}
	parts := strings.Split(env, ",")
	origins := make([]string, 0, len(parts))
	for _, o := range parts {
		o = strings.TrimSpace(o)
		if o != "" {
			origins = append(origins, o)
		}
	}
	if len(origins) == 0 {
		return []string{"*"}
	}
	return origins
}

// corsMiddleware adds CORS headers and handles OPTIONS preflight.
// Allowed origins are read from ONDA_CORS_ORIGINS (comma-separated). If the
// variable is unset, the legacy behavior (Access-Control-Allow-Origin: *) is
// preserved. If the request origin is not in the configured list, no CORS
// headers are sent.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	origins := corsOrigins()
	wildcard := len(origins) == 1 && origins[0] == "*"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := wildcard
		if !wildcard {
			for _, o := range origins {
				if o == origin {
					allowed = true
					break
				}
			}
		}

		if allowed {
			if wildcard {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}

		if r.Method == http.MethodOptions {
			if allowed {
				w.WriteHeader(http.StatusNoContent)
			} else {
				w.WriteHeader(http.StatusForbidden)
			}
			return
		}

		// Wrap response writer to catch 404s
		lw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(lw, r)
		if lw.statusCode == http.StatusNotFound {
			// Distinguish missing API routes from missing static assets so the log is actionable.
			kind := "static"
			if strings.HasPrefix(r.URL.Path, "/api/") {
				kind = "api"
			}
			Log("backend", "warn", fmt.Sprintf("404 %s: %s %s (from: %s)", kind, r.Method, r.URL.String(), r.UserAgent()))
		}
	})
}

// loggingResponseWriter wraps http.ResponseWriter to capture the status code.
type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lw *loggingResponseWriter) WriteHeader(code int) {
	lw.statusCode = code
	lw.ResponseWriter.WriteHeader(code)
}

// handleHealth returns the health status of the Onda service.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	gpuAvailable, gpuInfo, _ := checkGPU()
	gpuType := detectGPUType()

	projectRoot := resolveProjectRoot()
	frontendDir := filepath.Join(projectRoot, "frontend", "dist")

	// ── Read frontend version ──
	frontendVersion := ""
	if data, err := os.ReadFile(filepath.Join(frontendDir, "VERSION")); err == nil {
		frontendVersion = strings.TrimSpace(string(data))
	}

	// ── Read pipeline version ──
	pipelineVersion := ""
	if data, err := os.ReadFile(filepath.Join(resolveProjectRoot(), "VERSION")); err == nil {
		pipelineVersion = strings.TrimSpace(string(data))
	}

	// ── Version mismatch detection ──
	var mismatches []map[string]string
	if frontendVersion != "" && frontendVersion != Version {
		mismatches = append(mismatches, map[string]string{
			"component": "frontend",
			"expected":  Version,
			"actual":    frontendVersion,
		})
	}
	if pipelineVersion != "" && pipelineVersion != Version {
		mismatches = append(mismatches, map[string]string{
			"component": "pipeline",
			"expected":  Version,
			"actual":    pipelineVersion,
		})
	}

	// ── Overall status ──
	status := "ok"
	if (!gpuAvailable && gpuType == "cpu") || len(mismatches) > 0 {
		status = "degraded"
	}

	// ── Build components ──
	var gpuObj map[string]interface{}
	if gpuAvailable || gpuType == "rocm" {
		gpuObj = map[string]interface{}{"ok": true, "type": gpuType, "detail": gpuInfo}
	} else {
		gpuObj = map[string]interface{}{"ok": false, "type": gpuType, "code": "E3", "detail": gpuInfo}
	}
	if gpuType == "cpu" {
		gpuObj["warning"] = "No GPU detected — running on CPU. Performance may be degraded."
	}

	frontendOK := frontendVersion == Version
	frontendObj := map[string]interface{}{
		"ok":      frontendOK,
		"version": frontendVersion,
	}
	if !frontendOK && frontendVersion != "" {
		frontendObj["detail"] = fmt.Sprintf("version mismatch: expected %s, got %s", Version, frontendVersion)
	}

	pipelineOK := pipelineVersion == Version
	pipelineObj := map[string]interface{}{
		"ok":      pipelineOK,
		"version": pipelineVersion,
	}
	if !pipelineOK && pipelineVersion != "" {
		pipelineObj["detail"] = fmt.Sprintf("version mismatch: expected %s, got %s", Version, pipelineVersion)
	}

	mismatchObj := map[string]interface{}{"ok": true}
	if len(mismatches) > 0 {
		mismatchObj = map[string]interface{}{
			"ok":     false,
			"detail": mismatches,
		}
	}

	resp := map[string]interface{}{
		"status":  status,
		"version": Version,
		"backend": map[string]interface{}{
			"ok":      true,
			"detail":  "unified container running",
			"version": Version,
		},
		"frontend":         frontendObj,
		"pipeline":         pipelineObj,
		"gpu":              gpuObj,
		"disk":             checkDisk(),
		"version_mismatch": mismatchObj,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// ResultsGroup groups stem files by song.
type ResultsGroup struct {
	Song  string      `json:"song"`
	Files []FileEntry `json:"files"`
}

// handleResults lists all songs and their stems from the output directory.
// GET /api/results
func (s *Server) handleResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	projectRoot := resolveProjectRoot()
	outputDir := filepath.Join(projectRoot, "output")
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode([]ResultsGroup{})
		return
	}

	results := make([]ResultsGroup, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		song := entry.Name()
		stemDir := filepath.Join(outputDir, song)
		stemEntries, err := os.ReadDir(stemDir)
		if err != nil {
			continue
		}
		var files []FileEntry
		for _, stemEntry := range stemEntries {
			if stemEntry.IsDir() {
				continue
			}
			name := stemEntry.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if ext != ".wav" && ext != ".mp3" && ext != ".flac" && ext != ".ogg" && ext != ".m4a" {
				continue
			}
			files = append(files, FileEntry{
				Name: name,
				Path: "/api/files/" + song + "/" + name,
			})
		}
		if len(files) > 0 {
			results = append(results, ResultsGroup{Song: song, Files: files})
		}
	}
	// Sort by song name for deterministic output
	sort.Slice(results, func(i, j int) bool {
		return results[i].Song < results[j].Song
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(results)
}

// InputEntry describes an uploaded input file.
type InputEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// handleInputs lists uploaded input files from the input directory.
// GET /api/inputs
func (s *Server) handleInputs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	projectRoot := resolveProjectRoot()
	inputDir := filepath.Join(projectRoot, "input")
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode([]InputEntry{})
		return
	}

	var inputs []InputEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".wav" && ext != ".mp3" && ext != ".flac" && ext != ".ogg" && ext != ".m4a" {
			continue
		}
		inputs = append(inputs, InputEntry{
			Name: name,
			Path: "/app/input/" + name,
		})
	}
	sort.Slice(inputs, func(i, j int) bool {
		return inputs[i].Name < inputs[j].Name
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(inputs)
}

// handleQueueStatus returns all jobs ordered by status priority.
// GET /api/queue/status
func (s *Server) handleQueueStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	s.jobsMu.RLock()
	defer s.jobsMu.RUnlock()

	// Read pipeline status file for live step/progress info
	type PipelineStatusJSON struct {
		Status          string  `json:"status"`
		Step            string  `json:"step"`
		Progress        float64 `json:"progress"`
		OverallProgress float64 `json:"overall_progress"`
		Device          string  `json:"device"`
	}
	var pipelineStatus PipelineStatusJSON
	projectRoot := resolveProjectRoot()
	statusPath := filepath.Join(projectRoot, "output", "pipeline_status.json")
	if data, err := os.ReadFile(statusPath); err == nil {
		json.Unmarshal(data, &pipelineStatus)
	}

	// Prefer the per-step progress field; fall back to the multi-step overall
	// progress reported by chained pipelines.
	liveProgress := pipelineStatus.Progress
	if liveProgress == 0 && pipelineStatus.OverallProgress > 0 {
		liveProgress = pipelineStatus.OverallProgress
	}

	// Step name mapping and ordering
	stepOrder := map[string]int{"vocal": 1, "viperx": 1, "demucs": 2, "rubberband": 3}

	var jobList []*JobState
	for _, j := range s.jobs {
		// For the processing job, inject live step/progress from pipeline_status.json
		if j.Status == "processing" && pipelineStatus.Status != "" {
			j.StepName = capitalizeStep(pipelineStatus.Step)
			j.CurrentStep = stepOrder[pipelineStatus.Step]
			if j.CurrentStep == 0 {
				j.CurrentStep = 1
			}
			j.Progress = int(liveProgress * 100)
			j.Device = pipelineStatus.Device
			// Ensure total_steps is at least current_step
			if j.TotalSteps < j.CurrentStep {
				j.TotalSteps = j.CurrentStep
			}
		} else if j.Status == "done" {
			j.Progress = 100
			j.StepName = "Completado"
			j.CurrentStep = j.TotalSteps
		}
		jobList = append(jobList, j)
	}
	sort.Slice(jobList, func(i, j int) bool {
		order := map[string]int{"processing": 0, "waiting": 1, "done": 2, "error": 3}
		if order[jobList[i].Status] != order[jobList[j].Status] {
			return order[jobList[i].Status] < order[jobList[j].Status]
		}
		return jobList[i].Index < jobList[j].Index
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"jobs": jobList})
}

// cancelCurrentJob stops the running pipeline subprocess safely. It must be
// called with s.jobsMu held. It records that the job was cancelled and sends a
// SIGTERM only to the tracked PID, avoiding broad pkill commands that could
// kill unrelated container processes.
func (s *Server) cancelCurrentJob() {
	if s.currentCancel != nil {
		s.currentCancel()
	}
	if s.currentPID != 0 {
		killProcess(s.currentPID)
	}
	s.currentCancel = nil
	s.currentCmd = nil
	s.currentPID = 0
}

// handleQueueClear cancels the current job and removes all jobs from the queue.
// DELETE /api/queue
func (s *Server) handleQueueClear(w http.ResponseWriter, r *http.Request) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()

	s.cancelCurrentJob()

	// Clear all jobs
	s.jobs = make(map[string]*JobState)
	s.nextIndex = 0

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
}

// handleQueueCancel cancels the currently running job and removes it from the queue.
// POST /api/queue/cancel
func (s *Server) handleQueueCancel(w http.ResponseWriter, r *http.Request) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()

	s.cancelCurrentJob()

	// Remove all jobs — cancel means "stop everything and start fresh"
	s.jobs = make(map[string]*JobState)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

// capitalizeStep returns a display-friendly step name.
func capitalizeStep(step string) string {
	switch step {
	case "vocal", "viperx":
		return "Vocal"
	case "demucs":
		return "Demucs"
	case "rubberband":
		return "Rubberband"
	case "complete":
		return "Complete"
	default:
		return step
	}
}

// worker processes jobs sequentially from the queue.
func (s *Server) worker() {
	for job := range s.jobQueue {
		s.jobsMu.Lock()
		state, ok := s.jobs[job.Song]
		if !ok {
			// Job was cleared/cancelled — skip this channel item entirely
			s.jobsMu.Unlock()
			continue
		}
		state.Status = "processing"
		s.jobsMu.Unlock()

		// Reset pipeline_status.json so stale progress from a cancelled job doesn't bleed in
		if projectRoot := resolveProjectRoot(); projectRoot != "" {
			statusPath := filepath.Join(projectRoot, "output", "pipeline_status.json")
			os.WriteFile(statusPath, []byte(`{}`), 0644)
		}

		// Handle multi-step pipeline chaining
		steps := job.Steps
		if len(steps) > 1 {
			// Multi-step chaining: execute each step sequentially
			s.runMultiStepPipeline(job, steps, state)
		} else {
			// Single step (or old format): execute once
			s.runSinglePipeline(job, state)
		}
	}
}

// runSinglePipeline executes a single pipeline.sh invocation.
func (s *Server) runSinglePipeline(job JobRequest, state *JobState) {
	ctx, cancel := context.WithCancel(context.Background())
	script := "/app/pipeline.sh"
	pipelineArgs := job.Args
	// Tests may pass an explicit fake script as the first argument.
	if len(job.Args) > 0 && strings.HasSuffix(job.Args[0], ".sh") {
		if info, err := os.Stat(job.Args[0]); err == nil && !info.IsDir() {
			script = job.Args[0]
			pipelineArgs = job.Args[1:]
		}
	}
	args := append([]string{script}, pipelineArgs...)
	cmd := exec.CommandContext(ctx, "bash", args...)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	s.jobsMu.Lock()
	s.currentCancel = cancel
	s.currentCmd = cmd
	var err error
	if sErr := cmd.Start(); sErr != nil {
		err = sErr
	} else {
		s.currentPID = cmd.Process.Pid
	}
	s.jobsMu.Unlock()

	if err == nil {
		err = cmd.Wait()
	}

	s.jobsMu.Lock()
	s.currentCancel = nil
	s.currentCmd = nil
	s.currentPID = 0
	s.jobsMu.Unlock()
	cancel()

	output := out.Bytes()
	// Log all pipeline output to ring buffer with distinct timestamps
	logPipelineOutput(string(output))

	s.jobsMu.Lock()
	if state, ok := s.jobs[job.Song]; ok {
		if err != nil {
			state.Status = "error"
			errMsg := strings.TrimSpace(string(output))
			if errMsg == "" {
				errMsg = "pipeline failed"
			}
			state.Error = errMsg
			Log("pipeline", "error", "Pipeline failed for "+job.Song+": "+errMsg)
		} else {
			state.Status = "done"
			state.Files = listStems(job.Song)
			Log("pipeline", "success", fmt.Sprintf("Pipeline completed: %s (%d stems)", job.Song, len(state.Files)))
		}
	}
	s.jobsMu.Unlock()
}

// runMultiStepPipeline executes multiple pipeline.sh invocations, one per step,
// chaining outputs from each step to the next.
func (s *Server) runMultiStepPipeline(job JobRequest, steps []cli.PipelineStep, state *JobState) {
	projectRoot := resolveProjectRoot()
	song := job.Song
	outputDir := filepath.Join(projectRoot, "output", song)

	// Ensure output directory exists
	os.MkdirAll(outputDir, 0o755)

	currentInput := job.Config.Input
	allStems := make([]FileEntry, 0)

	for i, step := range steps {
		if !step.Enabled {
			Log("pipeline", "info", fmt.Sprintf("Step %d/%d (%s) disabled, skipping", i+1, len(steps), step.ID))
			continue
		}

		// Update job state with current step info
		s.jobsMu.Lock()
		if state, ok := s.jobs[job.Song]; ok {
			state.CurrentStep = i + 1
			state.StepName = stepTypeDisplay(step.Type)
		}
		s.jobsMu.Unlock()

		Log("pipeline", "info", fmt.Sprintf("Step %d/%d: %s (%s)", i+1, len(steps), step.ID, step.Type))

		// Build args for this specific step
		containerOutput := "/app/output/" + song
		stepArgs := buildStepPipelineArgs(step, currentInput, containerOutput, job.Config.Device)
		stepArgs = append(stepArgs, "--output", containerOutput)

		// For steps after the first, add --no-clean to preserve previous outputs
		if i > 0 {
			stepArgs = append(stepArgs, "--no-clean")
		}

		stepArgs = append(stepArgs, currentInput)

		// Execute this step
		ctx, cancel := context.WithCancel(context.Background())
		pipelineArgs := append([]string{"/app/pipeline.sh"}, stepArgs...)
		cmd := exec.CommandContext(ctx, "bash", pipelineArgs...)

		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out

		s.jobsMu.Lock()
		s.currentCancel = cancel
		s.currentCmd = cmd
		var err error
		if sErr := cmd.Start(); sErr != nil {
			err = sErr
		} else {
			s.currentPID = cmd.Process.Pid
		}
		s.jobsMu.Unlock()

		if err == nil {
			err = cmd.Wait()
		}

		s.jobsMu.Lock()
		s.currentCancel = nil
		s.currentCmd = nil
		s.currentPID = 0
		s.jobsMu.Unlock()
		cancel()

		output := out.Bytes()
		// Log pipeline output
		logPipelineOutput(string(output))

		// Check for errors
		if err != nil {
			s.jobsMu.Lock()
			if state, ok := s.jobs[job.Song]; ok {
				state.Status = "error"
				state.Error = fmt.Sprintf("Step %d (%s) failed: %s", i+1, step.ID, strings.TrimSpace(string(output)))
				Log("pipeline", "error", fmt.Sprintf("Pipeline step %d/%d failed for %s: %s", i+1, len(steps), job.Song, strings.TrimSpace(string(output))))
			}
			s.jobsMu.Unlock()
			return
		}

		// After step completes, find output stems for chaining to next step
		if i < len(steps)-1 {
			// Look for the routed stem to use as input for the next step
			routedInput := findChainedInput(outputDir, step)
			if routedInput != "" {
				// Convert host path to container path for next invocation
				currentInput = toInternalContainerPath(routedInput)
				Log("pipeline", "info", fmt.Sprintf("Chaining: step %d output → input for step %d: %s", i+1, i+2, currentInput))
			} else {
				// Fallback: if no routed stem found, use the original input
				Log("pipeline", "warn", fmt.Sprintf("No routed stem found for step %d, using original input", i+1))
				currentInput = job.Config.Input
			}
		}

		// Collect stems from this step
		stepStems := listStems(song)
		for _, f := range stepStems {
			// Only add if not already in the list
			found := false
			for _, existing := range allStems {
				if existing.Name == f.Name {
					found = true
					break
				}
			}
			if !found {
				allStems = append(allStems, f)
			}
		}
	}

	// Finalize: mark job as done
	s.jobsMu.Lock()
	if state, ok := s.jobs[job.Song]; ok {
		state.Status = "done"
		state.Files = listStems(song)
		state.CurrentStep = len(steps)
		Log("pipeline", "success", fmt.Sprintf("Pipeline completed: %s (%d stems, %d steps)", job.Song, len(state.Files), len(steps)))
	}
	s.jobsMu.Unlock()
}

// stepTypeDisplay returns a human-readable name for a step type.
func stepTypeDisplay(stepType string) string {
	switch stepType {
	case "vocal", "viperx":
		return "Vocal"
	case "demucs":
		return "Demucs"
	default:
		return stepType
	}
}

// findChainedInput looks for a stem file from the previous step that should be
// used as input to the next step. It looks for stems routed with StemRoute action.
func findChainedInput(outputDir string, step cli.PipelineStep) string {
	// First, look for stems that are explicitly routed
	for stem, route := range step.Stems {
		if route.Action == cli.ActionRoute {
			pattern := filepath.Join(outputDir, "*"+stem+"*")
			matches, _ := filepath.Glob(pattern)
			if len(matches) > 0 {
				return matches[0]
			}
		}
	}

	// Fallback: find any .wav file that looks like an inter-step stem
	// e.g., instrumental.wav (the conventional name)
	patterns := []string{
		filepath.Join(outputDir, "*instrumental*"),
		filepath.Join(outputDir, "*no_vocals*"),
	}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			return matches[0]
		}
	}

	return ""
}

// toInternalContainerPath converts a host path to a container-relative path.
func toInternalContainerPath(hostPath string) string {
	// Convert host output dir to container /app/output/
	projectRoot := resolveProjectRoot()
	if projectRoot != "" && strings.HasPrefix(hostPath, filepath.Join(projectRoot, "output")) {
		rel := strings.TrimPrefix(hostPath, filepath.Join(projectRoot, "output"))
		return "/app/output" + rel
	}
	// Convert host input dir to container /app/input/
	if projectRoot != "" && strings.HasPrefix(hostPath, filepath.Join(projectRoot, "input")) {
		rel := strings.TrimPrefix(hostPath, filepath.Join(projectRoot, "input"))
		return "/app/input" + rel
	}
	return hostPath
}

// logPipelineOutput logs all lines from a pipeline run to the ring buffer.
func logPipelineOutput(output string) {
	baseNano := time.Now().UnixNano()
	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		LogWithNano("pipeline", "info", line, baseNano-int64(i))
	}
}

// listStems reads the output directory for a song and returns the generated files.
func listStems(song string) []FileEntry {
	projectRoot := resolveProjectRoot()
	outputDir := filepath.Join(projectRoot, "output", song)
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return nil
	}
	var files []FileEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		files = append(files, FileEntry{
			Name: name,
			Path: "/api/files/" + song + "/" + name,
		})
	}
	return files
}

// normalizeContainerInput converts a relative filename or a bare input name into
// the container input path /app/input/<basename>. Paths already under /app/ are
// left untouched, and other absolute paths are preserved so the caller can pass
// through explicit host/container paths unchanged.
func normalizeContainerInput(input string) string {
	if input == "" {
		return input
	}
	if strings.HasPrefix(input, "/app/") {
		return input
	}
	if filepath.IsAbs(input) {
		return input
	}
	return "/app/input/" + filepath.Base(input)
}

// buildPipelineArgs constructs the argument list for pipeline.sh from a SeparateRequest.
// If a preset with Steps is referenced, those steps are returned separately for multi-step chaining.
// Returns: song name, pipeline args, list of steps for chaining (if any).
func buildPipelineArgs(req *SeparateRequest) (song string, args []string, steps []cli.PipelineStep) {
	req.Input = normalizeContainerInput(req.Input)
	song = strings.TrimSuffix(filepath.Base(req.Input), filepath.Ext(req.Input))
	containerOutput := "/app/output/" + song

	// --- Resolve preset steps if a named preset is provided ---
	if req.Preset != "" {
		preset, ok := getAllPresets()[req.Preset]
		if ok && len(preset.Steps) > 0 {
			steps = preset.Steps
		}
	}

	// If the request itself has explicit steps, use those (overrides preset lookup)
	if len(req.Steps) > 0 {
		steps = req.Steps
	}

	// --- Multi-step preset handling ---
	if len(steps) > 0 {
		// For multi-step presets, build args for the FIRST step only.
		// The worker will iterate through remaining steps.
		stepArgs := buildStepPipelineArgs(steps[0], req.Input, containerOutput, req.Device)
		args = append(args, stepArgs...)
		args = append(args, "--output", containerOutput)

		// If it's the only step, use the input directly
		// Otherwise, we'll chain
		if len(steps) == 1 {
			args = append(args, req.Input)
		} else {
			// For multi-step, pass input and add --no-clean
			args = append(args, "--no-clean")
			args = append(args, req.Input)
		}
		return song, args, steps
	}

	// --- BACKWARD COMPAT: old format (no steps) ---
	if req.Viperx {
		if req.ViperxKeep != "" {
			args = append(args, "--vocal-keep", req.ViperxKeep)
		}
	}
	if req.Pitch != 0 {
		args = append(args, "--pitch", fmt.Sprintf("%d", req.Pitch))
	}

	// Resolve model paths (pipeline.sh reads inference params from model's YAML)
	vocalModel := req.VocalModel
	if vocalModel == "" {
		vocalModel = req.ViperxModel
	}
	if vocalModel != "" {
		modelDir := resolveModelDir(vocalModel)
		if modelDir == "" {
			// Model not found on disk yet; pass the name through so callers
			// still see the requested --viperx-model flag.
			modelDir = vocalModel
		}
		args = append(args, "--viperx-model", modelDir)
		if isMdxModel(vocalModel) {
			args = append(args, "--vocal-type", "mdx")
		} else if isScnetModel(vocalModel) {
			args = append(args, "--vocal-type", "scnet")
		}
	}
	stemModel := req.StemModel
	if stemModel == "" {
		stemModel = req.DemucsModel
	}
	if stemModel == "" && req.Preset != "" {
		// Fallback: try to find models from old-format preset fields
		preset := getAllPresets()[req.Preset]
		// (old-format presets had StemModel directly, new ones use Steps)
		if preset.Description != "" && len(preset.Steps) == 0 {
			// This shouldn't happen with new format, but keep for safety
		}
	}
	if stemModel == "" && req.Demucs {
		stemModel = "htdemucs_ft"
	}
	if stemModel != "" {
		args = append(args, "--stem-model", stemModel)
		if req.Demucs && len(req.DemucsKeep) > 0 {
			args = append(args, "--demucs-keep", strings.Join(req.DemucsKeep, ","))
		}
	}

	// Demucs-specific flags: use saved config when the request does not override.
	if isDemucsModel(stemModel) {
		cfg := readModelConfigFromYaml(stemModel)
		shifts := req.Shifts
		if shifts <= 0 {
			shifts = cfg.Shifts
		}
		if shifts > 1 {
			args = append(args, "--shifts", fmt.Sprintf("%d", shifts))
		}
		segment := req.DemucsSegment
		if segment <= 0 {
			segment = cfg.Segment
		}
		segment = clampDemucsSegment(segment)
		if segment > 0 {
			args = append(args, "--demucs-segment", fmt.Sprintf("%d", int(segment)))
		}
		jobs := req.Jobs
		if jobs <= 0 {
			jobs = cfg.Jobs
		}
		if jobs > 0 {
			args = append(args, "--jobs", fmt.Sprintf("%d", jobs))
		}
	}
	// Device override (defaults to cuda in pipeline.sh)
	if req.Device != "" && req.Device != "cuda" {
		args = append(args, "--device", req.Device)
	}

	args = append(args, "--output", containerOutput)
	args = append(args, req.Input)
	return song, args, nil
}

// buildStepPipelineArgs builds pipeline.sh arguments for a single PipelineStep.
func buildStepPipelineArgs(step cli.PipelineStep, inputFile, outputDir, device string) []string {
	var args []string

	switch step.Type {
	case "viperx", "vocal":
		args = append(args, "--vocal-model", "BS_Roformer_Viperx")
		// Model
		if step.Model != "" {
			modelDir := resolveModelDir(step.Model)
			if modelDir != "" {
				args = append(args, "--vocal-model", modelDir)
			}
			if isMdxModel(step.Model) {
				args = append(args, "--vocal-type", "mdx")
			} else if isScnetModel(step.Model) {
				args = append(args, "--vocal-type", "scnet")
			}
		}
		// Keep setting based on stem routing
		if step.Stems != nil {
			_, hasVocals := step.Stems["vocals"]
			_, hasInst := step.Stems["instrumental"]
			if hasVocals && hasInst {
				args = append(args, "--vocal-keep", "both")
			} else if hasVocals {
				args = append(args, "--vocal-keep", "vocals")
			} else if hasInst {
				args = append(args, "--vocal-keep", "instrumental")
			}
		}
	case "demucs":
		stemModel := step.Model
		if stemModel == "" {
			stemModel = "htdemucs_ft"
		}
		args = append(args, "--stem-model", stemModel)
		// Stem keep based on routing (preserve a stable stem order)
		if step.Stems != nil {
			var keep []string
			for _, stem := range []string{"drums", "bass", "other", "vocals", "guitar", "piano"} {
				if route, ok := step.Stems[stem]; ok && (route.Action == cli.StemSave || route.Action == cli.ActionRoute) {
					keep = append(keep, stem)
				}
			}
			if len(keep) > 0 {
				args = append(args, "--demucs-keep", strings.Join(keep, ","))
			}
		}
		// Apply saved Demucs config when available.
		if isDemucsModel(stemModel) {
			cfg := readModelConfigFromYaml(stemModel)
			if cfg.Shifts > 1 {
				args = append(args, "--shifts", fmt.Sprintf("%d", cfg.Shifts))
			}
			segment := clampDemucsSegment(cfg.Segment)
			if segment > 0 {
				args = append(args, "--demucs-segment", fmt.Sprintf("%d", int(segment)))
			}
			if cfg.Jobs > 0 {
				args = append(args, "--jobs", fmt.Sprintf("%d", cfg.Jobs))
			}
		}
	}

	// Device override
	if device != "" && device != "cuda" {
		args = append(args, "--device", device)
	}

	args = append(args, "--output", outputDir)
	return args
}

// findRouteTargets returns a list of stem filenames that should be routed to a specific step.
// This is used by the worker to determine which files from step N are inputs to step N+1.
func findRouteTargets(outputDir string, step cli.PipelineStep) []string {
	var targets []string
	for stem, route := range step.Stems {
		if route.Action == cli.ActionRoute || route.Action == cli.StemSave {
			// Look for the stem file in the output directory
			pattern := filepath.Join(outputDir, "*"+stem+"*")
			matches, _ := filepath.Glob(pattern)
			for _, m := range matches {
				targets = append(targets, m)
			}
		}
	}
	return targets
}

// handleGPU returns GPU availability and info from the Docker container.
func (s *Server) handleGPU(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	available, info, _ := checkGPU()

	resp := GPUPresenceResponse{
		Available: available,
		Info:      info,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// handleModels returns the available presets as JSON.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(getAllPresets())
}

// SeparateRequest is the JSON body for POST /api/separate.
type SeparateRequest struct {
	Preset      string `json:"preset"`
	Input       string `json:"input"`
	Output      string `json:"output,omitempty"`
	VocalModel  string `json:"vocal_model,omitempty"`
	ViperxModel string `json:"viperx_model,omitempty"` // alias for VocalModel
	StemModel   string `json:"stem_model,omitempty"`
	DemucsModel string `json:"demucs_model,omitempty"` // alias for StemModel
	Pitch       int    `json:"pitch,omitempty"`

	Viperx     bool     `json:"viperx"`
	ViperxKeep string   `json:"viperx_keep,omitempty"`
	Demucs     bool     `json:"demucs"`
	DemucsKeep []string `json:"demucs_keep,omitempty"`

	// New v2.8.0: explicit steps array for multi-step pipeline chaining
	Steps []cli.PipelineStep `json:"steps,omitempty"`

	// Demucs-specific overrides (optional, only used when model is htdemucs*)
	Shifts        int     `json:"shifts,omitempty"`
	DemucsSegment float64 `json:"demucs_segment,omitempty"`
	Jobs          int     `json:"jobs,omitempty"`
	// Device override (defaults to cuda)
	Device string `json:"device,omitempty"`
}

// ModelConfigResponse is what the config API returns (read from model YAML or defaults).
type ModelConfigResponse struct {
	SegmentSize int     `json:"segment_size"`
	Overlap     float64 `json:"overlap"`
	ChunkSize   int     `json:"chunk_size"`
	BatchSize   int     `json:"batch_size"`
	Device      string  `json:"device"`
	Shifts      int     `json:"shifts"`
	Segment     float64 `json:"segment"`
	Jobs        int     `json:"jobs"`
}

// handleSeparate validates and enqueues a separation job to the sequential queue.
func (s *Server) handleSeparate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	var req SeparateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("invalid JSON: %v", err),
		})
		return
	}

	// Validate preset (optional — if provided, must exist in user presets)
	if req.Preset != "" {
		_, ok := getAllPresets()[req.Preset]
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("unknown preset %q", req.Preset),
			})
			return
		}
	}

	// Build pipeline arguments and extract song name
	// The third return value is the list of steps for multi-step chaining
	song, pipelineArgs, steps := buildPipelineArgs(&req)

	// Compute total pipeline steps
	totalSteps := len(steps)
	if totalSteps == 0 {
		// Old format: count from flags
		if req.Viperx {
			totalSteps++
		}
		if req.Demucs {
			totalSteps++
		}
		if req.Pitch != 0 {
			totalSteps++
		}
		if totalSteps == 0 {
			totalSteps = 2 // default: viperx + demucs
		}
	}

	// Check if song is already in the queue. Allow re-queuing if the previous
	// job is in a terminal state (done or error) so retries don't get stuck.
	s.jobsMu.Lock()
	if existing, exists := s.jobs[song]; exists {
		if existing.Status != "done" && existing.Status != "error" {
			s.jobsMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "song already queued",
			})
			return
		}
	}

	s.jobs[song] = &JobState{Song: song, Status: "waiting", Index: s.nextIndex, TotalSteps: totalSteps}
	s.nextIndex++
	s.jobsMu.Unlock()

	// Enqueue the job (with steps if multi-step)
	s.jobQueue <- JobRequest{Song: song, Args: pipelineArgs, Config: req, Steps: steps}

	Log("backend", "success", "Job queued: "+song)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "queued",
		"song":   song,
	})
}

// resolveModelDir resolves a model name to a directory path usable inside the
// Docker container. For Demucs PyTorch models (htdemucs_ft, htdemucs, etc.)
// the name is returned as-is (loaded by name, not path). For all other models
// (ViperX, Roformer, MDX, etc.), the model is looked up in listModels() and
// its /models/ path is returned directly (both containers use /models).
func resolveModelDir(name string) string {
	if name == "" {
		return ""
	}
	if name == "htdemucs_ft" || (strings.HasPrefix(name, "htdemucs") && !strings.Contains(name, ".onnx")) {
		return name
	}
	models := listModels()
	for _, m := range models.Models {
		if m.Name == name || m.DisplayName == name {
			modelDir := filepath.Dir(m.Path)
			// listModels() reports container paths under /app/models; map them
			// back to the current modelsBasePath so tests that override the
			// base directory get a real filesystem path.
			if strings.HasPrefix(modelDir, "/app/models/") {
				rel, err := filepath.Rel("/app/models", modelDir)
				if err == nil {
					modelDir = filepath.Join(modelsBasePath, rel)
				}
			}
			return modelDir
		}
	}
	return ""
}

// isMdxModel reports whether a model name/path refers to an MDX-C (MDX-Net)
// checkpoint.  It first checks the name for the MDX23C marker, then inspects
// the resolved model directory for checkpoint names or YAML topology fields
// that identify the TFC_TDF_net architecture used by MDX-C models.
func isMdxModel(name string) bool {
	if name == "" {
		return false
	}
	lower := strings.ToLower(name)
	if strings.Contains(lower, "mdx23c") {
		return true
	}

	modelDir := resolveModelDir(name)
	if modelDir == "" || modelDir == name {
		return false
	}
	entries, err := os.ReadDir(modelDir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fn := strings.ToLower(entry.Name())
		if strings.HasSuffix(fn, ".ckpt") && strings.Contains(fn, "mdx23c") {
			return true
		}
		if strings.HasSuffix(fn, ".yaml") || strings.HasSuffix(fn, ".yml") {
			data, err := os.ReadFile(filepath.Join(modelDir, entry.Name()))
			if err != nil {
				continue
			}
			var cfg map[string]interface{}
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				continue
			}
			model, ok := cfg["model"].(map[string]interface{})
			if !ok {
				continue
			}
			if _, ok := model["num_scales"]; ok {
				return true
			}
			if _, ok := model["num_subbands"]; ok {
				return true
			}
			if _, ok := model["num_blocks_per_scale"]; ok {
				return true
			}
		}
	}

	return false
}

// isScnetModel reports whether a model name/path refers to an SCNet checkpoint.
// It first checks the name for the scnet marker, then inspects the resolved
// model directory for checkpoint names or YAML topology fields (band_SR,
// band_stride, band_kernel) that identify the SCNet architecture.
func isScnetModel(name string) bool {
	if name == "" {
		return false
	}
	lower := strings.ToLower(name)
	if strings.Contains(lower, "scnet") {
		return true
	}

	modelDir := resolveModelDir(name)
	if modelDir == "" || modelDir == name {
		return false
	}
	entries, err := os.ReadDir(modelDir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fn := strings.ToLower(entry.Name())
		if strings.HasSuffix(fn, ".ckpt") && strings.Contains(fn, "scnet") {
			return true
		}
		if strings.HasSuffix(fn, ".yaml") || strings.HasSuffix(fn, ".yml") {
			data, err := os.ReadFile(filepath.Join(modelDir, entry.Name()))
			if err != nil {
				continue
			}
			var cfg map[string]interface{}
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				continue
			}
			model, ok := cfg["model"].(map[string]interface{})
			if !ok {
				continue
			}
			if _, ok := model["band_SR"]; ok {
				return true
			}
			if _, ok := model["band_stride"]; ok {
				return true
			}
			if _, ok := model["band_kernel"]; ok {
				return true
			}
		}
	}

	return false
}

// modelConfigsDir returns the directory where per-model YAML configs are stored
// for models that do not have an on-disk directory (e.g. built-in Demucs models).
func modelConfigsDir() string {
	return filepath.Join(resolveProjectRoot(), "config", "model_configs")
}

// modelConfigYamlPath returns the fallback YAML path for a model name.
func modelConfigYamlPath(name string) string {
	return filepath.Join(modelConfigsDir(), name+".yaml")
}

// findModelYaml returns the path to the model's YAML config file, or empty string.
// It first looks inside the model directory; if there is no YAML there, it falls
// back to config/model_configs/<name>.yaml.
func findModelYaml(modelName string) string {
	modelDir := resolveModelDir(modelName)
	if modelDir != "" {
		if info, err := os.Stat(modelDir); err == nil && info.IsDir() {
			for _, ext := range []string{"*.yaml", "*.yml"} {
				matches, err := filepath.Glob(filepath.Join(modelDir, ext))
				if err == nil && len(matches) > 0 {
					return matches[0]
				}
			}
		}
	}

	cfgPath := modelConfigYamlPath(modelName)
	if info, err := os.Stat(cfgPath); err == nil && !info.IsDir() {
		return cfgPath
	}
	return ""
}

// findYamlChildNode finds a child node by key in a mapping node.
func findYamlChildNode(parent *yaml.Node, key string) *yaml.Node {
	if parent.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(parent.Content)-1; i += 2 {
		if parent.Content[i].Value == key {
			return parent.Content[i+1]
		}
	}
	return nil
}

// readModelConfigFromYaml reads inference parameters from a model's YAML file using Go yaml.Node.
// Returns defaults if reading fails or model has no YAML.
func readModelConfigFromYaml(name string) ModelConfigResponse {
	defaults := ModelConfigResponse{
		SegmentSize: 256, Overlap: 0.25, ChunkSize: 0, BatchSize: 0,
		Device: "cuda", Shifts: 1, Segment: 0, Jobs: 0,
	}

	yamlPath := findModelYaml(name)
	if yamlPath == "" {
		return defaults
	}

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return defaults
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		log.Printf("WARN: failed to parse YAML %s: %v", yamlPath, err)
		return defaults
	}

	// doc.Content[0] is the root mapping
	if len(doc.Content) == 0 {
		return defaults
	}
	infNode := findYamlChildNode(doc.Content[0], "inference")
	if infNode == nil || infNode.Kind != yaml.MappingNode {
		return defaults
	}

	dimT := 801
	numOverlap := 4
	batchSize := 1

	if n := findYamlChildNode(infNode, "dim_t"); n != nil {
		if v, err := strconv.Atoi(n.Value); err == nil {
			dimT = v
		}
	}
	if n := findYamlChildNode(infNode, "num_overlap"); n != nil {
		if v, err := strconv.Atoi(n.Value); err == nil {
			numOverlap = v
		}
	}
	if n := findYamlChildNode(infNode, "batch_size"); n != nil {
		if v, err := strconv.Atoi(n.Value); err == nil {
			batchSize = v
		}
	}

	segSize := (dimT - 33) / 3
	if segSize < 1 {
		segSize = 1
	}
	overlap := 0.25
	if numOverlap > 0 {
		overlap = 1.0 / float64(numOverlap)
	}

	resp := ModelConfigResponse{
		SegmentSize: segSize,
		Overlap:     overlap,
		ChunkSize:   0,
		BatchSize:   batchSize,
		Device:      "cuda",
		Shifts:      1,
		Segment:     0,
		Jobs:        0,
	}

	// Read Demucs-specific overrides if present.
	if demNode := findYamlChildNode(doc.Content[0], "demucs"); demNode != nil && demNode.Kind == yaml.MappingNode {
		if n := findYamlChildNode(demNode, "shifts"); n != nil {
			if v, err := strconv.Atoi(n.Value); err == nil {
				resp.Shifts = v
			}
		}
		if n := findYamlChildNode(demNode, "segment"); n != nil {
			if v, err := strconv.ParseFloat(n.Value, 64); err == nil {
				resp.Segment = v
			}
		}
		if n := findYamlChildNode(demNode, "jobs"); n != nil {
			if v, err := strconv.Atoi(n.Value); err == nil {
				resp.Jobs = v
			}
		}
	}

	return resp
}

// writeModelConfigToYaml writes inference parameters to a model's YAML file using Go yaml.Node.
// If the model has no YAML on disk, it creates one at config/model_configs/<name>.yaml.
func writeModelConfigToYaml(name string, cfg ModelConfigResponse) error {
	// Clamp Demucs segment to the valid integer range accepted by the CLI.
	cfg.Segment = clampDemucsSegment(cfg.Segment)

	// Convert segment_size → dim_t, overlap → num_overlap
	dimT := cfg.SegmentSize*3 + 33
	numOverlap := 0
	if cfg.Overlap > 0 {
		numOverlap = int(1.0 / cfg.Overlap)
	}
	if numOverlap < 1 {
		numOverlap = 4
	}
	batchSize := cfg.BatchSize
	if batchSize < 1 {
		batchSize = 1
	}

	yamlPath := findModelYaml(name)
	var doc yaml.Node
	if yamlPath == "" {
		// No YAML yet: create one in the fallback config directory.
		yamlPath = modelConfigYamlPath(name)
		if err := os.MkdirAll(filepath.Dir(yamlPath), 0o755); err != nil {
			return fmt.Errorf("failed to create model config directory: %w", err)
		}
		raw := fmt.Sprintf("inference:\n  dim_t: %d\n  num_overlap: %d\n  batch_size: %d\ndemucs:\n  shifts: %d\n  segment: %s\n  jobs: %d\n",
			dimT, numOverlap, batchSize, cfg.Shifts, strconv.FormatFloat(cfg.Segment, 'f', -1, 64), cfg.Jobs)
		if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
			return fmt.Errorf("failed to seed YAML: %w", err)
		}
	} else {
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			return fmt.Errorf("failed to read YAML: %w", err)
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("failed to parse YAML: %w", err)
		}
	}

	if len(doc.Content) == 0 {
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc.Content = append(doc.Content, root)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("YAML root is not a mapping")
	}

	// Update inference section
	infNode := findYamlChildNode(root, "inference")
	if infNode == nil || infNode.Kind != yaml.MappingNode {
		infNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "inference"},
			infNode,
		)
	}
	setYamlChildInt(infNode, "dim_t", dimT)
	setYamlChildInt(infNode, "num_overlap", numOverlap)
	setYamlChildInt(infNode, "batch_size", batchSize)

	// Update Demucs-specific section
	demNode := findYamlChildNode(root, "demucs")
	if demNode == nil || demNode.Kind != yaml.MappingNode {
		demNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "demucs"},
			demNode,
		)
	}
	setYamlChildInt(demNode, "shifts", cfg.Shifts)
	setYamlChildFloat(demNode, "segment", cfg.Segment)
	setYamlChildInt(demNode, "jobs", cfg.Jobs)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}

	if err := os.WriteFile(yamlPath, out, 0644); err != nil {
		return fmt.Errorf("failed to write YAML: %w", err)
	}
	return nil
}

// setYamlChildInt sets or adds an integer scalar child in a mapping node.
func setYamlChildInt(node *yaml.Node, key string, value int) {
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1].Value = strconv.Itoa(value)
			node.Content[i+1].Tag = "!!int"
			node.Content[i+1].Style = 0
			return
		}
	}
	// Not found, append
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: strconv.Itoa(value), Tag: "!!int"},
	)
}

// setYamlChildFloat sets or adds a scalar child in a mapping node. Whole-number
// values are written as integers so the YAML type matches what callers such as
// the demucs CLI expect.
func setYamlChildFloat(node *yaml.Node, key string, value float64) {
	formatted := strconv.FormatFloat(value, 'f', -1, 64)
	tag := "!!float"
	if value == math.Trunc(value) {
		tag = "!!int"
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1].Value = formatted
			node.Content[i+1].Tag = tag
			node.Content[i+1].Style = 0
			return
		}
	}
	// Not found, append
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: formatted, Tag: tag},
	)
}

// setYamlChild sets or adds a scalar child in a mapping node.
func setYamlChild(node *yaml.Node, key, value string) {
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1].Value = value
			node.Content[i+1].Tag = "!!str"
			return
		}
	}
	// Not found, append
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: "!!str", Style: yaml.DoubleQuotedStyle},
	)
}

// clampDemucsSegment clamps a Demucs segment value to the valid integer range
// accepted by the demucs CLI. The model internal limit is 7.8 seconds but the
// CLI only accepts whole seconds, so the maximum configurable value is 7.
// Zero means "auto" and is returned as-is; values are rounded to the nearest
// integer and limited to [1, 7].
func clampDemucsSegment(v float64) float64 {
	if v <= 0 {
		return 0
	}
	rounded := math.Round(v)
	if rounded < 1 {
		return 1
	}
	if rounded > 7 {
		return 7
	}
	return rounded
}

// handleModelsConfig saves or retrieves per-model inference configuration.
// GET  /api/models/{name}/config  — reads inference params from the model's YAML
// POST /api/models/{name}/config  — writes inference params to the model's YAML
func (s *Server) handleModelsConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	name := r.PathValue("name")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing model name"})
		return
	}

	if r.Method == http.MethodGet {
		cfg := readModelConfigFromYaml(name)
		json.NewEncoder(w).Encode(cfg)
		return
	}

	if r.Method == http.MethodPost {
		var cfg ModelConfigResponse
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
			return
		}

		// Validate
		if cfg.SegmentSize <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "segment_size must be > 0"})
			return
		}
		if cfg.Overlap < 0 || cfg.Overlap >= 1 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "overlap must be >= 0 and < 1"})
			return
		}
		if cfg.Device != "" && cfg.Device != "cpu" && cfg.Device != "cuda" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "device must be 'cpu' or 'cuda'"})
			return
		}

		// Demucs segment is limited to whole seconds in [0, 7]; clamp defensively
		// so the value can never exceed what the CLI accepts.
		cfg.Segment = clampDemucsSegment(cfg.Segment)

		if err := writeModelConfigToYaml(name, cfg); err != nil {
			log.Printf("ERROR: failed to save model config for %s: %v", name, err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		Log("backend", "success", "Config saved to YAML: "+name)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"ok":     "true",
			"detail": fmt.Sprintf("config saved to YAML for model %s", name),
		})
		return
	}

	w.WriteHeader(http.StatusMethodNotAllowed)
	json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
}

// safeFilenamePattern matches names composed only of letters, digits, spaces,
// hyphens, underscores, dots and parentheses. Anything else (including path
// separators and control characters) is rejected.
var safeFilenamePattern = regexp.MustCompile(`^[A-Za-z0-9\s\-_\.\(\)]+$`)

// audioExtensions is the set of audio extensions considered valid for uploads.
// It is built from audio.SupportedExtensions for a single source of truth.
var audioExtensions = func() map[string]bool {
	m := make(map[string]bool)
	for _, ext := range audio.SupportedExtensions() {
		m[strings.ToLower(ext)] = true
	}
	return m
}()

// validateUploadFilename validates a raw upload filename. It rejects:
//   - empty names or names with path separators / traversal sequences
//   - names containing characters outside a small safe set
//   - names ending in a non-audio extension
//   - names with consecutive audio extensions (e.g. song.mp3.flac)
func validateUploadFilename(name string) error {
	if name == "" {
		return errors.New("empty filename")
	}

	// filepath.Base removes any directory prefix, but we still reject names
	// that explicitly contain separators or traversal markers.
	base := filepath.Base(name)
	if base != name || strings.Contains(name, "\\") || strings.Contains(name, "/") {
		return errors.New("path separators not allowed")
	}
	if strings.Contains(name, "..") {
		return errors.New("path traversal not allowed")
	}
	if !safeFilenamePattern.MatchString(name) {
		return errors.New("filename contains invalid characters")
	}

	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return errors.New("missing file extension")
	}
	if !audioExtensions[ext] {
		return errors.New("unsupported audio extension")
	}

	// Reject double audio extensions such as song.mp3.flac.
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if prevExt := strings.ToLower(filepath.Ext(stem)); prevExt != "" && audioExtensions[prevExt] {
		return errors.New("consecutive audio extensions not allowed")
	}

	return nil
}

// extractRawUploadFilename returns the raw filename parameter from the first
// multipart file part in r without Go's path normalization. It restores r.Body
// so that r.FormFile can still be called afterwards.
func extractRawUploadFilename(r *http.Request) (string, error) {
	contentType := r.Header.Get("Content-Type")
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", err
	}
	boundary, ok := params["boundary"]
	if !ok {
		return "", errors.New("missing multipart boundary")
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if part.FormName() != "file" {
			continue
		}
		disp := part.Header.Get("Content-Disposition")
		_, dispParams, err := mime.ParseMediaType(disp)
		if err != nil {
			return "", err
		}
		if raw := dispParams["filename"]; raw != "" {
			return raw, nil
		}
	}
	return "", errors.New("no file part found")
}

// handleUpload accepts a multipart file upload and saves it to disk.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Validate the raw filename before Go's multipart parser normalizes away
	// path traversal markers such as "../".
	rawName, err := extractRawUploadFilename(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "no file provided"})
		Log("backend", "error", "Upload failed: "+err.Error())
		return
	}
	if err := validateUploadFilename(rawName); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		Log("backend", "error", "Upload rejected: "+err.Error())
		return
	}

	// Determine input directory: prefer project root /input,
	// fall back to a temp dir if it doesn't exist.
	projectRoot := resolveProjectRoot()
	inputDir := filepath.Join(projectRoot, "input")
	if _, err := os.Stat(inputDir); os.IsNotExist(err) {
		os.MkdirAll(inputDir, 0o755)
	}
	// Fix permissions so container user (1000:1000) can write
	os.Chmod(inputDir, 0777)

	// Check disk space before parsing the form (500MB max)
	var diskStat syscall.Statfs_t
	if err := syscall.Statfs(inputDir, &diskStat); err == nil {
		freeBytes := diskStat.Bavail * uint64(diskStat.Bsize)
		if freeBytes < 500<<20 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInsufficientStorage)
			json.NewEncoder(w).Encode(map[string]string{"error": "disk space too low for upload"})
			return
		}
	}

	// Limit to 500MB
	r.ParseMultipartForm(500 << 20)
	file, header, err := r.FormFile("file")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "no file provided"})
		Log("backend", "error", "Upload failed: "+err.Error())
		return
	}
	defer file.Close()

	safeName := filepath.Base(header.Filename)
	destPath := filepath.Join(inputDir, safeName)
	dst, err := os.Create(destPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to save file"})
		Log("backend", "error", "Upload failed: "+err.Error())
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to write file"})
		Log("backend", "error", "Upload failed: "+err.Error())
		return
	}

	Log("backend", "success", "Uploaded: "+safeName)

	// The path inside the container is /app/input/filename
	containerPath := "/app/input/" + safeName
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"path": containerPath})
}

// handleUploadPitch accepts a multipart file upload and saves it to input_rubberband/.
func (s *Server) handleUploadPitch(w http.ResponseWriter, r *http.Request) {
	// Validate the raw filename before Go's multipart parser normalizes away
	// path traversal markers such as "../".
	rawName, err := extractRawUploadFilename(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "no file provided"})
		Log("backend", "error", "Pitch upload failed: "+err.Error())
		return
	}
	if err := validateUploadFilename(rawName); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		Log("backend", "error", "Pitch upload rejected: "+err.Error())
		return
	}

	projectRoot := resolveProjectRoot()
	inputDir := filepath.Join(projectRoot, "input_rubberband")
	if _, err := os.Stat(inputDir); os.IsNotExist(err) {
		os.MkdirAll(inputDir, 0o755)
	}
	// Fix permissions so container user (1000:1000) can write
	os.Chmod(inputDir, 0777)

	var diskStat syscall.Statfs_t
	if err := syscall.Statfs(inputDir, &diskStat); err == nil {
		freeBytes := diskStat.Bavail * uint64(diskStat.Bsize)
		if freeBytes < 500<<20 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInsufficientStorage)
			json.NewEncoder(w).Encode(map[string]string{"error": "disk space too low for upload"})
			return
		}
	}

	r.ParseMultipartForm(500 << 20)
	file, header, err := r.FormFile("file")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "no file provided"})
		Log("backend", "error", "Pitch upload failed: "+err.Error())
		return
	}
	defer file.Close()

	safeName := filepath.Base(header.Filename)
	destPath := filepath.Join(inputDir, safeName)
	dst, err := os.Create(destPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to save file"})
		Log("backend", "error", "Pitch upload failed: "+err.Error())
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to write file"})
		Log("backend", "error", "Pitch upload failed: "+err.Error())
		return
	}

	Log("backend", "success", "Pitch upload: "+safeName)

	containerPath := "/app/input_rubberband/" + safeName
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"path": containerPath})
}

// handleModelsCatalog returns the UVR model catalog (uvr_models.json) with
// per-model downloaded flags by comparing against the filesystem.
// GET /api/models/catalog
func (s *Server) handleModelsCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	// Try the container path first, then fall back to the project root
	data, err := os.ReadFile("/app/uvr_models.json")
	if err != nil {
		projectRoot := resolveProjectRoot()
		data, err = os.ReadFile(filepath.Join(projectRoot, "uvr_models.json"))
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "failed to read uvr_models.json catalog file",
			})
			return
		}
	}

	var catalog []UVRModelEntry
	if err := json.Unmarshal(data, &catalog); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "failed to parse uvr_models.json: " + err.Error(),
		})
		return
	}

	// Build a set of downloaded model filenames from the filesystem
	downloadedFiles := make(map[string]bool)
	for _, m := range listModels().Models {
		// Match without extension (m.Name is stripped of extension)
		downloadedFiles[m.Name] = true
		// Also try common extensions to match against Filename in catalog
		for _, ext := range []string{".pth", ".onnx", ".ckpt", ".th", ".safetensors", ".yaml"} {
			downloadedFiles[m.Name+ext] = true
		}
	}

	// Tag each catalog entry as downloaded if its filename exists on disk
	for i := range catalog {
		catalog[i].Source = "uvr"
		if downloadedFiles[catalog[i].Filename] ||
			downloadedFiles[catalog[i].Name] {
			catalog[i].Downloaded = true
		}
	}

	// Filter out entries with size_mb == 0 (yaml configs, dependencies).
	// They are still needed in uvr_models.json for dependency resolution
	// during downloads, but should not appear in the catalog UI.
	filtered := make([]UVRModelEntry, 0, len(catalog))
	for _, entry := range catalog {
		if entry.SizeMB > 0 {
			filtered = append(filtered, entry)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(filtered)
}

// handlePitchFileServe serves generated pitch-shifted files from the output directory.
// GET /api/pitch/files/{song}/{pitch}/{file}
func (s *Server) handlePitchFileServe(w http.ResponseWriter, r *http.Request) {
	song := r.PathValue("song")
	pitchStr := r.PathValue("pitch")
	file := r.PathValue("file")

	// Path traversal guard
	song = filepath.Clean(song)
	pitchStr = filepath.Clean(pitchStr)
	file = filepath.Clean(file)
	if strings.Contains(file, "..") || strings.Contains(song, "..") || strings.Contains(pitchStr, "..") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	projectRoot := resolveProjectRoot()
	outputBase := filepath.Join(projectRoot, "output")

	// Build path: /output/{song}/{song}_pitch{pitch}/{file}
	filePath := filepath.Join(outputBase, song, song+"_pitch"+pitchStr, file)

	// Verify the file is inside the output directory
	absPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(absPath, filepath.Clean(outputBase)+string(filepath.Separator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	http.ServeFile(w, r, absPath)
}

// handleFileServe serves generated output files from the project output directory.
func (s *Server) handleFileServe(w http.ResponseWriter, r *http.Request) {
	song := r.PathValue("song")
	file := r.PathValue("file")

	// Prevent directory traversal
	song = filepath.Clean(song)
	file = filepath.Clean(file)

	projectRoot := resolveProjectRoot()
	filePath := filepath.Join(projectRoot, "output", song, file)

	// Verify the file is inside the output directory
	outputPrefix := filepath.Join(projectRoot, "output")
	absPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(absPath, outputPrefix+string(filepath.Separator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	http.ServeFile(w, r, absPath)
}

// handleBackendStart is no longer supported in unified container mode.
func (s *Server) handleBackendStart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":  "no longer supported in unified container",
		"detail": "Backend is already running (unified container)",
	})
}

// handleBackendStop is no longer supported in unified container mode.
func (s *Server) handleBackendStop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":  "no longer supported in unified container",
		"detail": "Stop not applicable in unified container",
	})
}

// handleBackendRestart is no longer supported in unified container mode.
func (s *Server) handleBackendRestart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":  "no longer supported in unified container",
		"detail": "Restart not applicable in unified container",
	})
}

// handleDeleteSong deletes the entire output directory for a song.
func (s *Server) handleDeleteSong(w http.ResponseWriter, r *http.Request) {
	song := r.PathValue("song")
	song = filepath.Clean(song)

	projectRoot := resolveProjectRoot()
	dirPath := filepath.Join(projectRoot, "output", song)

	// Verify inside output/
	outputPrefix := filepath.Join(projectRoot, "output")
	absPath, err := filepath.Abs(dirPath)
	if err != nil || !strings.HasPrefix(absPath, outputPrefix+string(filepath.Separator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "song not found"})
		return
	}

	if err := os.RemoveAll(absPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Remove job from queue tracking if present
	s.jobsMu.Lock()
	delete(s.jobs, song)
	s.jobsMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"deleted": true, "song": song})
}

// handleDeleteFile deletes a single file within the output directory.
func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	if file == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing file parameter"})
		return
	}

	file = filepath.Clean(file)

	// B6: Prevent deleting files inside pitch subdirectories via this endpoint
	if strings.Contains(filepath.Dir(file), "_pitch") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "cannot delete files in pitch subdirectories"})
		return
	}

	projectRoot := resolveProjectRoot()
	filePath := filepath.Join(projectRoot, "output", file)

	// Verify inside output/
	outputPrefix := filepath.Join(projectRoot, "output")
	absPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(absPath, outputPrefix+string(filepath.Separator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "file not found"})
		return
	}

	if err := os.Remove(absPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Don't clear the status file for single-stem deletion; the pipeline is still valid.
	// Only handleDeleteSong (the whole directory delete) clears the status.

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"deleted": true, "file": file})
}

// handleDeleteInput deletes a file from the input directory.
// DELETE /api/inputs/{name}
func (s *Server) handleDeleteInput(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	name = filepath.Clean(name)

	projectRoot := resolveProjectRoot()
	filePath := filepath.Join(projectRoot, "input", name)

	// Verify inside input/
	inputPrefix := filepath.Join(projectRoot, "input")
	absPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(absPath, inputPrefix) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "input file not found"})
		return
	}

	if err := os.Remove(absPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	Log("backend", "info", "Deleted input: "+name)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"deleted": true, "file": name})
}

// handleDeletePitchUpload deletes a file from input_rubberband/.
// DELETE /api/uploads/pitch/{name}
func (s *Server) handleDeletePitchUpload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	name = filepath.Clean(name)

	projectRoot := resolveProjectRoot()
	filePath := filepath.Join(projectRoot, "input_rubberband", name)

	// Verify inside input_rubberband/
	inputPrefix := filepath.Join(projectRoot, "input_rubberband")
	absPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(absPath, inputPrefix) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "pitch upload not found"})
		return
	}

	if err := os.Remove(absPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	Log("backend", "info", "Deleted pitch upload: "+name)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"deleted": true, "file": name})
}


