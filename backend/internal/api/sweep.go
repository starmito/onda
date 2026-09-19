package api

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// pipelineSweepPatterns are command-line fragments that identify orphan
// pipeline processes. They are intentionally specific so the backend never
// matches unrelated python/bash processes.
var pipelineSweepPatterns = []string{
	"pipeline.sh",
	"inference_mdx.py",
	"inference_scnet.py",
	"inference_universal.py",
	"inference_onnx.py",
}

// sweepOrphanPipelineProcesses is invoked by cancelCurrentJob after the
// process group has been terminated. By default it is inert so the test suite
// cannot kill real processes. main.go installs the production implementation
// with EnablePipelineProcessSweep. Tests may also replace this variable to
// verify that the sweep is called.
var sweepOrphanPipelineProcesses = func() {}

// realSweepFunc holds the production sweep implementation. It is kept in a
// separate variable so tests can verify that EnablePipelineProcessSweep
// installs the expected function without invoking it.
var realSweepFunc = func() {
	ownCgroup := readOwnCgroup()
	if ownCgroup == "" {
		Log("backend", "warn", "Cannot determine own cgroup; skipping orphan pipeline sweep")
		return
	}
	sweepPipelineProcessesInCgroup(ownCgroup, pipelineSweepPatterns)
}

// EnablePipelineProcessSweep installs the production sweep implementation.
// It is called from main.go so the backend kills only orphan pipeline
// processes that share the backend's cgroup.
func EnablePipelineProcessSweep() {
	sweepOrphanPipelineProcesses = realSweepFunc
}

// readOwnCgroup returns the cgroup path of the current process.
func readOwnCgroup() string {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return ""
	}
	return parseProcCgroup(string(data))
}

// readProcCgroupAt returns the cgroup path of pid under procDir.
func readProcCgroupAt(procDir string, pid int) string {
	data, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return ""
	}
	return parseProcCgroup(string(data))
}

// readProcCmdlineAt returns the raw command line of pid under procDir.
func readProcCmdlineAt(procDir string, pid int) string {
	data, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return ""
	}
	return string(data)
}

// parseProcCgroup extracts the cgroup path from /proc/<pid>/cgroup contents.
// It supports cgroup v2 (single line) and cgroup v1 (multiple controllers) by
// returning the first non-empty path.
func parseProcCgroup(data string) string {
	for _, line := range strings.Split(data, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 && parts[2] != "" {
			return parts[2]
		}
	}
	return ""
}

// cmdlineMatchesAny reports whether cmdline contains any of the patterns.
func cmdlineMatchesAny(cmdline string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(cmdline, p) {
			return true
		}
	}
	return false
}

// cgroupMatches reports whether pidCgroup is the same cgroup as ownCgroup or
// a descendant of it. A descendant is accepted because container runtimes may
// place child processes in sub-cgroups while they still belong to this backend
// instance.
func cgroupMatches(ownCgroup, pidCgroup string) bool {
	if ownCgroup == "" || pidCgroup == "" {
		return false
	}
	if pidCgroup == ownCgroup {
		return true
	}
	return strings.HasPrefix(pidCgroup, ownCgroup+"/")
}

// shouldSweepProcess is the pure decision function: given the backend's
// cgroup, a candidate's cgroup and command line, decide whether it is an
// orphan pipeline process that must be killed.
func shouldSweepProcess(ownCgroup, pidCgroup, cmdline string, patterns []string) bool {
	if !cgroupMatches(ownCgroup, pidCgroup) {
		return false
	}
	return cmdlineMatchesAny(cmdline, patterns)
}

// listProcPIDs returns numeric entries in procDir that represent processes.
func listProcPIDs(procDir string) []int {
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return nil
	}
	pids := make([]int, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// killProcessForSweep sends SIGKILL to pid, ignoring ESRCH.
func killProcessForSweep(pid int) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// sweepPipelineProcessesInCgroup enumerates /proc and kills processes whose
// command line matches a pipeline pattern and whose cgroup is the same as (or
// a descendant of) the backend's cgroup.
func sweepPipelineProcessesInCgroup(ownCgroup string, patterns []string) {
	sweepPipelineProcessesInCgroupWithDeps("/proc", ownCgroup, patterns, killProcessForSweep)
}

// sweepPipelineProcessesInCgroupWithDeps is the testable version of the sweep:
// procDir, ownCgroup and the kill function are injected.
func sweepPipelineProcessesInCgroupWithDeps(procDir, ownCgroup string, patterns []string, killFn func(int) error) {
	for _, pid := range listProcPIDs(procDir) {
		pidCgroup := readProcCgroupAt(procDir, pid)
		cmdline := readProcCmdlineAt(procDir, pid)
		if shouldSweepProcess(ownCgroup, pidCgroup, cmdline, patterns) {
			_ = killFn(pid)
		}
	}
}
