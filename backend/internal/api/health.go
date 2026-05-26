package api

import (
	"os/exec"
	"strings"
)

const dockerContainer = "onda"

// HealthResponse es la respuesta del endpoint /api/health.
type HealthResponse struct {
	Status    string `json:"status"`
	Container string `json:"container"`
	GPU       bool   `json:"gpu"`
	GPUInfo   string `json:"gpu_info,omitempty"`
	Version   string `json:"version"`
}

// checkDockerContainer verifica si el contenedor está corriendo.
func checkDockerContainer() (string, error) {
	cmd := exec.Command("docker", "inspect", "-f", "{{.State.Status}}", dockerContainer)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// checkGPU verifica si el contenedor tiene acceso a GPU NVIDIA.
func checkGPU() (bool, string, error) {
	cmd := exec.Command("docker", "exec", dockerContainer,
		"nvidia-smi", "--query-gpu=name,memory.used,memory.total", "--format=csv,noheader")
	out, err := cmd.Output()
	if err != nil {
		return false, "", err
	}
	info := strings.TrimSpace(string(out))
	available := info != "" && !strings.Contains(info, "NVIDIA-SMI has failed")
	return available, info, nil
}
