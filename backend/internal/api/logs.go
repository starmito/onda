package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
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

// Formatos de timestamp en logs nginx:
//   access: [09/Jun/2026:10:44:36 +0000]
//   error:  2026/06/09 10:44:36 [error] ...
const nginxAccessLayout = "02/Jan/2006:15:04:05 -0700"
const nginxErrorLayout = "2006/01/02 15:04:05"

// readLocalFileLogs reads the last N lines from a local log file.
func readLocalFileLogs(path string, maxLines int) ([]LogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	// Use a larger buffer for long nginx log lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Take only the last maxLines
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	fallbackNano := time.Now().UnixNano()
	var fallbackIdx int64

	var entries []LogEntry
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		level := "info"
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") || strings.Contains(lower, "❌") || strings.Contains(lower, "fail") || strings.Contains(lower, "traceback") {
			level = "error"
		}

		// Intentar parsear timestamp real del log
		nano := int64(0)
		parsed := false

		// 1) Nginx access: buscar [XX/XXX/YYYY:HH:MM:SS TZ]
		if start := strings.Index(line, "["); start >= 0 {
			if end := strings.Index(line[start:], "]"); end >= 0 {
				ts := line[start+1 : start+end]
				if t, err2 := time.Parse(nginxAccessLayout, ts); err2 == nil {
					nano = t.UnixNano()
					parsed = true
				}
			}
		}

		// 2) Nginx error: buscar YYYY/MM/DD HH:MM:SS al inicio
		if !parsed && len(line) >= 19 {
			ts := line[:19]
			if t, err2 := time.Parse(nginxErrorLayout, ts); err2 == nil {
				nano = t.UnixNano()
				parsed = true
			}
		}

		if !parsed {
			nano = fallbackNano - fallbackIdx
			fallbackIdx++
		}

		entries = append(entries, LogEntry{
			Nano:    nano,
			Level:   level,
			Service: "nginx",
			Message: line,
		})
	}

	return entries, nil
}

func (s *Server) handleGetServiceLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var allLogs []LogEntry

	// Read nginx access log
	accessEntries, err := readLocalFileLogs("/tmp/nginx-access.log", 50)
	if err == nil {
		allLogs = append(allLogs, accessEntries...)
	}

	// Read nginx error log
	errorEntries, err := readLocalFileLogs("/tmp/nginx-error.log", 50)
	if err == nil {
		allLogs = append(allLogs, errorEntries...)
	}

	json.NewEncoder(w).Encode(allLogs)
}
