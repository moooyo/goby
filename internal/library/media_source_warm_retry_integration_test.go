//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

// Only the second complete AUTH transaction pauses. The first completes before
// its miss, and a third fallback transaction must be able to complete normally.
type mediaSourceWarmRetryCommitTracer struct {
	completions             atomic.Int32
	entered, resume         chan struct{}
	enteredOnce, resumeOnce sync.Once
}

func newMediaSourceWarmRetryCommitTracer() *mediaSourceWarmRetryCommitTracer {
	return &mediaSourceWarmRetryCommitTracer{entered: make(chan struct{}), resume: make(chan struct{})}
}

func (trace *mediaSourceWarmRetryCommitTracer) release() {
	trace.resumeOnce.Do(func() { close(trace.resume) })
}

func (trace *mediaSourceWarmRetryCommitTracer) TraceQueryStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	command := strings.ToLower(strings.TrimSpace(data.SQL))
	owned, _ := ctx.Value(mediaSourceAuthorizationOwnedKey{}).(bool)
	if owned && connection.PgConn().TxStatus() == 'T' && (command == "commit" || command == "rollback") && trace.completions.Add(1) == 2 {
		trace.enteredOnce.Do(func() { close(trace.entered) })
		<-trace.resume
	}
	return ctx
}

func (*mediaSourceWarmRetryCommitTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func mediaSourceWarmRetryTestHold(t *testing.T, ctx context.Context, root mediaSourceRootKey, domain string, background bool) func() {
	t.Helper()
	release, err := mediaSourceAdmission.acquireRoot(ctx, background, root, domain)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return release
}

func mediaSourceWarmRetryTestProfile(t *testing.T, store *Store, before map[string]uint64, retry, released, held, queued uint64) {
	t.Helper()
	after := store.MediaSourceAdmissionProfile()
	for name, want := range map[string]uint64{
		"retry_grants": retry, "released_queue_grants": released,
		"held_refresh_grants": held, "queued_grants": queued,
	} {
		if got := after[name] - before[name]; got != want {
			t.Errorf("bounded retry profile %s=%d, want %d", name, got, want)
		}
	}
}

func TestMediaSourceWarmPipelineFirstQueuedRefreshReleasesIO(t *testing.T) {
	for _, application := range []bool{false, true} {
		t.Run(map[bool]string{false: "login", true: "application"}[application], func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, application)
			trace := newMediaSourceWarmRetryCommitTracer()
			fixture = mediaSourceWarmPipelineTestStore(t, fixture, trace, true)
			t.Cleanup(trace.release)
			_, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
			ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			background := []func(){mediaSourceWarmRetryTestHold(t, ctx, root, domain, true), mediaSourceWarmRetryTestHold(t, ctx, root, domain, true)}
			foreground := mediaSourceWarmRetryTestHold(t, ctx, root, domain, false)
			profile := fixture.store.MediaSourceAdmissionProfile()
			var checks atomic.Int32
			results := make(chan playbackMediaTestResult, 1)
			go func() {
				file, result, err := fixture.store.AuthorizePlaybackMediaForChecked(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false,
					func(current PlaybackMediaAuthorization) error {
						if checks.Add(1) != 1 || current.Play.ID != play.ID || current.Source.Item.ID != fixture.item.ID {
							return errors.New("retry delivered a nonfinal canonical snapshot or repeated its checker")
						}
						return nil
					})
				results <- playbackMediaTestResult{file: file, result: result, err: err}
			}()
			mediaSourceRootAdmissionTestQueue(t, ctx, mediaSourceAdmission, root, 1)
			foreground()
			mediaSourceAdmissionTestWait(t, trace.entered, "second fresh AUTH after yielding the first fair grant")
			// Both background owners remain, but the refreshing foreground AUTH
			// owns no actual IO. It still owns its handoff, root pin and lifetime.
			mediaSourceWarmPipelineTestCounts(t, root, 2, 1)
			playbackMediaPipelineTestOwnerCounts(t, root, 2, 1)
			playbackMediaPipelineTestProfile(t, fixture.store, profile, 2, 0, 1, 1)
			mediaSourceWarmRetryTestProfile(t, fixture.store, profile, 0, 1, 0, 1)
			if checks.Load() != 0 {
				t.Fatal("the released fair grant invoked the delivery checker")
			}
			trace.release()
			result := awaitPlaybackMediaTest(t, ctx, results)
			if result.err != nil || result.file == nil || checks.Load() != 1 {
				if result.file != nil {
					_ = result.file.Close()
				}
				t.Fatalf("the second pure try failed to deliver once: checks=%d error=%v", checks.Load(), result.err)
			}
			contents, err := io.ReadAll(result.file)
			_ = result.file.Close()
			if err != nil || string(contents) != fixture.contents {
				t.Fatalf("the bounded retry delivered an invalid descriptor: %v", err)
			}
			playbackMediaPipelineTestDrain(t, fixture.store)
			for _, release := range background {
				release()
			}
			mediaSourceWarmPipelineTestCounts(t, root, 0, 0)
			playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
			playbackMediaPipelineTestProfile(t, fixture.store, profile, 2, 0, 1, 1)
			mediaSourceWarmRetryTestProfile(t, fixture.store, profile, 1, 1, 0, 1)
		})
	}
}

