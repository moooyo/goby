//go:build linux

package transcode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

const resourceLifecycleHelperName = "goby-resource-lifecycle-test-helper"

func init() {
	if filepath.Base(os.Args[0]) != resourceLifecycleHelperName {
		return
	}
	if err := os.WriteFile("leader.pid", []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		os.Exit(91)
	}
	// The final incomplete line is consumed by progress.finish after Cmd.Wait,
	// allowing the test to hold a real output-finalization boundary open.
	fmt.Fprint(os.Stdout, "progress=end")
	os.Exit(0)
}

func resourceLifecycleExecutable(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), resourceLifecycleHelperName)
	if err := os.Symlink(executable, path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunLifecycleVerifiesLeaderWaitBeforeFinalOutputDrain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	directory := t.TempDir()
	retired, draining, released, drained := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(released) })
	defer release()
	lifecycle := &resourceLifecycle{
		retire: func(barrierCtx context.Context) error {
			if barrierCtx.Err() != nil {
				return barrierCtx.Err()
			}
			data, err := os.ReadFile(filepath.Join(directory, "leader.pid"))
			if err != nil {
				return err
			}
			pid, err := strconv.Atoi(string(data))
			if err != nil || pid <= 0 {
				return errors.New("helper did not identify its leader")
			}
			if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); !errors.Is(err, os.ErrNotExist) {
				return errors.New("leader was not reaped before the retirement barrier")
			}
			return nil
		},
		onStage: func(event resourceLifecycleEvent) {
			switch event.Stage {
			case resourceProcessRetired:
				close(retired)
			case resourceWritersDrained:
				close(drained)
			}
		},
	}
	ctx = withResourceLifecycle(ctx, lifecycle)
	executable, input := resourceLifecycleExecutable(t), helperInput(t)
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, executable, directory, input, commandPlan(), 1, func(progress Progress) {
			if !progress.Ended {
				return
			}
			close(draining)
			<-released
		})
		done <- err
	}()
	select {
	case <-draining:
	case err := <-done:
		t.Fatalf("runner returned before final progress drain: %v", err)
	case <-ctx.Done():
		t.Fatal("runner did not reach final progress drain")
	}
	select {
	case <-retired:
	default:
		t.Fatal("verified process capacity stayed coupled to final output drain")
	}
	select {
	case <-drained:
		t.Fatal("output writer was reported drained while its callback was blocked")
	default:
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("runner did not finish after the output callback was released")
	}
	snapshot := lifecycle.snapshot()
	if !snapshot.RetirementVerified || snapshot.ProcessRetired.IsZero() || snapshot.WritersDrained.IsZero() ||
		!snapshot.WorkspaceSealed.IsZero() || !snapshot.TerminalPersisted.IsZero() {
		t.Fatalf("runner claimed a manager-owned finalization boundary: %+v", snapshot)
	}
}

func TestRunLifecycleRetirementFailureFailsProcessAndDoesNotRelease(t *testing.T) {
	failure := errors.New("retirement proof failed")
	lifecycle := &resourceLifecycle{retire: func(context.Context) error { return failure }}
	ctx := withResourceLifecycle(context.Background(), lifecycle)
	_, err := Run(ctx, resourceLifecycleExecutable(t), t.TempDir(), helperInput(t), commandPlan(), 1, nil)
	if !errors.Is(err, ErrProcess) || !errors.Is(err, failure) {
		t.Fatalf("runner hid a failed retirement proof: %v", err)
	}
	snapshot := lifecycle.snapshot()
	if !snapshot.ProcessRetired.IsZero() || snapshot.RetirementVerified || snapshot.WritersDrained.IsZero() {
		t.Fatalf("failed retirement released execution ownership: %+v", snapshot)
	}
}

func TestRunLifecycleDefaultDoesNotReportEarlyRetirement(t *testing.T) {
	lifecycle := &resourceLifecycle{}
	ctx := withResourceLifecycle(context.Background(), lifecycle)
	_, err := Run(ctx, resourceLifecycleExecutable(t), t.TempDir(), helperInput(t), commandPlan(), 1, func(Progress) {
		if snapshot := lifecycle.snapshot(); !snapshot.ProcessRetired.IsZero() || !snapshot.WritersDrained.IsZero() {
			t.Errorf("soft storage mode acquired an early lifecycle release: %+v", snapshot)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := lifecycle.snapshot(); snapshot.ProcessRetired.IsZero() || snapshot.WritersDrained.IsZero() || snapshot.RetirementVerified {
		t.Fatalf("default completion observation was lost: %+v", snapshot)
	}
}
