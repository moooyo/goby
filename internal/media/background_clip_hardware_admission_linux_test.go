package media

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/commanddomain"
)

func TestBackgroundClipHardwareCheckRunsAfterProcessCapacity(t *testing.T) {
	for _, native := range []bool{false, true} {
		name := "conventional"
		if native {
			name = "native"
		}
		t.Run(name, func(t *testing.T) {
			governor := newMediaProcessAdmission(1, 1, 4)
			ctx, cancel := context.WithTimeout(WithBackgroundProcess(context.Background()), 10*time.Second)
			defer cancel()
			release := mediaProcessAdmissionTestAcquire(t, governor, ctx)
			var releaseOnce sync.Once
			defer releaseOnce.Do(release)
			var available atomic.Bool
			available.Store(true)
			var checks atomic.Int32
			rejected := errors.New("captured device was replaced")
			ctx = backgroundClipHardwareContext(ctx, true, func(work context.Context) error {
				checks.Add(1)
				if err := work.Err(); err != nil {
					return err
				}
				if !available.Load() {
					return rejected
				}
				return nil
			})
			command := exec.CommandContext(ctx, "/must-never-spawn")
			var unknown atomic.Uint64
			done := make(chan error, 1)
			go func() {
				var process *mediaProcess
				var err error
				if native {
					process, err = startNativeMediaProcessWithCounter(ctx, command, governor, &commanddomain.CommandScope{}, &unknown)
				} else {
					process, err = startMediaProcessWithAdmission(ctx, command, governor, false)
				}
				if process != nil {
					err = errors.Join(err, errors.New("rejected hardware created a process owner"), process.Close())
				}
				done <- err
			}()
			defer func() {
				cancel()
				releaseOnce.Do(release)
			}()
			mediaProcessAdmissionTestWait(t, governor, 1, 1, 1)
			if checks.Load() != 0 {
				t.Fatal("hardware was checked before the process capacity wait")
			}
			available.Store(false)
			releaseOnce.Do(release)
			select {
			case err := <-done:
				if !errors.Is(err, rejected) || command.Process != nil || checks.Load() != 1 || unknown.Load() != 0 {
					t.Fatalf("hardware rejection did not prevent process start: checks=%d error=%v", checks.Load(), err)
				}
			case <-ctx.Done():
				t.Fatal("hardware rejection did not retire its pending process admission")
			}
			mediaProcessAdmissionTestWait(t, governor, 0, 0, 0)
		})
	}
}