func TestMediaSourceWarmPipelineSecondMissUsesBoundedFreshFallback(t *testing.T) {
	for _, application := range []bool{false, true} {
		for _, change := range []string{"success", "revoke", "stop", "binding", "cancel"} {
			t.Run(map[bool]string{false: "login", true: "application"}[application]+"/"+change, func(t *testing.T) {
				fixture := mediaSourceTestCatalog(t, nil)
				principal, play := playbackMediaTestPrincipal(t, fixture, application)
				trace := newMediaSourceWarmRetryCommitTracer()
				fixture = mediaSourceWarmPipelineTestStore(t, fixture, trace, true)
				t.Cleanup(trace.release)
				_, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
				ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
				defer cancel()
				caller, cancelCaller := context.WithCancel(ctx)
				defer cancelCaller()
				background := []func(){mediaSourceWarmRetryTestHold(t, ctx, root, domain, true), mediaSourceWarmRetryTestHold(t, ctx, root, domain, true)}
				initial := mediaSourceWarmRetryTestHold(t, ctx, root, domain, false)
				profile := fixture.store.MediaSourceAdmissionProfile()
				before := mediaSourceWarmPipelineTestFileCount(t, fixture.path)
				var checks atomic.Int32
				results := make(chan playbackMediaTestResult, 1)
				go func() {
					file, result, err := fixture.store.AuthorizePlaybackMediaForChecked(caller, principal, play.ID, fixture.item.ID, play.MediaSourceID, false,
						func(PlaybackMediaAuthorization) error {
							if checks.Add(1) != 1 {
								return errors.New("progress fallback repeated delivery policy")
							}
							return nil
						})
					results <- playbackMediaTestResult{file: file, result: result, err: err}
				}()
				mediaSourceRootAdmissionTestQueue(t, ctx, mediaSourceAdmission, root, 1)
				initial()
				mediaSourceAdmissionTestWait(t, trace.entered, "first queued refresh without foreground IO")
				mediaSourceWarmPipelineTestCounts(t, root, 2, 1)
				playbackMediaPipelineTestOwnerCounts(t, root, 2, 1)
				// Refill the foreground lane while AUTH2 is at its completion boundary.
				// Its next pure try must miss and queue the final held-IO refresh.
				foreground := mediaSourceWarmRetryTestHold(t, ctx, root, domain, false)
				trace.release()
				mediaSourceRootAdmissionTestQueue(t, ctx, mediaSourceAdmission, root, 1)
				playbackMediaPipelineTestOwnerCounts(t, root, 3, 0)
				playbackMediaPipelineTestProfile(t, fixture.store, profile, 2, 0, 2, 1)
				mediaSourceWarmRetryTestProfile(t, fixture.store, profile, 0, 1, 0, 1)
				if checks.Load() != 0 {
					t.Fatal("a missed pure try or fair fence invoked delivery policy")
				}
				var want error
				switch change {
				case "revoke":
					_, err := fixture.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", principal.SessionID)
					if err != nil {
						t.Fatal(err)
					}
					want = identity.ErrUnauthorized
				case "stop":
					_, err := fixture.pool.Exec(ctx, "UPDATE play_sessions SET state='Stopped',stopped_at=clock_timestamp() WHERE id=$1", play.ID)
					if err != nil {
						t.Fatal(err)
					}
					want = ErrNotFound
				case "binding":
					_, err := fixture.pool.Exec(ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", root.id)
					if err != nil {
						t.Fatal(err)
					}
					want = ErrSourceChanged
				case "cancel":
					cancelCaller()
					want = context.Canceled
				}
				if change != "success" && change != "cancel" {
					if err := os.Remove(fixture.path); err != nil {
						t.Fatal(err)
					}
				}
				if change == "cancel" {
					result := awaitPlaybackMediaTest(t, ctx, results)
					if result.file != nil || !errors.Is(result.err, want) || checks.Load() != 0 {
						t.Fatalf("canceled final queue reused authority or delivered: %v", result.err)
					}
					mediaSourceWarmPipelineTestCleanupQueued(t, ctx, root)
					mediaSourceWarmPipelineTestCounts(t, root, 3, 1)
					foreground()
				} else {
					foreground()
					result := awaitPlaybackMediaTest(t, ctx, results)
					if change == "success" {
						if result.err != nil || result.file == nil || checks.Load() != 1 {
							if result.file != nil {
								_ = result.file.Close()
							}
							t.Fatalf("the final held refresh did not make bounded progress: checks=%d error=%v", checks.Load(), result.err)
						}
						contents, err := io.ReadAll(result.file)
						_ = result.file.Close()
						if err != nil || string(contents) != fixture.contents {
							t.Fatalf("the bounded fallback returned an invalid source: %v", err)
						}
					} else if result.file != nil || !errors.Is(result.err, want) || checks.Load() != 0 {
						if result.file != nil {
							_ = result.file.Close()
						}
						t.Fatalf("final queue reused AUTH2 after %s: checks=%d error=%v, want %v", change, checks.Load(), result.err, want)
					}
				}
				playbackMediaPipelineTestDrain(t, fixture.store)
				for _, release := range background {
					release()
				}
				mediaSourceWarmPipelineTestCounts(t, root, 0, 0)
				playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
				if change == "cancel" {
					playbackMediaPipelineTestProfile(t, fixture.store, profile, 2, 0, 2, 1)
					mediaSourceWarmRetryTestProfile(t, fixture.store, profile, 0, 1, 0, 1)
				} else {
					playbackMediaPipelineTestProfile(t, fixture.store, profile, 3, 0, 2, 2)
					mediaSourceWarmRetryTestProfile(t, fixture.store, profile, 0, 1, 1, 2)
				}
				if change != "success" && (fixture.store.MediaSourceAdmissionProfile()["file_open_ns"] != profile["file_open_ns"] ||
					mediaSourceWarmPipelineTestFileCount(t, fixture.path) != before) {
					t.Fatal("a rejected fallback reached source opening or retained a descriptor")
				}
			})
		}
	}
}

func TestMediaSourceWarmPipelineFirstQueuedRefreshCancellationRetainsOwner(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	principal, play := playbackMediaTestPrincipal(t, fixture, true)
	trace := newMediaSourceWarmRetryCommitTracer()
	fixture = mediaSourceWarmPipelineTestStore(t, fixture, trace, true)
	t.Cleanup(trace.release)
	_, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	caller, cancelCaller := context.WithCancel(ctx)
	defer cancelCaller()
	background := []func(){mediaSourceWarmRetryTestHold(t, ctx, root, domain, true), mediaSourceWarmRetryTestHold(t, ctx, root, domain, true)}
	foreground := mediaSourceWarmRetryTestHold(t, ctx, root, domain, false)
	profile := fixture.store.MediaSourceAdmissionProfile()
	before := mediaSourceWarmPipelineTestFileCount(t, fixture.path)
	var checks atomic.Int32
	results := make(chan playbackMediaTestResult, 1)
	go func() {
		file, result, err := fixture.store.AuthorizePlaybackMediaForChecked(caller, principal, play.ID, fixture.item.ID, play.MediaSourceID, false,
			func(PlaybackMediaAuthorization) error { checks.Add(1); return nil })
		results <- playbackMediaTestResult{file: file, result: result, err: err}
	}()
	mediaSourceRootAdmissionTestQueue(t, ctx, mediaSourceAdmission, root, 1)
	foreground()
	mediaSourceAdmissionTestWait(t, trace.entered, "cancelable fresh AUTH after a released fair grant")
	cancelCaller()
	result := awaitPlaybackMediaTest(t, ctx, results)
	if result.file != nil || !errors.Is(result.err, context.Canceled) || checks.Load() != 0 {
		t.Fatalf("canceled yielded AUTH delivered a descriptor or checked policy: %v", result.err)
	}
	mediaSourceWarmPipelineTestCounts(t, root, 2, 1)
	playbackMediaPipelineTestOwnerCounts(t, root, 2, 1)
	drained := make(chan struct{})
	go func() { fixture.store.mediaSourceOwners.owners.Wait(); close(drained) }()
	select {
	case <-drained:
		t.Fatal("released IO capacity also abandoned the still-owned AUTH/root lifetime")
	default:
	}
	trace.release()
	mediaSourceAdmissionTestWait(t, drained, "canceled yielded AUTH and captured-lane pin cleanup")
	for _, release := range background {
		release()
	}
	mediaSourceWarmPipelineTestCounts(t, root, 0, 0)
	playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
	playbackMediaPipelineTestProfile(t, fixture.store, profile, 2, 0, 1, 1)
	mediaSourceWarmRetryTestProfile(t, fixture.store, profile, 0, 1, 0, 1)
	if fixture.store.MediaSourceAdmissionProfile()["file_open_ns"] != profile["file_open_ns"] ||
		mediaSourceWarmPipelineTestFileCount(t, fixture.path) != before {
		t.Fatal("canceled yielded AUTH opened or retained a source descriptor")
	}
}
