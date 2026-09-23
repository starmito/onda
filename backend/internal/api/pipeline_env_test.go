package api

import (
	"os"
	"strings"
	"testing"
)

// envMap builds a lookup map from a slice of KEY=VALUE strings. Keys that
// appear more than once keep the last value, matching the behaviour of exec.Cmd
// when it prepares the environment for the subprocess.
func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok {
			m[k] = v
		}
	}
	return m
}

func TestBuildPipelineEnv_ForwardsCacheVars(t *testing.T) {
	root := setTestRoot(t, "pipeline-env-")
	t.Setenv("ONDA_CONFIG_DIR", root+"/cfg")
	want := map[string]string{
		"HF_HOME":         root + "/.cache/huggingface",
		"TORCH_HOME":      root + "/.cache/torch",
		"NUMBA_CACHE_DIR": root + "/.cache/numba",
		"XDG_CACHE_HOME":  root + "/.cache/xdg",
	}
	for key, val := range want {
		t.Setenv(key, val)
	}

	extra := []string{"ONDA_EXTRA=value"}
	env := envMap(buildPipelineEnv(extra))

	if env["PYTHONUNBUFFERED"] != "1" {
		t.Errorf("PYTHONUNBUFFERED = %q, want 1", env["PYTHONUNBUFFERED"])
	}
	if env["ONDA_CONFIG_DIR"] != root+"/cfg" {
		t.Errorf("ONDA_CONFIG_DIR = %q, want %q", env["ONDA_CONFIG_DIR"], root+"/cfg")
	}
	for key, val := range want {
		if env[key] != val {
			t.Errorf("%s = %q, want %q", key, env[key], val)
		}
	}
	if env["ONDA_EXTRA"] != "value" {
		t.Errorf("ONDA_EXTRA = %q, want value", env["ONDA_EXTRA"])
	}
}

func TestBuildPipelineEnv_SkipsUnsetCacheVars(t *testing.T) {
	root := setTestRoot(t, "pipeline-env-unset-")
	t.Setenv("ONDA_CONFIG_DIR", root+"/cfg")
	for _, key := range []string{"HF_HOME", "TORCH_HOME", "NUMBA_CACHE_DIR", "XDG_CACHE_HOME"} {
		prev, had := os.LookupEnv(key)
		os.Unsetenv(key)
		t.Cleanup(func() {
			if had {
				os.Setenv(key, prev)
			} else {
				os.Unsetenv(key)
			}
		})
	}

	env := envMap(buildPipelineEnv(nil))

	if env["PYTHONUNBUFFERED"] != "1" {
		t.Errorf("PYTHONUNBUFFERED = %q, want 1", env["PYTHONUNBUFFERED"])
	}
	if env["ONDA_CONFIG_DIR"] != root+"/cfg" {
		t.Errorf("ONDA_CONFIG_DIR = %q, want %q", env["ONDA_CONFIG_DIR"], root+"/cfg")
	}
	for _, key := range []string{"HF_HOME", "TORCH_HOME", "NUMBA_CACHE_DIR", "XDG_CACHE_HOME"} {
		if _, ok := env[key]; ok {
			t.Errorf("%s should not be present when unset", key)
		}
	}
}

func TestBuildPipelineEnv_ExtraOverrides(t *testing.T) {
	root := setTestRoot(t, "pipeline-env-override-")
	t.Setenv("ONDA_CONFIG_DIR", root+"/cfg")
	t.Setenv("HF_HOME", root+"/.cache/huggingface")

	extra := []string{"HF_HOME=/override/hf"}
	env := envMap(buildPipelineEnv(extra))

	// The extra value is appended last, so it wins in exec.Cmd's env preparation.
	if env["HF_HOME"] != "/override/hf" {
		t.Errorf("HF_HOME = %q, want /override/hf", env["HF_HOME"])
	}
}

// TestBuildPipelineEnv_IncludesProcessEnvironment ensures the base environment
// is preserved, because the pipeline still needs PATH and other variables.
func TestBuildPipelineEnv_IncludesProcessEnvironment(t *testing.T) {
	root := setTestRoot(t, "pipeline-env-base-")
	t.Setenv("ONDA_CONFIG_DIR", root+"/cfg")
	// PATH is always present in the process environment.
	if path := os.Getenv("PATH"); path == "" {
		t.Skip("PATH not set in process environment")
	}

	env := envMap(buildPipelineEnv(nil))
	if env["PATH"] == "" {
		t.Error("PATH not preserved from process environment")
	}
}
