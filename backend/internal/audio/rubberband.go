package audio

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// RubberbandPitch aplica pitch shift a un archivo de audio usando ffmpeg con el filtro rubberband.
// semitones: semitonos (-12 a +12)
// input: ruta DENTRO del contenedor al archivo de entrada
// output: ruta DENTRO del contenedor al archivo de salida
// ffmpeg se ejecuta dentro del contenedor onda via docker exec.
func RubberbandPitch(semitones int, input, output string) error {
	if semitones == 0 {
		// Si pitch=0, copiar el archivo en vez de procesar
		return CopyFile(input, output)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Usar ffmpeg con el filtro rubberband (más fiable que rubberband-cli)
	cmd := exec.CommandContext(ctx, "docker", "exec", "onda", "ffmpeg",
		"-y",
		"-i", input,
		"-af", fmt.Sprintf("rubberband=pitch=%d", semitones),
		output)

	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg rubberband failed: %w\nOutput: %s", err, string(outputBytes))
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
