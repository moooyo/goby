//go:build linux

package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/transcode"
)

// This exercises ownership with actual temporary descriptors. The consumer is
// a controlled contract stub, not an encoder/AUTH/media acceptance fixture.
type playbackOwnedInputTestConsumer struct {
	run func(context.Context, transcode.Spec, *os.File) (transcode.Record, error)
}

func (consumer playbackOwnedInputTestConsumer) Ensure(ctx context.Context, spec transcode.Spec, file *os.File) (transcode.Record, error) {
	return consumer.run(ctx, spec, file)
}

func playbackOwnedInputTestFile(t *testing.T) *os.File {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "actual-owned-input"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func TestPlaybackOwnedInputTransferAndDuplicateRetainActualFD(t *testing.T) {
	var gate playbackStopIntentGate
	scope := playbackStopIntentTestScope("play-fd-transfer")
	grant, err := gate.acquireValidated(scope)
	if err != nil {
		t.Fatal(err)
	}
	staleCopy := *grant
	file := playbackOwnedInputTestFile(t)
	input, err := newPlaybackOwnedInput(file, grant)
	if err != nil {
		t.Fatal(err)
	}
	grant.release()
	staleCopy.release()
	if gate.usage().References != 1 {
		t.Fatal("the transferred descriptor depended on an old handle's cleanup")
	}
	if _, err := staleCopy.fork(scope); !errors.Is(err, transcode.ErrJobCancelled) {
		t.Fatal("a transferred old handle remained capable of admission")
	}
	duplicate, err := input.duplicate()
	if err != nil {
		t.Fatal(err)
	}
	first, firstErr := input.file.Stat()
	second, secondErr := duplicate.file.Stat()
	if firstErr != nil || secondErr != nil || !os.SameFile(first, second) || input.file.Fd() == duplicate.file.Fd() {
		t.Fatal("the independent loan was not an actual same-file descriptor duplicate")
	}
	stop, err := gate.acceptValidatedStop(scope)
	if err != nil {
		t.Fatal(err)
	}
	stop.finish(true)
	if err := input.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("the source owner was released before actual descriptor Close")
	}
	if _, err := duplicate.file.Stat(); err != nil || gate.usage().References != 1 || !gate.blocked(scope) {
		t.Fatal("parent Close erased the independent worker descriptor/intent lifetime")
	}
	if err := duplicate.close(); err != nil || gate.usage() != (playbackStopIntentUsage{}) {
		t.Fatal("the final actual duplicate Close failed to collect terminal ownership")
	}
}

func TestPlaybackOwnedInputStoppedLoanCannotReachConsumer(t *testing.T) {
	var gate playbackStopIntentGate
	scope := playbackStopIntentTestScope("play-fd-stopped")
	grant, err := gate.acquireValidated(scope)
	if err != nil {
		t.Fatal(err)
	}
	file := playbackOwnedInputTestFile(t)
	input, err := newPlaybackOwnedInput(file, grant)
	if err != nil {
		t.Fatal(err)
	}
	stop, err := gate.acceptValidatedStop(scope)
	if err != nil {
		t.Fatal(err)
	}
	stop.finish(true)
	called := false
	consumer := playbackOwnedInputTestConsumer{run: func(context.Context, transcode.Spec, *os.File) (transcode.Record, error) {
		called = true
		return transcode.Record{}, nil
	}}
	if _, err := input.ensure(context.Background(), consumer, transcode.Spec{Scope: scope}); !errors.Is(err, transcode.ErrJobCancelled) || called {
		t.Fatal("a stopped descriptor reached the consuming admission")
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) || gate.usage() != (playbackStopIntentUsage{}) {
		t.Fatal("rejected admission did not close its actual descriptor before collection")
	}
}

