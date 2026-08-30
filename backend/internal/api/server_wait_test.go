package api

import (
	"context"
	"errors"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestWaitWithTimeout_Success verifies that waitWithTimeout returns nil when
// the underlying wait function finishes before the context is cancelled.
func TestWaitWithTimeout_Success(t *testing.T) {
	ctx := context.Background()
	wait := func() error { return nil }

	if err := waitWithTimeout(ctx, wait, 100*time.Millisecond); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// TestWaitWithTimeout_Error verifies that waitWithTimeout surfaces errors from
// the underlying wait function.
func TestWaitWithTimeout_Error(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("wait failed")
	wait := func() error { return wantErr }

	err := waitWithTimeout(ctx, wait, 100*time.Millisecond)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

// TestWaitWithTimeout_AlreadyCancelled verifies that if the context is already
// cancelled but wait() has already returned, the real wait error is returned
// instead of a timeout error.
func TestWaitWithTimeout_AlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	wantErr := errors.New("wait failed")
	wait := func() error { return wantErr }

	err := waitWithTimeout(ctx, wait, 100*time.Millisecond)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

// TestWaitWithTimeout_CancelledThenReturns verifies that when the context is
// cancelled but wait() returns within the grace period, the real error is
// returned.
func TestWaitWithTimeout_CancelledThenReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	wantErr := errors.New("wait failed")

	wait := func() error {
		<-ctx.Done()
		time.Sleep(10 * time.Millisecond)
		return wantErr
	}

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	err := waitWithTimeout(ctx, wait, 200*time.Millisecond)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

// TestWaitWithTimeout_Timeout verifies that waitWithTimeout returns a
// descriptive timeout error when wait() does not return after the context is
// cancelled. The test uses a very short timeout so it remains fast.
func TestWaitWithTimeout_Timeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// block is closed at the end of the test so the goroutine spawned by
	// waitWithTimeout can exit cleanly instead of leaking.
	block := make(chan struct{})
	defer close(block)

	wait := func() error {
		<-ctx.Done()
		<-block
		return nil
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := waitWithTimeout(ctx, wait, 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "cmd.Wait timeout") {
		t.Fatalf("expected timeout error containing 'cmd.Wait timeout', got %v", err)
	}
}

// TestWaitCmdResult_Success verifies the public waitCmdResult helper against a
// real subprocess that exits successfully.
func TestWaitCmdResult_Success(t *testing.T) {
	cmd := exec.Command("bash", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start command: %v", err)
	}
	if err := waitCmdResult(context.Background(), cmd); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// TestWaitCmdResult_Error verifies that waitCmdResult surfaces exit errors from
// a real subprocess.
func TestWaitCmdResult_Error(t *testing.T) {
	cmd := exec.Command("bash", "-c", "exit 42")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start command: %v", err)
	}
	err := waitCmdResult(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
		t.Fatalf("expected exit code 42, got %v", err)
	}
}

// TestCancelCurrentJob_SweepsOrphans verifies that cancelling a running job
// invokes the orphan-pipeline cleanup sweep.
func TestCancelCurrentJob_SweepsOrphans(t *testing.T) {
	s := &Server{
		mux:      http.NewServeMux(),
		jobQueue: make(chan JobRequest, 1),
		jobs:     map[string]*JobState{},
	}

	cancelled := false
	s.currentCancel = func() { cancelled = true }
	s.currentPID = 9999

	swept := false
	origSweep := sweepOrphanPipelineProcesses
	sweepOrphanPipelineProcesses = func() { swept = true }
	defer func() { sweepOrphanPipelineProcesses = origSweep }()

	s.jobsMu.Lock()
	s.cancelCurrentJob()
	s.jobsMu.Unlock()

	if !cancelled {
		t.Error("expected context cancel function to be called")
	}
	if !swept {
		t.Error("expected sweepOrphanPipelineProcesses to be called")
	}
}
