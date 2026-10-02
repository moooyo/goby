//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

type mediaSourceImmediateFallbackTraceKey struct{}

type mediaSourceImmediateFallbackSQLTracer struct {
	statements atomic.Int32
	commits    atomic.Int32
}

func (trace *mediaSourceImmediateFallbackSQLTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.statements.Add(1)
	return context.WithValue(ctx, mediaSourceImmediateFallbackTraceKey{}, strings.EqualFold(strings.TrimSpace(data.SQL), "commit"))
}

func (trace *mediaSourceImmediateFallbackSQLTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if commit, _ := ctx.Value(mediaSourceImmediateFallbackTraceKey{}).(bool); commit && data.Err == nil {
		trace.commits.Add(1)
	}
}

func (*mediaSourceImmediateFallbackSQLTracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	return ctx
}

func (trace *mediaSourceImmediateFallbackSQLTracer) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {
	trace.statements.Add(1)
}

func (*mediaSourceImmediateFallbackSQLTracer) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {
}

// Only the helper receives this wrapper. Its first Done call is the queued
// wait's select setup, after the memory-only discard callback has returned.
// Keeping that select unevaluated lets the test close ready before wait runs.
type mediaSourceImmediateFallbackWaitContext struct {
	context.Context
	entered, resume         chan struct{}
	enteredOnce, resumeOnce sync.Once
}

func newMediaSourceImmediateFallbackWaitContext(ctx context.Context) *mediaSourceImmediateFallbackWaitContext {
	return &mediaSourceImmediateFallbackWaitContext{Context: ctx, entered: make(chan struct{}), resume: make(chan struct{})}
}

func (ctx *mediaSourceImmediateFallbackWaitContext) Done() <-chan struct{} {
	ctx.enteredOnce.Do(func() {
		close(ctx.entered)
		<-ctx.resume
	})
	return ctx.Context.Done()
}

func (ctx *mediaSourceImmediateFallbackWaitContext) release() {
	ctx.resumeOnce.Do(func() { close(ctx.resume) })
}

func mediaSourceImmediateFallbackTestProfile(t *testing.T, store *Store, before map[string]uint64, wants map[string]uint64) {
	t.Helper()
	after := store.MediaSourceAdmissionProfile()
	for name, want := range wants {
		if got := after[name] - before[name]; got != want {
			t.Errorf("initial IO helper profile %s=%d, want %d", name, got, want)
		}
	}
	if after["io_measurement_version"] != 5 {
		t.Error("first-fallback queue facts did not retain the version-5 profile boundary")
	}
}

func mediaSourceImmediateFallbackTestLocks(t *testing.T, fixture mediaSourceFixture, ctx context.Context, principal identity.Principal, play PlaySession) {
	t.Helper()
	playbackMediaPipelineTestRowLocked(t, ctx, fixture, "SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT", principal.SessionID, false)
	playbackMediaPipelineTestRowLocked(t, ctx, fixture, "SELECT id FROM play_sessions WHERE id=$1 FOR UPDATE NOWAIT", play.ID, false)
	if principal.IsApplicationKey() {
		playbackMediaPipelineTestRowLocked(t, ctx, fixture, "SELECT credential_id FROM application_keys WHERE credential_id=$1 FOR UPDATE NOWAIT", principal.SessionID, false)
		playbackMediaPipelineTestRowLocked(t, ctx, fixture, "SELECT id FROM application_key_clients WHERE id=$1 FOR UPDATE NOWAIT", principal.ClientSessionID, false)
	} else {
		playbackMediaPipelineTestRowLocked(t, ctx, fixture, "SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", principal.User.ID, false)
	}
}

