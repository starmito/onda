package api

import "os"

func readProjectFile(name string) ([]byte, error) {
	paths := []string{"/app/" + name, resolveProjectRoot() + "/" + name}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			return data, nil
		}
	}
	return nil, os.ErrNotExist
}
