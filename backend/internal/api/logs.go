package api

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
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

func (s *Server) handleGetServiceLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	services := []string{"onda", "onda-gui"}
	var allLogs []LogEntry

	baseNano := time.Now().UnixNano()
	var lineIdx int64

	for _, svc := range services {
		cmd := exec.Command("docker", "logs", "--tail", "50", svc)
		out, err := cmd.CombinedOutput()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			level := "info"
			lower := strings.ToLower(line)
			if strings.Contains(lower, "error") || strings.Contains(lower, "❌") || strings.Contains(lower, "fail") || strings.Contains(lower, "traceback") {
				level = "error"
			}
			allLogs = append(allLogs, LogEntry{
				Nano:    baseNano - lineIdx,
				Level:   level,
				Service: svc,
				Message: line,
			})
			lineIdx++
		}
	}

	json.NewEncoder(w).Encode(allLogs)
}
