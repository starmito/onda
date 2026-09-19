package api

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

func TestParseProcCgroup(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"0::/docker/abc123", "/docker/abc123"},
		{"12:freezer:/docker/abc123\n11:cpu,cpuacct:/docker/abc123", "/docker/abc123"},
		{"0::/\n", "/"},
		{"", ""},
		{"\n", ""},
		{"0::", ""},
	}
	for _, c := range cases {
		got := parseProcCgroup(c.input)
		if got != c.want {
			t.Errorf("parseProcCgroup(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestCgroupMatches(t *testing.T) {
	cases := []struct {
		own  string
		pid  string
		want bool
	}{
		{"/docker/abc", "/docker/abc", true},
		{"/docker/abc", "/docker/abc/sub", true},
		{"/docker/abc", "/docker/abc/sub/deeper", true},
		{"/docker/abc", "/docker/abc2", false},
		{"/docker/abc", "/docker/abcd", false},
		{"/docker/abc", "/other", false},
		{"", "/docker/abc", false},
		{"/docker/abc", "", false},
	}
	for _, c := range cases {
		got := cgroupMatches(c.own, c.pid)
		if got != c.want {
			t.Errorf("cgroupMatches(%q, %q) = %v, want %v", c.own, c.pid, got, c.want)
		}
	}
}

func TestCmdlineMatchesAny(t *testing.T) {
	patterns := []string{"pipeline.sh", "inference_universal.py"}
	cases := []struct {
		cmdline string
		want    bool
	}{
		{"bash\x00/app/pipeline.sh\x00--viperx", true},
		{"python\x00inference_universal.py\x00--model foo", true},
		{"python\x00inference_other.py", false},
		{"", false},
	}
	for _, c := range cases {
		got := cmdlineMatchesAny(c.cmdline, patterns)
		if got != c.want {
			t.Errorf("cmdlineMatchesAny(%q) = %v, want %v", c.cmdline, got, c.want)
		}
	}
}

func TestShouldSweepProcess(t *testing.T) {
	own := "/docker/abc"
	patterns := pipelineSweepPatterns

	cases := []struct {
		name    string
		cgroup  string
		cmdline string
		want    bool
	}{
		{"same cgroup matches pipeline.sh", own, "bash\x00/app/pipeline.sh\x00--viperx", true},
		{"sub cgroup matches inference", own + "/sub", "python\x00inference_universal.py", true},
		{"other cgroup does not match", "/docker/other", "bash\x00/app/pipeline.sh", false},
		{"same cgroup but unrelated cmd", own, "python\x00unrelated.py", false},
		{"empty cgroup rejected", "", "bash\x00/app/pipeline.sh", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shouldSweepProcess(own, c.cgroup, c.cmdline, patterns)
			if got != c.want {
				t.Errorf("shouldSweepProcess(...) = %v, want %v", got, c.want)
			}
		})
	}
}

func writeFakeProc(t *testing.T, procDir string, pid int, cgroup, cmdline string) {
	t.Helper()
	pidDir := filepath.Join(procDir, strconv.Itoa(pid))
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("failed to create fake proc dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "cgroup"), []byte("0::"+cgroup+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write cgroup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "cmdline"), []byte(cmdline), 0o644); err != nil {
		t.Fatalf("failed to write cmdline: %v", err)
	}
}

func TestSweepPipelineProcessesInCgroupWithDeps(t *testing.T) {
	root := setTestRoot(t, "sweep-test-")
	procDir := filepath.Join(root, "proc")

	// PID 100: same cgroup, matches pipeline.sh.
	writeFakeProc(t, procDir, 100, "/docker/abc", "bash\x00/app/pipeline.sh\x00--viperx")
	// PID 200: same cgroup, does not match any pattern.
	writeFakeProc(t, procDir, 200, "/docker/abc", "python\x00unrelated.py")
	// PID 300: other cgroup, matches pipeline.sh but cgroup differs.
	writeFakeProc(t, procDir, 300, "/docker/other", "bash\x00/app/pipeline.sh")
	// PID 400: sub-cgroup, matches inference pattern.
	writeFakeProc(t, procDir, 400, "/docker/abc/sub", "python\x00inference_universal.py")

	var killed []int
	killFn := func(pid int) error {
		killed = append(killed, pid)
		return nil
	}

	sweepPipelineProcessesInCgroupWithDeps(procDir, "/docker/abc", pipelineSweepPatterns, killFn)

	slices.Sort(killed)
	want := []int{100, 400}
	if !slices.Equal(killed, want) {
		t.Errorf("killed PIDs = %v, want %v", killed, want)
	}
}

func TestListProcPIDs(t *testing.T) {
	root := setTestRoot(t, "list-proc-")
	procDir := filepath.Join(root, "proc")
	for _, pid := range []int{1, 42, 1000} {
		if err := os.MkdirAll(filepath.Join(procDir, strconv.Itoa(pid)), 0o755); err != nil {
			t.Fatalf("failed to create pid dir: %v", err)
		}
	}
	// Non-numeric entries must be ignored.
	if err := os.MkdirAll(filepath.Join(procDir, "self"), 0o755); err != nil {
		t.Fatalf("failed to create self dir: %v", err)
	}

	got := listProcPIDs(procDir)
	slices.Sort(got)
	want := []int{1, 42, 1000}
	if !slices.Equal(got, want) {
		t.Errorf("listProcPIDs(%q) = %v, want %v", procDir, got, want)
	}
}

func TestSweepOrphanPipelineProcesses_InertByDefault(t *testing.T) {
	// The default implementation must be inert. Calling it must not enumerate
	// /proc or kill any process.
	sweepOrphanPipelineProcesses()
}

func TestEnablePipelineProcessSweep_InstallsRealSweep(t *testing.T) {
	orig := sweepOrphanPipelineProcesses
	defer func() { sweepOrphanPipelineProcesses = orig }()

	called := false
	sweepOrphanPipelineProcesses = func() { called = true }

	EnablePipelineProcessSweep()

	// The stub must have been replaced by the production implementation.
	sweepOrphanPipelineProcesses()
	if called {
		t.Error("EnablePipelineProcessSweep did not replace the stub with the real implementation")
	}
}
