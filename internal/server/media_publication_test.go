package server

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/transcode"
)

func TestMediaPublicationOriginalSourceRetirementDoesNotCancelAnotherItem(t *testing.T) {
	runtime := newOriginalStreamRuntime()
	t.Cleanup(func() { runtime.stop(); runtime.wait() })
	actor := identity.Principal{User: identity.User{ID: "viewer"}, SessionID: "auth", Kind: "emby"}
	first, finishFirst, err := runtime.enterSource(actor, "first", "first-source")
	if err != nil {
		t.Fatal(err)
	}
	defer finishFirst()
	second, finishSecond, err := runtime.enterSource(actor, "second", "second-source")
	if err != nil {
		t.Fatal(err)
	}
	defer finishSecond()
	pending := runtime.retireSource("first", "first-source")
	if len(pending) != 1 || first.Err() != context.Canceled || second.Err() != nil {
		t.Fatal("source retirement did not isolate its registered reader")
	}
	select {
	case <-pending[0]:
		t.Fatal("reader was reported joined before its cleanup")
	default:
	}
	finishFirst()
	select {
	case <-pending[0]:
	default:
		t.Fatal("finished reader remained registered")
	}
}

func TestMediaPublicationRegistrationRechecksSourceAfterRegistryInsertion(t *testing.T) {
	f := newAudioRuntimeFixture(t)
	f.h.retire(f.session)
	f.h.verify = func(context.Context, identity.Principal, transcode.Scope, string, transcode.Plan) (*os.File, library.MediaFile, error) {
		f.h.mu.Lock()
		registered := len(f.h.sessions)
		f.h.mu.Unlock()
		if registered != 1 {
			t.Error("source was checked before publishing its cancellation handle")
		}
		return nil, library.MediaFile{}, library.ErrSourceChanged
	}
	if _, err := f.h.registerVerified(context.Background(), f.principal, f.source, "play_new-registration", f.decision, 0); !errors.Is(err, library.ErrSourceChanged) {
		t.Fatalf("changed source admission: %v", err)
	}
	f.h.mu.Lock()
	remaining := len(f.h.sessions)
	f.h.mu.Unlock()
	if remaining != 0 {
		t.Fatal("rejected late registration retained an output revision")
	}
}

func TestMediaPublicationRetirementDuringRegistrationCannotReturnAnActiveSession(t *testing.T) {
	f := newAudioRuntimeFixture(t)
	f.h.retire(f.session)
	var verified *os.File
	f.h.verify = func(_ context.Context, _ identity.Principal, scope transcode.Scope, _ string, _ transcode.Plan) (*os.File, library.MediaFile, error) {
		f.h.cancelMatching(scope.AuthSessionID, scope.PlaySessionID)
		verified = hlsRuntimeInput(t)
		return verified, f.source, nil
	}
	_, err := f.h.registerVerified(context.Background(), f.principal, f.source, "play_retired-registration", f.decision, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retired registration returned: %v", err)
	}
	if _, err := verified.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("verification descriptor was retained")
	}
}

func TestMediaPublicationTransientRecheckDoesNotRetireExistingSession(t *testing.T) {
	f := newAudioRuntimeFixture(t)
	transient := errors.New("temporary catalog observation failure")
	f.h.verify = func(context.Context, identity.Principal, transcode.Scope, string, transcode.Plan) (*os.File, library.MediaFile, error) {
		return nil, library.MediaFile{}, transient
	}
	_, err := f.h.registerVerified(context.Background(), f.principal, f.source, f.session.key.scope.PlaySessionID, f.decision, 0)
	if !errors.Is(err, transient) || f.session.ctx.Err() != nil {
		t.Fatal("transient recheck erased an existing grant")
	}
}

type publicationJoinJobs struct {
	*audioRuntimeJobs
	entered chan struct{}
	release chan struct{}
}

func (jobs *publicationJoinJobs) CancelSource(ctx context.Context, itemID, sourceID string) error {
	close(jobs.entered)
	select {
	case <-jobs.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestMediaPublicationJoinsOriginalReaderAndConversionBeforeCompleting(t *testing.T) {
	f := newAudioRuntimeFixture(t)
	jobs := &publicationJoinJobs{audioRuntimeJobs: f.jobs, entered: make(chan struct{}), release: make(chan struct{})}
	f.h.manager = jobs
	s := &Server{originals: newOriginalStreamRuntime(), hls: f.h}
	reader, finish, err := s.originals.enterSource(f.principal, f.source.Item.ID, f.source.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.retireReplacedMediaSource(ctx, f.source.Item.ID, f.source.SourceID) }()
	select {
	case <-jobs.entered:
	case <-ctx.Done():
		t.Fatal("producer retirement did not begin")
	}
	if reader.Err() != context.Canceled {
		t.Fatal("the old source reader was not cancelled")
	}
	select {
	case <-result:
		t.Fatal("publication completed before producer exit")
	default:
	}
	close(jobs.release)
	select {
	case <-result:
		t.Fatal("publication completed before reader cleanup")
	default:
	}
	finish()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("publication did not join its resources")
	}
}
