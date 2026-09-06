package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"
)

// ProcessInfo describes the live state of a single OS process.
type ProcessInfo struct {
	Alive      bool   `json:"alive"`
	Pid        int    `json:"pid"`
	Cmd        string `json:"cmd"`
	ElapsedSec int    `json:"elapsed_sec"`
}

// collectProcessState reports whether pid is alive, its command line and how
// many seconds have elapsed since it started. It returns Alive=false when the
// process does not exist or its /proc entry is unreadable.
func collectProcessState(pid int) ProcessInfo {
	info := ProcessInfo{Pid: pid}
	if pid <= 0 {
		return info
	}

	// Verify the process exists by sending signal 0.
	proc, err := os.FindProcess(pid)
	if err != nil {
		return info
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return info
	}

	// The creation time of /proc/<pid> approximates the process start time.
	procDir := fmt.Sprintf("/proc/%d", pid)
	stat, err := os.Stat(procDir)
	if err != nil {
		return info
	}

	cmdline, err := os.ReadFile(procDir + "/cmdline")
	if err != nil {
		// Process exists but cmdline unreadable; still report alive.
		info.Alive = true
		info.ElapsedSec = int(time.Since(stat.ModTime()).Seconds())
		if info.ElapsedSec < 0 {
			info.ElapsedSec = 0
		}
		return info
	}
	info.Cmd = strings.ReplaceAll(string(cmdline), "\x00", " ")
	info.Cmd = strings.TrimSpace(info.Cmd)

	info.ElapsedSec = int(time.Since(stat.ModTime()).Seconds())
	if info.ElapsedSec < 0 {
		info.ElapsedSec = 0
	}
	info.Alive = true
	return info
}

// handleProcessStatus serves GET /api/processes/status with a real-time view
// of queued jobs, GPU memory and the currently running pipeline process.
func (s *Server) handleProcessStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("method %s not allowed", r.Method),
		})
		return
	}

	jobs := s.collectQueueJobs()

	gpu := getGPUInfo()
	gpuObj := map[string]interface{}{
		"total_mb": gpu.VRAMTotalMB,
		"used_mb":  gpu.VRAMUsedMB,
		"free_mb":  gpu.VRAMFreeMB,
		"runtime":  gpu.Runtime,
		"name":     gpu.Name,
	}

	var blocked []map[string]string
	for _, j := range jobs {
		if j.Status == "blocked_no_gpu" {
			blocked = append(blocked, map[string]string{
				"song":    j.Song,
				"message": j.BlockedReasonMsg,
			})
		}
	}

	s.jobsMu.RLock()
	pid := s.currentPID
	s.jobsMu.RUnlock()

	resp := map[string]interface{}{
		"queue_jobs":       jobs,
		"gpu":              gpuObj,
		"pipeline_process": collectProcessState(pid),
		"blocked":          blocked,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
