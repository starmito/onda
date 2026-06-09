package audio

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SupportedExtensions devuelve las extensiones de audio soportadas
func SupportedExtensions() []string {
	return []string{".wav", ".mp3", ".flac", ".ogg", ".m4a", ".aiff", ".wma"}
}

// IsAudioFile comprueba si un archivo tiene extensión de audio soportada
func IsAudioFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, supported := range SupportedExtensions() {
		if ext == supported {
			return true
		}
	}
	return false
}

// CopyFile copia un archivo de src a dst sobrescribiendo el destino si existe
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("cannot open source file: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("cannot create destination file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return fmt.Errorf("cannot copy file data: %w", err)
	}

	if err := out.Sync(); err != nil {
		return err
	}
	return out.Close()
}
