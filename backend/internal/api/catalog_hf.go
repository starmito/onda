package api

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"
)

var (
	hfCatalogData []byte
	hfCatalogOnce sync.Once
	hfCatalogErr  error
)

func loadHFCatalog() ([]byte, error) {
	hfCatalogOnce.Do(func() {
		// Try container path first, then project root
		for _, p := range []string{"/app/hf_models.json", "hf_models.json"} {
			data, err := os.ReadFile(p)
			if err == nil {
				hfCatalogData = data
				return
			}
		}
		hfCatalogErr = os.ErrNotExist
	})
	return hfCatalogData, hfCatalogErr
}

func (s *Server) handleModelsCatalogHF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	data, err := loadHFCatalog()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "HF catalog not available"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
