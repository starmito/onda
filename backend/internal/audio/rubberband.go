package audio

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// RubberbandPitch aplica pitch shift a un archivo de audio usando rubberband-cli.
// semitones: semitonos (-12 a +12)
// input: ruta al archivo de entrada
// output: ruta al archivo de salida
// rubberband-cli se ejecuta directamente (mismo contenedor).
func RubberbandPitch(semitones int, input, output string) error {
	if semitones == 0 {
		// Si pitch=0, copiar el archivo en vez de procesar
		return CopyFile(input, output)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Usar rubberband-cli (calidad profesional, no ffmpeg)
	cmd := exec.CommandContext(ctx, "rubberband",
		"-p", fmt.Sprintf("%d", semitones),
		input, output)

	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rubberband pitch failed: %w\nOutput: %s", err, string(outputBytes))
	}
	return nil
}

// Deprecated: only used in tests
// IsRubberbandInstalled verifica si rubberband está disponible
func IsRubberbandInstalled() bool {
	cmd := exec.Command("which", "rubberband")
	err := cmd.Run()
	return err == nil
}
