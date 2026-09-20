//go:build linux

package transcode

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestManagerSourceRetirementJoinsOwnedProcessesAndPreservesOtherSources(t *testing.T) {
	started := make(chan byte, 3)
	stopping := make(chan byte, 3)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	options := managerTestOptions(t, func(ctx context.Context, _, _ string, input *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		var key [1]byte
		if _, err := input.ReadAt(key[:], 0); err != nil {
			return RunResult{}, err
		}
		started <- key[0]
		<-ctx.Done()
		stopping <- key[0]
		if key[0] == 'a' {
			<-release
		}
		return RunResult{}, ctx.Err()
	})
	options.MaxJobs, options.MaxUserJobs, options.MaxSessionJobs = 3, 3, 3
	manager := newTestManager(t, options)
	specs := []Spec{managerTestSpec(1), managerTestSpec(2), managerTestSpec(3)}
	specs[1].Scope.ItemID = "another-item"
	specs[2].Scope.SourceID = "another-source"
	var inputs []*os.File
	for index, spec := range specs {
		input := managerTestInput(t)
		if _, err := input.WriteAt([]byte{byte('a' + index)}, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.Ensure(context.Background(), spec, input); err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, input)
	}
	deadline := time.After(3 * time.Second)
	for range specs {
		select {
		case <-started:
		case <-deadline:
			t.Fatal("controlled producers did not start")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- manager.CancelSource(ctx, specs[0].Scope.ItemID, specs[0].Scope.SourceID) }()
	select {
	case key := <-stopping:
		if key != 'a' {
			t.Fatalf("retirement cancelled an unrelated producer: %q", key)
		}
	case <-ctx.Done():
		t.Fatal("source producer was not cancelled")
	}
	select {
	case <-done:
		t.Fatal("source retirement returned before process exit")
	default:
	}
	close(release)
	released = true
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("source retirement did not join process exit")
	}
	assertManagerInputClosed(t, inputs[0])
	for _, input := range inputs[1:] {
		if _, err := input.Stat(); err != nil {
			t.Fatal("source retirement closed unrelated input")
		}
	}
	select {
	case key := <-stopping:
		t.Fatalf("unrelated source was retired: %q", key)
	default:
	}
}
