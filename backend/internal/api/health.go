package api

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// GPUPresenceResponse es la respuesta del endpoint /api/gpu.
type GPUPresenceResponse struct {
	Available bool   `json:"available"`
	Info      string `json:"info"`
}

// checkGPU verifica si hay GPU NVIDIA vía PyTorch.
func checkGPU() (bool, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	script := "import torch; d=torch.cuda.get_device_properties(0) if torch.cuda.is_available() else None; print(f'{d.name}, {torch.cuda.memory_allocated(0)//1024//1024} MiB, {torch.cuda.get_device_properties(0).total_memory//1024//1024} MiB' if d else 'CUDA not available')"
	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	out, err := cmd.Output()
	if err != nil {
		return false, "", err
	}
	info := strings.TrimSpace(string(out))
	available := info != "" && !strings.Contains(info, "CUDA not available")
	return available, info, nil
}

// detectGPUType ejecuta detect_gpu.sh directamente.
// Devuelve "cuda", "rocm", o "cpu".
func detectGPUType() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "/usr/local/bin/detect_gpu.sh")
	out, err := cmd.Output()
	if err != nil {
		return "cpu" // fallback safe
	}
	return strings.TrimSpace(string(out))
}

// checkDisk returns disk health for the project output directory.
// ok=true if > 10 GB free; otherwise ok=false with code "E5".
func checkDisk() map[string]interface{} {
	projectRoot := findProjectRoot()
	outputDir := filepath.Join(projectRoot, "output")

	// Ensure the directory exists for Statfs; create if missing.
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		os.MkdirAll(outputDir, 0o755)
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(outputDir, &stat); err != nil {
		return map[string]interface{}{
			"ok":     false,
			"code":   "E5",
			"detail": fmt.Sprintf("cannot stat output dir: %v", err),
		}
	}

	freeBytes := stat.Bavail * uint64(stat.Bsize)
	freeGB := float64(freeBytes) / (1024 * 1024 * 1024)

	if freeGB < 10 {
		return map[string]interface{}{
			"ok":     false,
			"code":   "E5",
			"detail": fmt.Sprintf("only %.1f GB free on /output", freeGB),
		}
	}

	return map[string]interface{}{
		"ok":     true,
		"detail": fmt.Sprintf("%.1f GB free", freeGB),
	}
}