func TestPlaybackOwnedInputConsumerHoldsContextUntilReturn(t *testing.T) {
	var gate playbackStopIntentGate
	scope := playbackStopIntentTestScope("play-fd-consumer")
	grant, err := gate.acquireValidated(scope)
	if err != nil {
		t.Fatal(err)
	}
	file := playbackOwnedInputTestFile(t)
	input, err := newPlaybackOwnedInput(file, grant)
	if err != nil {
		t.Fatal(err)
	}
	entered, resume := make(chan struct{}), make(chan struct{})
	consumer := playbackOwnedInputTestConsumer{run: func(ctx context.Context, spec transcode.Spec, file *os.File) (transcode.Record, error) {
		_, release, err := gate.holdContext(ctx, spec.Scope)
		if err != nil {
			return transcode.Record{}, errors.Join(err, file.Close())
		}
		defer release()
		close(entered)
		<-resume
		return transcode.Record{ID: "controlled-consumer-contract", Spec: spec}, file.Close()
	}}
	done, joined := make(chan error, 1), make(chan struct{})
	var resumeOnce sync.Once
	releaseConsumer := func() { resumeOnce.Do(func() { close(resume) }) }
	t.Cleanup(func() {
		releaseConsumer()
		select {
		case <-joined:
		case <-time.After(5 * time.Second):
			t.Error("the controlled consuming owner did not join")
		}
	})
	go func() {
		defer close(joined)
		_, err := input.ensure(context.Background(), consumer, transcode.Spec{Scope: scope})
		done <- err
	}()
	select {
	case <-entered:
	case earlyErr := <-done:
		t.Fatalf("consumer returned before its owned wait: %v", earlyErr)
	case <-time.After(5 * time.Second):
		t.Fatal("the controlled consumer did not reach its owned wait")
	}
	if err := input.close(); !errors.Is(err, transcode.ErrOutputUnavailable) {
		t.Fatal("a caller cleanup closed an input already handed to its consumer")
	}
	stop, err := gate.acceptValidatedStop(scope)
	if err != nil {
		t.Fatal(err)
	}
	stop.finish(true)
	if usage := gate.usage(); usage.References != 2 || usage.Entries != 1 || !gate.blocked(scope) {
		t.Fatal("durable state collected an input with actual consuming owners")
	}
	releaseConsumer()
	var consumerErr error
	select {
	case consumerErr = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the controlled consumer did not return after release")
	}
	select {
	case <-joined:
	case <-time.After(5 * time.Second):
		t.Fatal("the controlled consumer did not join after return")
	}
	if consumerErr != nil || gate.usage() != (playbackStopIntentUsage{}) {
		t.Fatal("actual consumer return and descriptor Close did not join ownership")
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("a consumed input still retained its actual descriptor")
	}
}

func TestPlaybackOwnedInputRejectsOtherGateAndUnownedContexts(t *testing.T) {
	var first, second playbackStopIntentGate
	scope := playbackStopIntentTestScope("play-context-owner")
	owner, err := first.acquireValidated(scope)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.release()
	ctx := context.WithValue(context.Background(), playbackAdmissionContextKey{}, owner)
	if _, _, err := second.holdContext(ctx, scope); !errors.Is(err, transcode.ErrInvalidScope) {
		t.Fatal("another server generation accepted this ownership")
	}
	if _, _, err := first.holdContext(context.Background(), scope); !errors.Is(err, transcode.ErrInvalidScope) {
		t.Fatal("scope strings without actual ownership created a contextual loan")
	}
	other := scope
	other.ItemID = "another-item"
	if _, _, err := first.holdContext(ctx, other); !errors.Is(err, transcode.ErrInvalidScope) {
		t.Fatal("the consuming context moved to another canonical resource")
	}
}

func TestPlaybackOwnedInputPanicPreservesUnknownConsumption(t *testing.T) {
	var gate playbackStopIntentGate
	scope := playbackStopIntentTestScope("play-panic-consumer")
	owner, err := gate.acquireValidated(scope)
	if err != nil {
		t.Fatal(err)
	}
	file := playbackOwnedInputTestFile(t)
	input, err := newPlaybackOwnedInput(file, owner)
	if err != nil {
		t.Fatal(err)
	}
	panicked := false
	func() {
		defer func() { panicked = recover() != nil }()
		_, _ = input.ensure(context.Background(), playbackOwnedInputTestConsumer{run: func(context.Context, transcode.Spec, *os.File) (transcode.Record, error) {
			panic("controlled consumer ownership sentinel")
		}}, transcode.Spec{Scope: scope})
	}()
	if !panicked || input.state != playbackOwnedInputUnknown || input.reference == nil || gate.usage().References != 1 {
		t.Fatal("unknown consuming ownership was converted into a successful release")
	}
	if err := input.close(); !errors.Is(err, transcode.ErrOutputUnavailable) || gate.close() {
		t.Fatal("request/Close claimed an unknown actual consumer was joined")
	}
	// The test's temporary FD is closed by its explicit test cleanup. This
	// does not assert manager/process join or manufacture a recovered owner.
}
