package api

import (
	"os"
	"path/filepath"
)

func readProjectFile(name string) ([]byte, error) {
	p := filepath.Join(dataRoot(), name)
	data, err := os.ReadFile(p)
	if err == nil {
		return data, nil
	}
	return nil, err
}
