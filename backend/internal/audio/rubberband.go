package audio

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RubberbandPitch aplica pitch shift a un archivo de audio usando rubberband CLI.
// semitones: semitonos (-12 a +12)
// input: ruta del HOST al archivo de entrada
// output: ruta del HOST al archivo de salida
func RubberbandPitch(semitones int, input, output string) error {
	if semitones == 0 {
		// Si pitch=0, copiar el archivo en vez de procesar
		return copyFile(input, output)
	}

	// Convertir paths del host a paths del contenedor
	// /home/starmito/projects/onda/output/... → /output/...
	// /home/starmito/projects/onda/input/...  → /input/...
	projectRoot := findProjectRoot()
	containerInput := strings.Replace(input, projectRoot, "", 1)
	containerOutput := strings.Replace(output, projectRoot, "", 1)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "exec", "onda", "rubberband",
		"-p", fmt.Sprintf("%d", semitones),
		containerInput, containerOutput)

	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rubberband failed: %w\nOutput: %s", err, string(outputBytes))
	}
	return nil
}

// IsRubberbandInstalled verifica si rubberband está disponible dentro del contenedor onda
func IsRubberbandInstalled() bool {
	cmd := exec.Command("docker", "exec", "onda", "which", "rubberband")
	err := cmd.Run()
	return err == nil
}

// findProjectRoot busca el directorio raíz del proyecto (donde está el archivo VERSION).
func findProjectRoot() string {
	if root := os.Getenv("ONDA_ROOT"); root != "" {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			return root
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "VERSION")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "."
}