func TestPlaybackMediaInitialFallbackImmediateRetainsCommittedFactsAndOneAUTH(t *testing.T) {
	for _, application := range []bool{false, true} {
		t.Run(map[bool]string{false: "login", true: "application"}[application], func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, application)
			trace := &mediaSourceImmediateFallbackSQLTracer{}
			fixture = mediaSourceWarmPipelineTestStore(t, fixture, trace, true)
			hint, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
			ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			worker, finish, err := fixture.store.beginMediaSourceLifetime(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer finish()
			worker = mediaSourceAuthorizationContext(worker, root, domain, false)
			handoffRelease, err := mediaSourceHandoffAdmission.acquireRoot(worker, false, root, domain)
			if err != nil {
				t.Fatal(err)
			}
			defer handoffRelease()
			held := mediaSourceRootAdmissionTestHold(t, ctx, root, domain, mediaSourceRootOwnerLimit)
			before := fixture.store.MediaSourceAdmissionProfile()
			snapshot, current, err := fixture.store.readPlaybackMediaAuthorizationPrepared(worker, principal, playbackMediaOwner(principal),
				play.ID, fixture.item.ID, play.MediaSourceID, false, &hint, nil)
			if err != nil {
				t.Fatal(err)
			}
			if trace.commits.Load() != 1 {
				t.Fatal("the initial real AUTH did not complete exactly one COMMIT")
			}
			mediaSourceImmediateFallbackTestLocks(t, fixture, worker, principal, play)
			savedSnapshot, savedCurrent := snapshot, current
			statements := trace.statements.Load()
			var misses atomic.Int32
			release, refresh, err := acquirePlaybackMediaInitialIO(worker, mediaSourceAdmission, root, domain, &snapshot, &current, func() {
				misses.Add(1)
				// This is the only injected action between the two real memory
				// acquisitions. It performs no SQL, filesystem work or waiting.
				held[0]()
			})
			if release != nil {
				t.Cleanup(release)
			}
			if err != nil || release == nil || refresh || misses.Load() != 1 ||
				!reflect.DeepEqual(snapshot, savedSnapshot) || !reflect.DeepEqual(current, savedCurrent) {
				t.Fatalf("a real miss followed by immediate fallback discarded AUTH1: refresh=%t misses=%d error=%v", refresh, misses.Load(), err)
			}
			if trace.statements.Load() != statements || trace.commits.Load() != 1 {
				t.Fatal("the first IO fallback performed SQL or repeated the committed AUTH")
			}
			ioRelease := measureMediaSourceIOOwner(release)
			var warm *warmMediaSourceRoot
			var file *os.File
			delivered := false
			defer func() {
				if (warm != nil || file != nil && !delivered) && ioRelease == nil {
					cleanupRelease, cleanupErr := mediaSourceAdmission.acquireCleanupRoot(root, domain)
					if cleanupErr != nil {
						t.Errorf("test-owned undelivered source cleanup failed: %v", cleanupErr)
						return
					}
					ioRelease = cleanupRelease
				}
				if file != nil {
					_ = file.Close()
				}
				if warm != nil {
					warm.release()
				}
				if ioRelease != nil {
					ioRelease()
				}
			}()
			playbackMediaPipelineTestOwnerCounts(t, root, 3, 0)
			mediaSourceWarmPipelineTestCounts(t, root, 3, 1)
			current.Source = snapshot.mediaFile
			if current.Play.ID != play.ID || current.Principal.SessionID != principal.SessionID || current.Source.Item.ID != fixture.item.ID {
				t.Fatal("immediate fallback did not retain the canonical committed snapshot")
			}
			// A real independent mutation verifies that delivery policy runs
			// after the AUTH SHARE locks were released, with actual IO charged.
			if _, err := fixture.pool.Exec(worker, "UPDATE play_sessions SET state=state WHERE id=$1", play.ID); err != nil {
				t.Fatal(err)
			}
			warm, err = fixture.store.borrowWarmMediaSourceRoot(hint.root)
			if err != nil || warm == nil {
				t.Fatalf("the admitted source did not have the fixture's real warm anchor: %v", err)
			}
			file, err = warm.openMediaSource(worker, snapshot)
			if err != nil || file == nil {
				t.Fatalf("immediate fallback did not perform the actual source open: %v", err)
			}
			warm.release()
			warm = nil
			ioRelease()
			ioRelease = nil
			playbackMediaPipelineTestOwnerCounts(t, root, 2, 0)
			mediaSourceWarmPipelineTestCounts(t, root, 2, 1)
			if err := fixture.store.checkOpenedMediaPublication(worker, snapshot); err != nil {
				t.Fatal(err)
			}
			delivered = true
			contents, err := io.ReadAll(file)
			_ = file.Close()
			file = nil
			if err != nil || string(contents) != fixture.contents || trace.commits.Load() != 1 {
				t.Fatalf("one-AUTH fallback did not deliver its real source: commits=%d error=%v", trace.commits.Load(), err)
			}
			handoffRelease()
			finish()
			for _, retire := range held {
				retire()
			}
			playbackMediaPipelineTestDrain(t, fixture.store)
			playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
			mediaSourceWarmPipelineTestCounts(t, root, 0, 0)
			mediaSourceImmediateFallbackTestProfile(t, fixture.store, before, map[string]uint64{
				"authorizations": 1, "try_misses": 1, "queued_grants": 1,
				"first_fallback_immediate_grants": 1, "first_fallback_actual_queued_grants": 0,
				"reauthorizations": 0, "released_queue_grants": 0, "held_refresh_grants": 0,
			})
		})
	}
}

