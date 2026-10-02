//go:build linux

package transcode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type generatedWindowEvidenceRepository struct {
	managerTestRepository
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	fail    bool
}

func (repository *generatedWindowEvidenceRepository) Update(ctx context.Context, record Record) error {
	if record.State == "completed" {
		repository.once.Do(func() { close(repository.entered) })
		select {
		case <-repository.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		if repository.fail {
			return errors.New("controlled terminal persistence failure")
		}
	}
	return repository.managerTestRepository.Update(ctx, record)
}

// These tests isolate the terminal evidence barrier. Opaque test artifacts
// qualify for cache accounting only; independent media proof remains required
// before the private publisher can expose any source-range slot.
func generatedWindowEvidenceRunner(evidence *[MaxHLSRenditions]GeneratedInputEvidence) runnerFunc {
	return func(_ context.Context, _, directory string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		if err := os.WriteFile(filepath.Join(directory, "segment-000300.ts"), []byte("accounted test artifact"), 0o600); err != nil {
			return RunResult{}, err
		}
		playlist := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:3\n#EXT-X-PLAYLIST-TYPE:EVENT\n#EXT-X-MEDIA-SEQUENCE:300\n#EXT-X-DISCONTINUITY\n#EXTINF:3.0000000,\nsegment-000300.ts\n#EXT-X-ENDLIST\n"
		if err := os.WriteFile(filepath.Join(directory, "main.m3u8"), []byte(playlist), 0o600); err != nil {
			return RunResult{}, err
		}
		return RunResult{ExitCode: 0, WindowInputEvidence: evidence}, nil
	}
}

func TestGeneratedWindowManagerEvidenceWaitsForTerminalPersistence(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "failed"}[fail], func(t *testing.T) {
			fixture := generatedClosureTestFixture(false, false)
			repository := &generatedWindowEvidenceRepository{entered: make(chan struct{}), release: make(chan struct{}), fail: fail}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(repository.release) }) }
			defer release()
			options := managerTestOptions(t, generatedWindowEvidenceRunner(&fixture.input))
			options.Repository = repository
			manager := newTestManager(t, options)
			// Cleanup releases the controlled terminal write before manager
			// shutdown joins the terminal attempt, including test failures.
			t.Cleanup(release)
			spec := managerTestSpec(700)
			spec.Plan = fixture.plan
			record, err := manager.Ensure(context.Background(), spec, managerTestInput(t))
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-repository.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("job did not reach the controlled terminal persistence attempt")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			_, err = manager.GeneratedWindowInputEvidence(ctx, spec.Scope, record.ID)
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("evidence escaped before terminal persistence retired: %v", err)
			}
			release()
			terminal := managerTestWaitFinished(t, manager, record.ID)
			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			observed, err := manager.GeneratedWindowInputEvidence(ctx, spec.Scope, record.ID)
			if fail {
				if !errors.Is(err, ErrPersistence) || terminal.State != "failed" {
					t.Fatalf("failed terminal persistence published evidence: %+v, %v", terminal, err)
				}
				return
			}
			if err != nil || observed != fixture.input || terminal.State != "completed" || terminal.OutputBytes <= 0 {
				t.Fatalf("committed fully accounted evidence: %+v, %+v, %v", terminal, observed, err)
			}
			foreign := spec.Scope
			foreign.DeviceID = "foreign-device"
			if _, err := manager.GeneratedWindowInputEvidence(ctx, foreign, record.ID); !errors.Is(err, ErrJobNotFound) {
				t.Fatalf("foreign scope retrieved another window's evidence: %v", err)
			}
		})
	}
}

func TestGeneratedWindowManagerRejectsNormalOutputWithoutInputAssociation(t *testing.T) {
	fixture := generatedClosureTestFixture(false, false)
	manager := newTestManager(t, managerTestOptions(t, generatedWindowEvidenceRunner(nil)))
	spec := managerTestSpec(701)
	spec.Plan = fixture.plan
	record, err := manager.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	terminal := managerTestWaitFinished(t, manager, record.ID)
	if terminal.State != "failed" || terminal.ErrorCode != "invalid_output" {
		t.Fatalf("normal output without complete input association was retained as successful: %+v", terminal)
	}
	if _, err := manager.GeneratedWindowInputEvidence(context.Background(), spec.Scope, record.ID); !errors.Is(err, ErrJobFailed) {
		t.Fatalf("failed input association remained accessible: %v", err)
	}
}
