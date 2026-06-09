package api

import (
	"encoding/json"
	"net/http"
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
