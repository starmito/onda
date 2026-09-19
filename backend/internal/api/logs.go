package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxLogEntries = 200
const defaultMaxLogFileSize = 2 * 1024 * 1024
const serviceLogRetention = 7 * 24 * time.Hour
const serviceLogTimeSuffix = "20060102-150405"

// LogEntry represents a single log line. ID is used for deduplication when
// the in-memory ring buffer is merged with the persisted log file.
type LogEntry struct {
	ID      string `json:"id,omitempty"`
	Nano    int64  `json:"nano"`
	Level   string `json:"level"`
	Service string `json:"service"`
	Message string `json:"message"`
}

var (
	logBuffer   []LogEntry
	logBufferMu sync.RWMutex
)

var (
	defaultLogStore   *serviceLogStore
	defaultLogStoreMu sync.Mutex
	logIDSeq          atomic.Uint64
)

// currentLogStore returns the default service log store, recreating it when the
// configured log path changes (for example after a data-root switch). This
// keeps log persistence tied to the effective data root at call time.
func currentLogStore() *serviceLogStore {
	defaultLogStoreMu.Lock()
	defer defaultLogStoreMu.Unlock()
	wantPath := defaultServiceLogPath()
	if defaultLogStore != nil && defaultLogStore.path == wantPath {
		return defaultLogStore
	}
	defaultLogStore = newServiceLogStore(wantPath)
	return defaultLogStore
}

// defaultServiceLogPath returns the persistent log file path. It can be
// overridden with the ONDA_SERVICE_LOG_PATH environment variable, mainly for
// tests.
func defaultServiceLogPath() string {
	if p := os.Getenv("ONDA_SERVICE_LOG_PATH"); p != "" {
		return p
	}
	return filepath.Join(mustSub("logs"), "onda.log")
}

// newServiceLogStore creates a file-backed log store. It attempts to create
// the parent directory but never fails: if the directory is not writable the
// store simply drops entries silently.
func newServiceLogStore(path string) *serviceLogStore {
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	st := &serviceLogStore{
		path:    path,
		maxSize: defaultMaxLogFileSize,
	}
	st.purge()
	return st
}

// serviceLogStore persists log entries to an append-only JSON-lines file with
// time-stamped rotations and age-based retention.
type serviceLogStore struct {
	path    string
	maxSize int64
	mu      sync.Mutex
}

// persist writes the entry to the log file and rotates it when it grows past
// maxSize. Errors are ignored: logging must never break the application.
func (st *serviceLogStore) persist(entry LogEntry) {
	st.mu.Lock()
	defer st.mu.Unlock()

	f, err := os.OpenFile(st.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	// Encode writes one JSON object per line plus a newline.
	_ = json.NewEncoder(f).Encode(entry)

	info, err := f.Stat()
	_ = f.Close()
	if err == nil && info.Size() > st.maxSize {
		st.rotateLocked()
	}
	st.purgeLocked()
}

// rotateLocked moves the active log file to a timestamped backup. If a
// generation already exists for the current second, a numeric counter is
// appended so no history is overwritten. The caller must hold st.mu.
func (st *serviceLogStore) rotateLocked() {
	suffix := time.Now().Format(serviceLogTimeSuffix)
	rotated := st.path + "." + suffix
	for i := 1; ; i++ {
		if _, err := os.Stat(rotated); os.IsNotExist(err) {
			break
		}
		rotated = fmt.Sprintf("%s.%s.%d", st.path, suffix, i)
	}
	_ = os.Rename(st.path, rotated)
}

// purgeLocked removes rotated generations older than serviceLogRetention. The
// active log file is never removed. The caller must hold st.mu.
func (st *serviceLogStore) purgeLocked() {
	dir := filepath.Dir(st.path)
	base := filepath.Base(st.path)
	cutoff := time.Now().Add(-serviceLogRetention)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if name == base || !strings.HasPrefix(name, base+".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// purge is the public wrapper for purgeLocked, used during initialization.
func (st *serviceLogStore) purge() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.purgeLocked()
}

// readRecent returns the most recent log entries, reading the active file and
// then rotated generations from newest to oldest until the limit is reached.
// A limit <= 0 means no limit and reads every generation.
func (st *serviceLogStore) readRecent(limit int) []LogEntry {
	st.mu.Lock()
	defer st.mu.Unlock()

	dir := filepath.Dir(st.path)
	base := filepath.Base(st.path)
	rotated, err := listRotatedFiles(dir, base)
	if err != nil {
		rotated = nil
	}

	var entries []LogEntry
	entries = append(entries, st.readFileLocked(st.path)...)

	if limit <= 0 {
		for _, p := range rotated {
			entries = append(entries, st.readFileLocked(p)...)
		}
		return entries
	}

	if len(entries) >= limit {
		return entries
	}
	for _, p := range rotated {
		entries = append(entries, st.readFileLocked(p)...)
		if len(entries) >= limit {
			break
		}
	}
	return entries
}

// readFileLocked reads every JSON-lines entry from path. The caller must hold
// st.mu.
func (st *serviceLogStore) readFileLocked(path string) []LogEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []LogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var e LogEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries
}

// listRotatedFiles returns the rotated log files for base in this directory,
// ordered from newest to oldest by modification time.
func listRotatedFiles(dir, base string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type fileInfo struct {
		path string
		mt   time.Time
	}
	var files []fileInfo
	for _, e := range entries {
		name := e.Name()
		if name == base || !strings.HasPrefix(name, base+".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: filepath.Join(dir, name), mt: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].mt.After(files[j].mt)
	})
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.path
	}
	return paths, nil
}

