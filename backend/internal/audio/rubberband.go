package audio

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// RubberbandPitch aplica pitch shift a un archivo de audio usando rubberband CLI.
// semitones: semitonos (-12 a +12)
// input: ruta del HOST al archivo de entrada
// output: ruta del HOST al archivo de salida
func RubberbandPitch(semitones int, input, output string) error {
	if semitones == 0 {
		// Si pitch=0, copiar el archivo en vez de procesar
		return CopyFile(input, output)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "exec", "onda", "rubberband",
		"-p", fmt.Sprintf("%d", semitones),
		input, output)

	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rubberband failed: %w\nOutput: %s", err, string(outputBytes))
	}
	return nil
}

// Deprecated: only used in tests
// IsRubberbandInstalled verifica si rubberband está disponible dentro del contenedor onda
func IsRubberbandInstalled() bool {
	cmd := exec.Command("docker", "exec", "onda", "which", "rubberband")
	err := cmd.Run()
	return err == nil
}
