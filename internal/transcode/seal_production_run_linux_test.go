//go:build linux

package transcode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProductionSealProcessExitRequiresOwnedCancellation(t *testing.T) {
	for _, test := range []struct {
		name, program string
		owned, want   bool
	}{
		{"normal", "exit 0", false, true},
		{"foreign_signal_exit", "exit 255", false, false},
		{"owned_ffmpeg_signal_exit", "exit 255", true, true},
		{"ordinary_failure", "exit 1", true, false},
		{"foreign_kill", "kill -KILL $$", false, false},
		{"owned_kill", "kill -KILL $$", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := exec.Command("/bin/sh", "-c", test.program).Run()
			if got := productionSealProcessExitSafe(err, test.owned); got != test.want {
				t.Fatalf("exit proof = %v, want %v: %v", got, test.want, err)
			}
		})
	}
	validExit := exec.Command("/bin/sh", "-c", "exit 255").Run()
	for _, err := range []error{exec.ErrWaitDelay, errors.Join(validExit, exec.ErrWaitDelay), errors.New("unknown wait failure")} {
		if productionSealProcessExitSafe(err, true) {
			t.Fatal("output-drain failure acquired retained-production proof")
		}
	}
}

const sealProgressProofHelperName = "goby-seal-progress-proof-helper"

func init() {
	if filepath.Base(os.Args[0]) != sealProgressProofHelperName {
		return
	}
	signal.Ignore(syscall.SIGTERM)
	fmt.Fprint(os.Stdout, "out_time_us=1000000\ntotal_size=2048\nprogress=continue\n")
	fmt.Fprint(os.Stdout, strings.Repeat("x", maxProgressLine+1))
	for {
		time.Sleep(time.Second)
	}
}

func TestRunProductionSealProofRejectsProgressFailureHiddenByCancellation(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(t.TempDir(), sealProgressProofHelperName)
	if err := os.Symlink(executable, helper); err != nil {
		t.Fatal(err)
	}
	plan := commandPlan()
	plan.SegmentMode, plan.EndTicks = "vod", plan.DurationTicks
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 1)
	type outcome struct {
		result RunResult
		err    error
	}
	done := make(chan outcome, 1)
	retired := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		select {
		case <-retired:
		case <-time.After(5 * time.Second):
			t.Error("observer helper remained active during cleanup")
		}
	})
	input, directory := helperInput(t), t.TempDir()
	go func() {
		defer close(retired)
		result, err := Run(ctx, helper, directory, input, plan, 1, func(Progress) {
			select {
			case entered <- struct{}{}:
			default:
			}
		})
		done <- outcome{result: result, err: err}
	}()
	select {
	case <-entered:
		cancel()
	case result := <-done:
		t.Fatalf("runner finished before its observer entered: %+v", result)
	case <-time.After(5 * time.Second):
		t.Fatal("observer did not enter its valid initial progress block")
	}
	select {
	case result := <-done:
		if !errors.Is(result.err, context.Canceled) || result.result.ProgressFailure == nil ||
			result.result.ProgressFailure.Reason != "line_too_long" || result.result.ProductionSealSafe {
			t.Fatalf("cancel priority hid observer failure from retained-output proof: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled observer helper was not retired")
	}
}
