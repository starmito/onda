package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func findFlagByName(flags []ModelFlagValue, name string) ModelFlagValue {
	for _, f := range flags {
		if f.Name == name {
			return f
		}
	}
	return ModelFlagValue{}
}

// TestGetModelFlagsResponse_RaisesRangeToMatchSavedValue covers the production
// path that uses knownFlags/fallbackFlagsForModel: a Demucs-like model whose
// generic shifts range is 1..10 but whose saved configuration uses shifts=20.
// Before the fix the response returned max=10 and could not represent value=20.
func TestGetModelFlagsResponse_RaisesRangeToMatchSavedValue(t *testing.T) {
	setTestRoot(t, "flags-range-raise-")

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      20,
		Segment:     7,
		Jobs:        8,
	}
	if err := writeModelConfigToYaml("MyCustomDemucs", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	resp, err := getModelFlagsResponse("MyCustomDemucs")
	if err != nil {
		t.Fatalf("getModelFlagsResponse failed: %v", err)
	}

	shifts := findFlagByName(resp.Flags, "shifts")
	if shifts.Name == "" {
		t.Fatal("shifts flag not found")
	}
	if toInt(shifts.Value) != 20 {
		t.Errorf("shifts value = %v, want 20", shifts.Value)
	}
	if toInt(shifts.Max) < 20 {
		t.Errorf("shifts max = %v, want >= 20", shifts.Max)
	}
	if toInt(shifts.Default) != 20 {
		t.Errorf("shifts default = %v, want 20", shifts.Default)
	}
}

// TestGetModelFlagsResponse_LowersRangeToMatchSavedValue checks that values
// below the generic minimum also widen the declared range (and update the
// default so it stays inside the new range).
func TestGetModelFlagsResponse_LowersRangeToMatchSavedValue(t *testing.T) {
	setTestRoot(t, "flags-range-lower-")

	cfg := ModelConfigResponse{
		SegmentSize: 256,
		Overlap:     0.25,
		BatchSize:   1,
		Shifts:      0,
		Segment:     7,
		Jobs:        8,
	}
	if err := writeModelConfigToYaml("MyCustomDemucsLow", cfg); err != nil {
		t.Fatalf("writeModelConfigToYaml failed: %v", err)
	}

	resp, err := getModelFlagsResponse("MyCustomDemucsLow")
	if err != nil {
		t.Fatalf("getModelFlagsResponse failed: %v", err)
	}

	shifts := findFlagByName(resp.Flags, "shifts")
	if shifts.Name == "" {
		t.Fatal("shifts flag not found")
	}
	if toInt(shifts.Value) != 0 {
		t.Errorf("shifts value = %v, want 0", shifts.Value)
	}
	if toInt(shifts.Min) != 0 {
		t.Errorf("shifts min = %v, want 0", shifts.Min)
	}
	if toInt(shifts.Default) != 0 {
		t.Errorf("shifts default = %v, want 0", shifts.Default)
	}
}

// TestSaveModelFlags_AcceptsValueAfterRangeExpansion checks that saving a flag
// value outside the generic range succeeds because the declared range is
// widened before validation. Before the fix, saving shifts=20 for a model whose
// generic range is 1..10 would be rejected by validateFlagValue.
func TestSaveModelFlags_AcceptsValueAfterRangeExpansion(t *testing.T) {
	setTestRoot(t, "flags-save-expand-")

	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/models/{name}/config", s.handleModelsConfig)
	s.mux.HandleFunc("POST /api/models/{name}/config", s.handleModelsConfig)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)

	body := []byte(`{"flags":{"shifts":20}}`)
	postResp, err := http.Post(srv.URL+"/api/models/MyCustomDemucsSave/config", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusOK {
		t.Fatalf("POST status = %d, want 200", postResp.StatusCode)
	}

	getResp, err := http.Get(srv.URL + "/api/models/MyCustomDemucsSave/config")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}

	var got ModelFlagsResponse
	if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode GET response: %v", err)
	}
	shifts := findFlagByName(got.Flags, "shifts")
	if shifts.Name == "" {
		t.Fatal("shifts flag not found after save")
	}
	if toInt(shifts.Value) != 20 {
		t.Errorf("shifts value = %v, want 20", shifts.Value)
	}
	if toInt(shifts.Max) < 20 {
		t.Errorf("shifts max = %v, want >= 20", shifts.Max)
	}
}