func newLogID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), logIDSeq.Add(1))
}

// clientIP returns the originating client IP from common proxy headers or the
// request's RemoteAddr. It never returns the port.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	if ip := r.Header.Get("X-Real-Ip"); ip != "" {
		return strings.TrimSpace(ip)
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host != "" {
		return host
	}
	return r.RemoteAddr
}

// logDeletion writes a structured, grep-friendly audit entry for every deletion.
// kind describes what was deleted, name is a repo-relative path, and files/bytes
// quantify the removed data. The client IP and User-Agent are taken from r.
func logDeletion(r *http.Request, kind, name string, files, bytes int64) {
	ip := clientIP(r)
	ua := strings.ReplaceAll(r.UserAgent(), `"`, `\"`)
	Log("backend", "info", fmt.Sprintf(`Deletion: kind=%s name=%q files=%d bytes=%d ip=%s ua=%q`, kind, name, files, bytes, ip, ua))
}

// Log añade una entrada al ring buffer en memoria y la persiste en disco.
func Log(service, level, message string) {
	entry := LogEntry{
		ID:      newLogID(),
		Nano:    time.Now().UnixNano(),
		Level:   level,
		Service: service,
		Message: message,
	}

	logBufferMu.Lock()
	logBuffer = append(logBuffer, entry)
	if len(logBuffer) > maxLogEntries {
		logBuffer = logBuffer[len(logBuffer)-maxLogEntries:]
	}
	logBufferMu.Unlock()

	currentLogStore().persist(entry)
}

// LogWithNano añade una entrada al ring buffer con un timestamp específico y
// la persiste en disco.
func LogWithNano(service, level, message string, nano int64) {
	entry := LogEntry{
		ID:      newLogID(),
		Nano:    nano,
		Level:   level,
		Service: service,
		Message: message,
	}

	logBufferMu.Lock()
	logBuffer = append(logBuffer, entry)
	if len(logBuffer) > maxLogEntries {
		logBuffer = logBuffer[len(logBuffer)-maxLogEntries:]
	}
	logBufferMu.Unlock()

	currentLogStore().persist(entry)
}

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	logBufferMu.RLock()
	defer logBufferMu.RUnlock()
	// Devolver los más recientes primero
	result := make([]LogEntry, len(logBuffer))
	for i, entry := range logBuffer {
		result[len(logBuffer)-1-i] = entry
	}
	json.NewEncoder(w).Encode(result)
}

// ondaServices are the service names whose logs belong in the Services tab.
// Legacy services such as nginx are intentionally excluded.
var ondaServices = map[string]bool{
	"backend":   true,
	"pipeline":  true,
	"inference": true,
	"onda":      true,
}

func (s *Server) handleGetServiceLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	limit := maxLogEntries
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	persisted := currentLogStore().readRecent(limit)

	logBufferMu.RLock()
	buf := make([]LogEntry, len(logBuffer))
	copy(buf, logBuffer)
	logBufferMu.RUnlock()

	// Deduplicate entries that exist both in the file and in memory using the
	// entry ID. The file is the source of truth for persisted entries.
	seen := make(map[string]struct{}, len(persisted))
	for _, e := range persisted {
		seen[e.ID] = struct{}{}
	}

	combined := make([]LogEntry, 0, len(persisted)+len(buf))
	combined = append(combined, persisted...)
	for _, e := range buf {
		if _, ok := seen[e.ID]; ok {
			continue
		}
		seen[e.ID] = struct{}{}
		combined = append(combined, e)
	}

	// Most recent first.
	sort.Slice(combined, func(i, j int) bool {
		return combined[i].Nano > combined[j].Nano
	})

	result := make([]LogEntry, 0, min(len(combined), limit))
	for _, entry := range combined {
		if !ondaServices[entry.Service] {
			continue
		}
		result = append(result, entry)
		if len(result) >= limit {
			break
		}
	}

	json.NewEncoder(w).Encode(result)
}
