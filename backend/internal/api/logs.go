package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const maxLogEntries = 200

type LogEntry struct {
	Nano    int64  `json:"nano"`
	Level   string `json:"level"`
	Service string `json:"service"`
	Message string `json:"message"`
}

var (
	logBuffer   []LogEntry
	logBufferMu sync.RWMutex
)

// Log añade una entrada al ring buffer.
// Si se superan maxLogEntries, elimina la más antigua.
func Log(service, level, message string) {
	logBufferMu.Lock()
	defer logBufferMu.Unlock()
	entry := LogEntry{
		Nano:    time.Now().UnixNano(),
		Level:   level,
		Service: service,
		Message: message,
	}
	logBuffer = append(logBuffer, entry)
	if len(logBuffer) > maxLogEntries {
		logBuffer = logBuffer[len(logBuffer)-maxLogEntries:]
	}
}

// LogWithNano añade una entrada al ring buffer con un timestamp específico.
func LogWithNano(service, level, message string, nano int64) {
	logBufferMu.Lock()
	defer logBufferMu.Unlock()
	entry := LogEntry{
		Nano:    nano,
		Level:   level,
		Service: service,
		Message: message,
	}
	logBuffer = append(logBuffer, entry)
	if len(logBuffer) > maxLogEntries {
		logBuffer = logBuffer[len(logBuffer)-maxLogEntries:]
	}
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

	logBufferMu.RLock()
	defer logBufferMu.RUnlock()

	result := make([]LogEntry, 0, min(len(logBuffer), limit))
	for i := len(logBuffer) - 1; i >= 0; i-- {
		entry := logBuffer[i]
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