func TestPlaybackMediaInitialFallbackQueuedReadyRequiresFreshAUTH(t *testing.T) {
	for _, application := range []bool{false, true} {
		for _, change := range []string{"success", "revoke", "binding"} {
			t.Run(map[bool]string{false: "login", true: "application"}[application]+"/"+change, func(t *testing.T) {
				fixture := mediaSourceTestCatalog(t, nil)
				principal, play := playbackMediaTestPrincipal(t, fixture, application)
				trace := &mediaSourceImmediateFallbackSQLTracer{}
				fixture = mediaSourceWarmPipelineTestStore(t, fixture, trace, true)
				hint, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
				ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
				defer cancel()
				ctx = mediaSourceAuthorizationContext(ctx, root, domain, false)
				held := mediaSourceRootAdmissionTestHold(t, ctx, root, domain, mediaSourceRootOwnerLimit)
				before := fixture.store.MediaSourceAdmissionProfile()
				snapshot, current, err := fixture.store.readPlaybackMediaAuthorizationPrepared(ctx, principal, playbackMediaOwner(principal),
					play.ID, fixture.item.ID, play.MediaSourceID, false, &hint, nil)
				if err != nil {
					t.Fatal(err)
				}
				gate := newMediaSourceImmediateFallbackWaitContext(ctx)
				t.Cleanup(gate.release)
				results := make(chan mediaSourceQueueFactTestResult, 1)
				go func() {
					release, refresh, acquireErr := acquirePlaybackMediaInitialIO(gate, mediaSourceAdmission, root, domain, &snapshot, &current, nil)
					results <- mediaSourceQueueFactTestResult{release: release, queued: refresh, err: acquireErr}
				}()
				mediaSourceAdmissionTestWait(t, gate.entered, "queued helper after discarding facts and before wait select")
				if !reflect.DeepEqual(snapshot, indexedMediaSource{}) || !reflect.DeepEqual(current, PlaybackMediaAuthorization{}) {
					t.Fatal("queued helper carried AUTH1 through its capacity wait")
				}
				captured := mediaSourceQueueFactTestWaiter(t, mediaSourceAdmission, root)
				mediaSourceImmediateFallbackTestLocks(t, fixture, ctx, principal, play)
				var want error
				switch change {
				case "revoke":
					if _, err := fixture.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", principal.SessionID); err != nil {
						t.Fatal(err)
					}
					want = identity.ErrUnauthorized
				case "binding":
					if _, err := fixture.pool.Exec(ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", root.id); err != nil {
						t.Fatal(err)
					}
					want = ErrSourceChanged
				}
				if want != nil {
					if err := os.Remove(fixture.path); err != nil {
						t.Fatal(err)
					}
				}
				// Retire the real owner while Done is still held. ready is now
				// already closed, but that cannot change the request's queue fact.
				held[0]()
				select {
				case <-captured.ready:
				default:
					t.Fatal("queued grant was not ready before releasing the wait select")
				}
				gate.release()
				var acquired mediaSourceQueueFactTestResult
				select {
				case acquired = <-results:
				case <-ctx.Done():
					t.Fatal("ready queued helper did not return")
				}
				if acquired.release != nil {
					t.Cleanup(acquired.release)
				}
				if acquired.err != nil || acquired.release == nil || !acquired.queued {
					t.Fatalf("ready-before-wait fallback reused AUTH1: refresh=%t error=%v", acquired.queued, acquired.err)
				}
				playbackMediaPipelineTestOwnerCounts(t, root, 3, 0)
				acquired.release()
				playbackMediaPipelineTestOwnerCounts(t, root, 2, 0)
				// Exercise the helper's refresh contract with another complete real
				// transaction. The helper itself owns only acquisition counters.
				snapshot, current, err = fixture.store.readPlaybackMediaAuthorizationPrepared(ctx, principal, playbackMediaOwner(principal),
					play.ID, fixture.item.ID, play.MediaSourceID, false, &hint, nil)
				if want != nil {
					if !errors.Is(err, want) || !reflect.DeepEqual(snapshot, indexedMediaSource{}) || !reflect.DeepEqual(current, PlaybackMediaAuthorization{}) {
						t.Fatalf("queued fresh AUTH did not reject %s before storage: %v", change, err)
					}
					if fixture.store.MediaSourceAdmissionProfile()["file_open_ns"] != before["file_open_ns"] {
						t.Fatal("rejected queued authority reached filesystem opening")
					}
				} else {
					if err != nil || current.Play.ID != play.ID || trace.commits.Load() != 2 {
						t.Fatalf("queued grant did not complete AUTH2: commits=%d error=%v", trace.commits.Load(), err)
					}
					ioRelease, granted, acquireErr := mediaSourceAdmission.tryAcquireRoot(ctx, false, root, domain)
					if acquireErr != nil || !granted || ioRelease == nil {
						t.Fatalf("fresh AUTH2 could not take the remaining real IO slot: %v", acquireErr)
					}
					t.Cleanup(ioRelease)
					warm, borrowErr := fixture.store.borrowWarmMediaSourceRoot(hint.root)
					if borrowErr != nil || warm == nil {
						t.Fatalf("fresh queued AUTH could not pin the warm source: %v", borrowErr)
					}
					file, openErr := warm.openMediaSource(ctx, snapshot)
					warm.release()
					ioRelease()
					if openErr != nil || file == nil {
						t.Fatalf("fresh queued AUTH could not open its actual source: %v", openErr)
					}
					contents, readErr := io.ReadAll(file)
					_ = file.Close()
					if readErr != nil || string(contents) != fixture.contents {
						t.Fatalf("fresh queued AUTH delivered an invalid descriptor: %v", readErr)
					}
				}
				for _, retire := range held {
					retire()
				}
				playbackMediaPipelineTestDrain(t, fixture.store)
				playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
				mediaSourceImmediateFallbackTestProfile(t, fixture.store, before, map[string]uint64{
					"authorizations": 2, "try_misses": 1, "queued_grants": 1,
					"first_fallback_actual_queued_grants": 1, "first_fallback_immediate_grants": 0,
				})
			})
		}
	}
}

