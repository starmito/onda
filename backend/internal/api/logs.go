package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const maxLogEntries = 200
const defaultMaxLogFileSize = 2 * 1024 * 1024

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
	defaultLogStore *serviceLogStore
	logIDSeq        atomic.Uint64
)

func init() {
	defaultLogStore = newServiceLogStore(defaultServiceLogPath())
}

// defaultServiceLogPath returns the persistent log file path. It can be
// overridden with the ONDA_SERVICE_LOG_PATH environment variable, mainly for
// tests.
func defaultServiceLogPath() string {
	if p := os.Getenv("ONDA_SERVICE_LOG_PATH"); p != "" {
		return p
	}
	return "/app/logs/onda.log"
}

// newServiceLogStore creates a file-backed log store. It attempts to create
// the parent directory but never fails: if the directory is not writable the
// store simply drops entries silently.
func newServiceLogStore(path string) *serviceLogStore {
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	return &serviceLogStore{
		path:    path,
		maxSize: defaultMaxLogFileSize,
	}
}

// serviceLogStore persists log entries to an append-only JSON-lines file with
// simple single-generation rotation.
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
		_ = os.Rename(st.path, st.path+".1")
	}
}

// readAll returns every entry stored in the current log file and the previous
// rotated generation (.1). Entries are returned in file order (oldest first).
func (st *serviceLogStore) readAll() []LogEntry {
	st.mu.Lock()
	defer st.mu.Unlock()

	var entries []LogEntry
	for _, p := range []string{st.path + ".1", st.path} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			var e LogEntry
			if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
				continue
			}
			entries = append(entries, e)
		}
		_ = f.Close()
	}
	return entries
}

func newLogID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), logIDSeq.Add(1))
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

	defaultLogStore.persist(entry)
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

	defaultLogStore.persist(entry)
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

	persisted := defaultLogStore.readAll()

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
