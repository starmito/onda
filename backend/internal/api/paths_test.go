package api

import (
	"strings"
	"testing"
)

func TestDataRoot_FromEnv(t *testing.T) {
	t.Setenv("ONDA_DATA_DIR", "/raiz-a")
	got := dataRoot()
	if got != "/raiz-a" {
		t.Fatalf("dataRoot() = %q, want %q", got, "/raiz-a")
	}
}

func TestDataRoot_DefaultMatchesCurrentBehaviour(t *testing.T) {
	t.Setenv("ONDA_DATA_DIR", "")
	got := dataRoot()
	want := findProjectRoot()
	if got != want {
		t.Fatalf("dataRoot() = %q, want findProjectRoot() = %q", got, want)
	}
}

func TestSub_JoinsUnderRoot(t *testing.T) {
	t.Setenv("ONDA_DATA_DIR", "/raiz")
	got, err := sub("output")
	if err != nil {
		t.Fatalf("sub(\"output\") returned error: %v", err)
	}
	want := "/raiz/output"
	if got != want {
		t.Fatalf("sub(\"output\") = %q, want %q", got, want)
	}
}

func TestSub_RejectsTraversal(t *testing.T) {
	t.Setenv("ONDA_DATA_DIR", "/raiz")
	cases := []string{
		"..",
		"../etc",
		"a/b",
		"",
		"foo\\bar",
	}
	for _, name := range cases {
		got, err := sub(name)
		if err == nil {
			t.Errorf("sub(%q) = %q; expected error", name, got)
			continue
		}
		if !strings.Contains(err.Error(), "invalid subpath") {
			t.Errorf("sub(%q) error = %v; want error containing 'invalid subpath'", name, err)
		}
	}
}