func TestPlaybackMediaInitialFallbackCancellationRetainsActualOwner(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	principal, play := playbackMediaTestPrincipal(t, fixture, true)
	trace := &mediaSourceImmediateFallbackSQLTracer{}
	fixture = mediaSourceWarmPipelineTestStore(t, fixture, trace, true)
	hint, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	caller, cancelCaller := context.WithCancel(ctx)
	defer cancelCaller()
	// Establish the held test slots before registering an actual worker owner,
	// so a failed fixture acquisition cannot strand its lifetime or handoff.
	held := mediaSourceRootAdmissionTestHold(t, ctx, root, domain, mediaSourceRootOwnerLimit)
	worker, finish, err := fixture.store.beginMediaSourceLifetime(caller)
	if err != nil {
		t.Fatal(err)
	}
	worker = mediaSourceAuthorizationContext(worker, root, domain, false)
	handoffRelease, err := mediaSourceHandoffAdmission.acquireRoot(worker, false, root, domain)
	if err != nil {
		finish()
		t.Fatal(err)
	}
	entered, resume := make(chan struct{}), make(chan struct{})
	var resumeOnce sync.Once
	releaseBarrier := func() { resumeOnce.Do(func() { close(resume) }) }
	t.Cleanup(releaseBarrier)
	before := fixture.store.MediaSourceAdmissionProfile()
	results := make(chan playbackMediaTestResult, 1)
	var refreshOnError atomic.Bool
	go func() {
		file, _, workErr := runOwnedMediaSourceHandoff(worker, func() { handoffRelease(); finish() }, func() (*os.File, MediaFile, error) {
			snapshot, current, readErr := fixture.store.readPlaybackMediaAuthorizationPrepared(worker, principal, playbackMediaOwner(principal),
				play.ID, fixture.item.ID, play.MediaSourceID, false, &hint, nil)
			if readErr != nil {
				return nil, MediaFile{}, readErr
			}
			release, refresh, acquireErr := acquirePlaybackMediaInitialIO(worker, mediaSourceAdmission, root, domain, &snapshot, &current, func() {
				close(entered)
				// The actual helper remains owned after its receiver cancels.
				// This boundary uses memory only and does not borrow an IO slot.
				<-resume
			})
			refreshOnError.Store(refresh)
			if release != nil {
				release()
			}
			return nil, MediaFile{}, acquireErr
		}, func(file *os.File) {
			if file != nil {
				_ = file.Close()
			}
		})
		results <- playbackMediaTestResult{file: file, err: workErr}
	}()
	mediaSourceAdmissionTestWait(t, entered, "owned helper after its first actual IO miss")
	cancelCaller()
	result := awaitPlaybackMediaTest(t, ctx, results)
	if result.file != nil || !errors.Is(result.err, context.Canceled) {
		t.Fatalf("canceled fallback receiver did not return cancellation: %v", result.err)
	}
	playbackMediaPipelineTestOwnerCounts(t, root, 3, 0)
	mediaSourceWarmPipelineTestCounts(t, root, 3, 1)
	drained := make(chan struct{})
	go func() { fixture.store.mediaSourceOwners.owners.Wait(); close(drained) }()
	select {
	case <-drained:
		t.Fatal("canceled receiver abandoned its actual fallback worker")
	default:
	}
	releaseBarrier()
	mediaSourceAdmissionTestWait(t, drained, "canceled initial fallback and its retained handoff owner")
	if !refreshOnError.Load() {
		t.Fatal("fallback cancellation before enqueue lost the original failure-guard result")
	}
	for _, retire := range held {
		retire()
	}
	playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
	mediaSourceWarmPipelineTestCounts(t, root, 0, 0)
	mediaSourceImmediateFallbackTestProfile(t, fixture.store, before, map[string]uint64{
		"authorizations": 1, "try_misses": 1, "queued_grants": 0,
		"first_fallback_actual_queued_grants": 0, "first_fallback_immediate_grants": 0,
	})
	if trace.commits.Load() != 1 || fixture.store.MediaSourceAdmissionProfile()["file_open_ns"] != before["file_open_ns"] {
		t.Fatal("canceled initial fallback performed another AUTH or opened storage")
	}
}