func TestBackgroundClipHardwareCheckRunsAfterSourceAndCapacityWaits(t *testing.T) {
	mediaProcessAdmissionTestWait(t, mediaProcessAdmission, 0, 0, 0)
	ctx, cancel := context.WithTimeout(WithBackgroundProcess(context.Background()), 10*time.Second)
	defer cancel()
	first := mediaProcessAdmissionTestAcquire(t, mediaProcessAdmission, ctx)
	second := mediaProcessAdmissionTestAcquire(t, mediaProcessAdmission, ctx)
	var firstOnce, secondOnce sync.Once
	defer firstOnce.Do(first)
	defer secondOnce.Do(second)
	input, err := os.CreateTemp(t.TempDir(), "hardware-source-")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	entered, sourceReady := make(chan struct{}), make(chan struct{})
	var sourceOnce sync.Once
	defer sourceOnce.Do(func() { close(sourceReady) })
	var checks atomic.Int32
	var available, parsed atomic.Bool
	available.Store(true)
	rejected := errors.New("device removed during reader admission")
	ctx = WithSourceReadPhase(ctx, func(work context.Context, run func(context.Context) error) error {
		close(entered)
		select {
		case <-sourceReady:
			return run(work)
		case <-work.Done():
			return work.Err()
		}
	})
	ctx = backgroundClipHardwareContext(ctx, true, func(work context.Context) error {
		checks.Add(1)
		if err := work.Err(); err != nil {
			return err
		}
		if !available.Load() {
			return rejected
		}
		return nil
	})
	done := make(chan error, 1)
	sink := &analysisDiscardStderr{}
	go func() {
		done <- runBackgroundClipProcess(ctx, "/must-never-spawn", input, nil, 5*time.Second, 1024, sink,
			func(io.Reader) error { parsed.Store(true); return nil }, true)
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("GPU clip did not reach source-read admission")
	}
	if checks.Load() != 0 {
		t.Fatal("hardware was checked before the source-read wait")
	}
	sourceOnce.Do(func() { close(sourceReady) })
	mediaProcessAdmissionTestWait(t, mediaProcessAdmission, 2, 2, 1)
	if checks.Load() != 0 {
		t.Fatal("hardware was checked before process capacity became available")
	}
	available.Store(false)
	firstOnce.Do(first)
	select {
	case err := <-done:
		if !errors.Is(err, rejected) || checks.Load() != 1 || parsed.Load() || !errors.Is(sink.failure(), rejected) {
			t.Fatalf("GPU clip crossed rejected hardware admission: checks=%d parsed=%t error=%v", checks.Load(), parsed.Load(), err)
		}
	case <-ctx.Done():
		t.Fatal("rejected GPU clip did not return")
	}
	secondOnce.Do(second)
	mediaProcessAdmissionTestWait(t, mediaProcessAdmission, 0, 0, 0)
}

func TestBackgroundClipSoftwareProcessIgnoresHardwareCallback(t *testing.T) {
	ctx := backgroundClipHardwareContext(context.Background(), false, func(context.Context) error {
		t.Error("software clip called a GPU admission check")
		return errors.New("unexpected hardware check")
	})
	tool := analysisProcessTestTool(t, "printf software")
	sink := &analysisDiscardStderr{}
	var output []byte
	err := runBackgroundClipProcess(ctx, tool, nil, nil, time.Second, 1024, sink, func(reader io.Reader) error {
		var err error
		output, err = io.ReadAll(reader)
		return err
	}, false)
	if err != nil || string(output) != "software" {
		t.Fatalf("software clip changed its nil hardware-check behavior: output=%q error=%v", output, err)
	}
}

func TestBackgroundClipHardwareRejectionClosesOwnedPipeEnds(t *testing.T) {
	mediaProcessAdmissionTestWait(t, mediaProcessAdmission, 0, 0, 0)
	input, err := os.CreateTemp(t.TempDir(), "rejected-hardware-source-")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	executable, err := os.Open("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	defer executable.Close()
	// Finalizers must not conceal descriptors retained by an unstarted Cmd.
	previousGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previousGC)
	countFiles := func() int {
		t.Helper()
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}
	before := countFiles()
	rejected := errors.New("captured hardware is unavailable")
	checks := 0
	ctx := backgroundClipHardwareContext(context.Background(), true, func(context.Context) error {
		checks++
		return rejected
	})
	for range 32 {
		sink := &analysisDiscardStderr{}
		err := runBackgroundClipProcess(ctx, "/must-never-spawn", input, nil, time.Second, 1024, sink,
			func(io.Reader) error { t.Error("rejected clip ran its parser"); return nil }, true, executable)
		if !errors.Is(err, rejected) || !errors.Is(sink.failure(), rejected) {
			t.Fatalf("hardware rejection lost its original failure: %v", err)
		}
	}
	if after := countFiles(); after != before || checks != 32 {
		t.Fatalf("unstarted clips retained owned pipes: before=%d after=%d checks=%d", before, after, checks)
	}
	for _, file := range []*os.File{input, executable} {
		if _, err := file.Stat(); err != nil {
			t.Fatal("rejected clip closed a borrowed source or executable", err)
		}
	}
	mediaProcessAdmissionTestWait(t, mediaProcessAdmission, 0, 0, 0)
}
