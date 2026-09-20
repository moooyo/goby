package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/analysiscache"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func analysisLifecycleFixture() *mediaAnalysisRuntime {
	ctx, cancel := context.WithCancel(context.Background())
	return &mediaAnalysisRuntime{ctx: ctx, cancel: cancel, done: make(chan struct{}), publish: make(chan struct{}, 1)}
}

func TestAnalysisShutdownRetainsOwnedWorkAfterCallerTimeout(t *testing.T) {
	runtime := analysisLifecycleFixture()
	work, leave, err := runtime.enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	runtime.BeginClose()
	if !errors.Is(work.Err(), context.Canceled) {
		t.Fatal("shutdown did not cancel the owned operation")
	}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.Close(expired); !errors.Is(err, context.Canceled) {
		t.Fatal("a waiting caller falsely completed live analysis shutdown")
	}
	select {
	case <-runtime.done:
		t.Fatal("cancellation released an operation that has not returned")
	default:
	}
	if _, extraLeave, err := runtime.enter(context.Background()); err == nil {
		extraLeave()
		t.Fatal("closing analysis accepted another operation")
	}
	leave()
	leave()
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := runtime.Close(bounded); err != nil {
		t.Fatal(err)
	}
}

func TestAnalysisPublicationWaitIsCancellableWithoutTakingAnotherOwnersGate(t *testing.T) {
	runtime := analysisLifecycleFixture()
	unlocked, err := runtime.lockPublication(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if release, err := runtime.lockPublication(expired); release != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled publication acquired another owner's gate")
	}
	if len(runtime.publish) != 1 {
		t.Fatal("canceling a waiter released the active publication")
	}
	unlocked()
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	release, err := runtime.lockPublication(bounded)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err := runtime.Close(bounded); err != nil {
		t.Fatal(err)
	}
}

func TestAnalysisKnownDependencyFailureWithdrawsOnlyAffectedAdmission(t *testing.T) {
	runtime := analysisLifecycleFixture()
	runtime.profiles = map[string]library.AnalysisExecutionProfile{
		library.TaskIntroAnalysisKey:     {Version: library.AnalysisProfileVersion, Available: true},
		library.TaskPreviewGenerationKey: {Version: library.AnalysisProfileVersion, Available: true},
	}
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, library.ErrAnalysisSourceChanged, analysiscache.ErrLimit} {
		runtime.rememberFailure(library.TaskIntroAnalysisKey, err)
		profile, profileErr := runtime.executionProfile(library.TaskIntroAnalysisKey)
		if profileErr != nil || !profile.Available {
			t.Fatal("a task-local failure withdrew unrelated executable inventory")
		}
	}
	runtime.rememberFailure(library.TaskIntroAnalysisKey, media.ErrAnalysisUnavailable)
	intro, err := runtime.executionProfile(library.TaskIntroAnalysisKey)
	if err != nil || intro.Available || intro.UnavailableReason != "dependencies_unavailable" || library.ValidateAnalysisExecutionProfile(intro) != nil {
		t.Fatal("known missing dependency did not produce a closed unavailable admission snapshot")
	}
	preview, err := runtime.executionProfile(library.TaskPreviewGenerationKey)
	if err != nil || !preview.Available {
		t.Fatal("intro dependency failure disabled the independent preview profile")
	}
	runtime.rememberFailure("", analysiscache.ErrUnsafe)
	for _, key := range []string{library.TaskIntroAnalysisKey, library.TaskPreviewGenerationKey} {
		profile, err := runtime.executionProfile(key)
		if err != nil || profile.Available || profile.UnavailableReason != "cache_unavailable" || library.ValidateAnalysisExecutionProfile(profile) != nil {
			t.Fatal("unsafe shared storage retained an executable admission")
		}
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
