package audio

import (
	"fmt"
	"os/exec"
)

// RubberbandPitch aplica pitch shift a un archivo de audio usando rubberband CLI.
// semitones: semitonos (-12 a +12)
// input: ruta al archivo de entrada
// output: ruta al archivo de salida
func RubberbandPitch(semitones int, input, output string) error {
	if semitones == 0 {
		// Si pitch=0, copiar el archivo en vez de procesar
		return copyFile(input, output)
	}
	cmd := exec.Command("docker", "exec", "onda", "rubberband", "-p", fmt.Sprintf("%d", semitones), input, output)
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
