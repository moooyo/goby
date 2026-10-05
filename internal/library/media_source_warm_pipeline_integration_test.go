//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

type mediaSourceWarmPublicationTraceKey struct{}

type mediaSourceWarmPublicationTracer struct {
	itemID                    string
	entered, resume, finished chan struct{}
	enteredOnce, resumeOnce   sync.Once
	finishedOnce              sync.Once
}

type mediaSourceWarmCommitTracer struct {
	entered, resume         chan struct{}
	enteredOnce, resumeOnce sync.Once
}

func newMediaSourceWarmCommitTracer() *mediaSourceWarmCommitTracer {
	return &mediaSourceWarmCommitTracer{entered: make(chan struct{}), resume: make(chan struct{})}
}

func (trace *mediaSourceWarmCommitTracer) release() {
	trace.resumeOnce.Do(func() { close(trace.resume) })
}

func (trace *mediaSourceWarmCommitTracer) TraceQueryStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	command := strings.ToLower(strings.TrimSpace(data.SQL))
	owned, _ := ctx.Value(mediaSourceAuthorizationOwnedKey{}).(bool)
	if owned && connection.PgConn().TxStatus() == 'T' && (command == "commit" || command == "rollback") {
		trace.enteredOnce.Do(func() { close(trace.entered) })
		// Keep the actual transaction owner at its completion boundary, including
		// after caller cancellation, until the independent observer releases it.
		<-trace.resume
	}
	return ctx
}

func (*mediaSourceWarmCommitTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func newMediaSourceWarmPublicationTracer(itemID string) *mediaSourceWarmPublicationTracer {
	return &mediaSourceWarmPublicationTracer{itemID: itemID, entered: make(chan struct{}), resume: make(chan struct{}), finished: make(chan struct{})}
}

func (trace *mediaSourceWarmPublicationTracer) release() {
	trace.resumeOnce.Do(func() { close(trace.resume) })
}

func (trace *mediaSourceWarmPublicationTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	selected := len(data.Args) == 3 && data.Args[0] == trace.itemID &&
		strings.HasPrefix(data.SQL, "SELECT "+MediaOperationSourceRevisionSQL+",EXISTS (")
	if selected {
		trace.enteredOnce.Do(func() { close(trace.entered) })
		// Ignore cancellation until the test releases this boundary. The
		// request may return while its actual query/descriptor owner remains.
		<-trace.resume
	}
	return context.WithValue(ctx, mediaSourceWarmPublicationTraceKey{}, selected)
}

func (trace *mediaSourceWarmPublicationTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if selected, _ := ctx.Value(mediaSourceWarmPublicationTraceKey{}).(bool); selected {
		trace.finishedOnce.Do(func() { close(trace.finished) })
	}
}

// The isolated adapter shares only the fixture's already owned catalog and
// published anchors. It never retires those shared resources. Its private pool,
// source owners and any newly initialized cold anchors are cleaned up first.
func mediaSourceWarmPipelineTestStore(t *testing.T, fixture mediaSourceFixture, trace pgx.QueryTracer, warm bool) mediaSourceFixture {
	t.Helper()
	playbackMediaPipelineTestDrain(t, fixture.store)
	config := fixture.pool.Config().Copy()
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(fixture.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, pool)
	store := &Store{pool: pool, ownership: fixture.store.ownership}
	shared := make(map[*os.Root]bool)
	fixture.store.mu.Lock()
	store.roots = append([]approvedRoot(nil), fixture.store.roots...)
	store.rootBindingAnchors = make(map[string]rootBindingAnchor, len(fixture.store.rootBindingAnchors))
	for _, approved := range fixture.store.roots {
		if approved.root != nil {
			shared[approved.root] = true
		}
	}
	for id, anchor := range fixture.store.rootBindingAnchors {
		if anchor.approved != nil {
			shared[anchor.approved] = true
		}
		if warm {
			store.rootBindingAnchors[id] = anchor
		}
	}
	fixture.store.mu.Unlock()
	if !warm {
		for index := range store.roots {
			store.roots[index].root = nil
		}
	}
	t.Cleanup(func() {
		store.closing.Store(true)
		store.beginCloseMediaSources()
		playbackMediaPipelineTestDrain(t, store)
		store.mu.Lock()
		store.closed = true
		for _, approved := range store.roots {
			if approved.root != nil && !shared[approved.root] {
				store.retireRootAnchorLocked(approved.root)
			}
		}
		for _, anchor := range store.rootBindingAnchors {
			if anchor.approved != nil && !shared[anchor.approved] {
				store.retireRootAnchorLocked(anchor.approved)
			}
		}
		store.mu.Unlock()
		done := make(chan struct{})
		go func() { store.rootOpens.Wait(); store.rootClosures.Wait(); close(done) }()
		mediaSourceAdmissionTestWait(t, done, "isolated cold anchor cleanup")
	})
	fixture.store, fixture.pool = store, pool
	return fixture
}

func mediaSourceWarmPipelineTestCounts(t *testing.T, root mediaSourceRootKey, wantIO, wantHandoff int) {
	t.Helper()
	mediaSourceAdmission.mu.Lock()
	ioOwners := mediaSourceAdmission.roots[root].active
	mediaSourceAdmission.mu.Unlock()
	mediaSourceHandoffAdmission.mu.Lock()
	handoffOwners := mediaSourceHandoffAdmission.roots[root].active
	mediaSourceHandoffAdmission.mu.Unlock()
	if ioOwners != wantIO || handoffOwners != wantHandoff {
		t.Fatalf("warm pipeline owners: actual_io=%d handoff=%d; want %d/%d", ioOwners, handoffOwners, wantIO, wantHandoff)
	}
}

func mediaSourceWarmPipelineTestFileCount(t *testing.T, path string) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if current, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name())); err == nil && current == path {
			count++
		}
	}
	return count
}

func mediaSourceWarmPipelineTestCleanupQueued(t *testing.T, ctx context.Context, root mediaSourceRootKey) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		mediaSourceAdmission.mu.Lock()
		queued := false
		for _, waiter := range mediaSourceAdmission.waiters {
			queued = queued || waiter.root == root && waiter.cleanup
		}
		mediaSourceAdmission.mu.Unlock()
		if queued {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("owned file cleanup did not queue: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestMediaSourceWarmPipelinePublicationWaitReleasesActualIO(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	principal, play := playbackMediaTestPrincipal(t, fixture, false)
	healthy := mediaSourceTestCatalog(t, nil)
	healthyPrincipal, healthyPlay := playbackMediaTestPrincipal(t, healthy, false)
	trace := newMediaSourceWarmPublicationTracer(fixture.item.ID)
	fixture = mediaSourceWarmPipelineTestStore(t, fixture, trace, true)
	t.Cleanup(trace.release)
	_, root, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	before := mediaSourceWarmPipelineTestFileCount(t, fixture.path)
	profile := fixture.store.MediaSourceAdmissionProfile()
	results := make(chan playbackMediaTestResult, 1)
	go func() {
		file, result, err := fixture.store.AuthorizePlaybackMediaFor(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false)
		results <- playbackMediaTestResult{file: file, result: result, err: err}
	}()
	mediaSourceAdmissionTestWait(t, trace.entered, "post-open publication query boundary")
	mediaSourceWarmPipelineTestCounts(t, root, 0, 1)
	playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
	if got := mediaSourceWarmPipelineTestFileCount(t, fixture.path); got != before+1 {
		t.Fatalf("publication boundary did not retain exactly one opened source descriptor: %d, want %d", got, before+1)
	}
	if got := fixture.store.MediaSourceAdmissionProfile()["warm_opens"] - profile["warm_opens"]; got != 1 {
		t.Fatalf("publication test did not execute the warm root path: %d", got)
	}
	file, _, err := healthy.store.AuthorizePlaybackMediaFor(ctx, healthyPrincipal, healthyPlay.ID, healthy.item.ID, healthyPlay.MediaSourceID, false)
	if err != nil || file == nil {
		t.Fatalf("an unrelated root could not progress beside a publication wait: %v", err)
	}
	_ = file.Close()
	trace.release()
	result := awaitPlaybackMediaTest(t, ctx, results)
	if result.err != nil || result.file == nil {
		t.Fatalf("publication readback did not resume: %v", result.err)
	}
	contents, err := io.ReadAll(result.file)
	_ = result.file.Close()
	if err != nil || string(contents) != fixture.contents {
		t.Fatalf("warm pipeline returned an invalid descriptor: %v", err)
	}
	playbackMediaPipelineTestDrain(t, fixture.store)
	mediaSourceWarmPipelineTestCounts(t, root, 0, 0)
}

func TestMediaSourceWarmPipelineCanceledPublicationRetainsFileUntilCleanupAdmission(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	principal, play := playbackMediaTestPrincipal(t, fixture, true)
	healthy := mediaSourceTestCatalog(t, nil)
	healthyPrincipal, healthyPlay := playbackMediaTestPrincipal(t, healthy, false)
	trace := newMediaSourceWarmPublicationTracer(fixture.item.ID)
	fixture = mediaSourceWarmPipelineTestStore(t, fixture, trace, true)
	t.Cleanup(trace.release)
	_, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	caller, cancelCaller := context.WithCancel(ctx)
	defer cancelCaller()
	before := mediaSourceWarmPipelineTestFileCount(t, fixture.path)
	profile := fixture.store.MediaSourceAdmissionProfile()
	results := make(chan playbackMediaTestResult, 1)
	go func() {
		file, result, err := fixture.store.AuthorizePlaybackMediaFor(caller, principal, play.ID, fixture.item.ID, play.MediaSourceID, false)
		results <- playbackMediaTestResult{file: file, result: result, err: err}
	}()
	mediaSourceAdmissionTestWait(t, trace.entered, "opened source waiting for publication readback")
	mediaSourceWarmPipelineTestCounts(t, root, 0, 1)
	releases := mediaSourceRootAdmissionTestHold(t, ctx, root, domain, mediaSourceRootOwnerLimit)
	cancelCaller()
	result := awaitPlaybackMediaTest(t, ctx, results)
	if result.file != nil || !errors.Is(result.err, context.Canceled) {
		t.Fatalf("canceled publication caller received a descriptor or wrong error: %v", result.err)
	}
	ownersDone := make(chan struct{})
	go func() { fixture.store.mediaSourceOwners.owners.Wait(); close(ownersDone) }()
	trace.release()
	mediaSourceWarmPipelineTestCleanupQueued(t, ctx, root)
	mediaSourceWarmPipelineTestCounts(t, root, mediaSourceRootOwnerLimit, 1)
	select {
	case <-ownersDone:
		t.Fatal("the source lifetime drained while its undelivered descriptor cleanup was queued")
	default:
	}
	if got := mediaSourceWarmPipelineTestFileCount(t, fixture.path); got != before+1 {
		t.Fatalf("the canceled receiver closed a worker-owned descriptor before cleanup admission: %d, want %d", got, before+1)
	}
	file, _, err := healthy.store.AuthorizePlaybackMediaFor(ctx, healthyPrincipal, healthyPlay.ID, healthy.item.ID, healthyPlay.MediaSourceID, false)
	if err != nil || file == nil {
		t.Fatalf("queued cleanup on one root prevented another configured domain from progressing: %v", err)
	}
	_ = file.Close()
	for _, release := range releases {
		release()
	}
	mediaSourceAdmissionTestWait(t, ownersDone, "uncancelable actual source cleanup")
	mediaSourceWarmPipelineTestCounts(t, root, 0, 0)
	if got := mediaSourceWarmPipelineTestFileCount(t, fixture.path); got != before {
		t.Fatalf("undelivered source descriptor remained after its actual owner drained: %d, want %d", got, before)
	}
	after := fixture.store.MediaSourceAdmissionProfile()
	if after["cleanup_wait_ns"] <= profile["cleanup_wait_ns"] || after["cleanup_held_ns"] <= profile["cleanup_held_ns"] {
		t.Fatal("canceled warm publication did not record actual admitted cleanup")
	}
}

type mediaSourceHandoffRejectContext struct {
	context.Context
	ready  *atomic.Bool
	cancel context.CancelFunc
}

func (ctx mediaSourceHandoffRejectContext) Err() error {
	if ctx.ready.Load() {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func TestOwnedMediaSourceHandoffCleanupRetainsActualIOAndStoreOwner(t *testing.T) {
	for _, failure := range []string{"caller-cancel", "receiver-reject", "work-error"} {
		t.Run(failure, func(t *testing.T) {
			store := mediaSourceAdmissionTestStore()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			caller, cancelCaller := context.WithCancel(ctx)
			defer cancelCaller()
			worker, finish, err := store.beginMediaSourceLifetime(caller)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(finish)
			t.Cleanup(func() {
				closeContext, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancelClose()
				if err := store.Close(closeContext); err != nil {
					t.Errorf("handoff fixture did not drain: %v", err)
				}
			})
			file, err := os.CreateTemp(t.TempDir(), "owned-handoff-*.media")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			root := mediaSourceRootKey{catalog: "handoff-cleanup-fixture", id: failure}
			domain := filepath.Dir(file.Name())
			releaseHandoff, err := mediaSourceHandoffAdmission.acquireRoot(ctx, false, root, domain)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(releaseHandoff)
			workStarted, cleanupStarted, resumeCleanup := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var resumeOnce, enteredOnce sync.Once
			t.Cleanup(func() { resumeOnce.Do(func() { close(resumeCleanup) }) })
			var ready atomic.Bool
			var handoffContext context.Context = worker
			if failure == "receiver-reject" {
				// Cancellation begins at the receiver's first post-work authority
				// check. Done remains open until the result has been selected.
				handoffContext = mediaSourceHandoffRejectContext{Context: worker, ready: &ready, cancel: cancelCaller}
			}
			var releases, cleanups atomic.Int32
			cleanupResult := make(chan error, 1)
			results := make(chan mediaSourceWorkerTestResult, 1)
			go func() {
				opened, source, err := runOwnedMediaSourceHandoff(handoffContext, func() {
					releaseHandoff()
					releases.Add(1)
					finish()
				}, func() (*os.File, MediaFile, error) {
					close(workStarted)
					if failure == "caller-cancel" {
						<-worker.Done()
					} else if failure == "receiver-reject" {
						ready.Store(true)
					} else {
						return file, MediaFile{}, ErrForbidden
					}
					return file, MediaFile{}, nil
				}, func(undelivered *os.File) {
					cleanups.Add(1)
					releaseIO, err := mediaSourceAdmission.acquireCleanupRoot(root, domain)
					if err != nil {
						cleanupResult <- err
						return
					}
					defer releaseIO()
					enteredOnce.Do(func() { close(cleanupStarted) })
					<-resumeCleanup
					cleanupResult <- undelivered.Close()
				})
				results <- mediaSourceWorkerTestResult{file: opened, source: source, err: err}
			}()
			mediaSourceAdmissionTestWait(t, workStarted, "owned handoff work")
			if failure == "caller-cancel" {
				cancelCaller()
			}
			mediaSourceAdmissionTestWait(t, cleanupStarted, "worker-owned cleanup actual IO admission")
			if failure == "work-error" {
				cancelCaller()
			}
			result := mediaSourceRootAdmissionTestReceive(t, ctx, results)
			if result.file != nil || !errors.Is(result.err, context.Canceled) {
				t.Fatalf("canceled or refusing receiver obtained a file: %v", result.err)
			}
			mediaSourceWarmPipelineTestCounts(t, root, 1, 1)
			if _, err := file.Stat(); err != nil || releases.Load() != 0 || cleanups.Load() != 1 {
				t.Fatalf("the receiver closed the file or released the actual cleanup owner: releases=%d cleanups=%d error=%v", releases.Load(), cleanups.Load(), err)
			}
			expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancelExpired()
			if err := store.Close(expired); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Store Close did not retain blocked descriptor cleanup: %v", err)
			}
			select {
			case <-store.done:
				t.Fatal("Store drained before the worker actually closed its undelivered file")
			default:
			}
			resumeOnce.Do(func() { close(resumeCleanup) })
			mediaSourceAdmissionTestWait(t, store.done, "worker-only file cleanup and Store drain")
			if err := <-cleanupResult; err != nil {
				t.Fatalf("the worker could not close its undelivered descriptor: %v", err)
			}
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) || releases.Load() != 1 || cleanups.Load() != 1 {
				t.Fatalf("handoff cleanup was incomplete or repeated: releases=%d cleanups=%d error=%v", releases.Load(), cleanups.Load(), err)
			}
			mediaSourceWarmPipelineTestCounts(t, root, 0, 0)
		})
	}
}

func TestOwnedMediaSourceHandoffAcceptedACKSurvivesLaterCancellation(t *testing.T) {
	store := mediaSourceAdmissionTestStore()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	caller, cancelCaller := context.WithCancel(ctx)
	defer cancelCaller()
	worker, finish, err := store.beginMediaSourceLifetime(caller)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(finish)
	t.Cleanup(func() {
		closeContext, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelClose()
		if err := store.Close(closeContext); err != nil {
			t.Errorf("accepted handoff fixture did not drain: %v", err)
		}
	})
	file, err := os.CreateTemp(t.TempDir(), "accepted-handoff-*.media")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	root := mediaSourceRootKey{catalog: "handoff-accepted-fixture", id: "accepted"}
	domain := filepath.Dir(file.Name())
	releaseHandoff, err := mediaSourceHandoffAdmission.acquireRoot(ctx, false, root, domain)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseHandoff)
	entered, resume := make(chan struct{}), make(chan struct{})
	var resumeOnce sync.Once
	t.Cleanup(func() { resumeOnce.Do(func() { close(resume) }) })
	var cleanups, releases atomic.Int32
	opened, _, err := runOwnedMediaSourceHandoff(worker, func() {
		close(entered)
		<-resume
		releaseHandoff()
		releases.Add(1)
		finish()
	}, func() (*os.File, MediaFile, error) { return file, MediaFile{}, nil }, func(file *os.File) {
		cleanups.Add(1)
		_ = file.Close()
	})
	if err != nil || opened != file {
		t.Fatalf("the receiver did not accept the handoff: %v", err)
	}
	mediaSourceAdmissionTestWait(t, entered, "accepted ACK owner finalization")
	cancelCaller()
	if _, err := opened.Stat(); err != nil || cleanups.Load() != 0 {
		t.Fatalf("later cancellation reclaimed a descriptor after accepted ACK: cleanups=%d error=%v", cleanups.Load(), err)
	}
	mediaSourceWarmPipelineTestCounts(t, root, 0, 1)
	resumeOnce.Do(func() { close(resume) })
	playbackMediaPipelineTestDrain(t, store)
	if releases.Load() != 1 || cleanups.Load() != 0 {
		t.Fatalf("accepted handoff owner did not finish exactly once: releases=%d cleanups=%d", releases.Load(), cleanups.Load())
	}
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := opened.Stat(); err != nil {
		t.Fatalf("Store drain closed a successfully handed-off descriptor: %v", err)
	}
	mediaSourceWarmPipelineTestCounts(t, root, 0, 0)
}

func TestMediaSourceWarmPipelineRetainsAnchorAndFileSnapshotGuards(t *testing.T) {
	for _, change := range []string{"anchor-replacement", "ctime", "named-symlink"} {
		t.Run(change, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			source, err := fixture.store.readMediaSourceFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			_, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
			playbackMediaPipelineTestDrain(t, fixture.store)
			warm, err := fixture.store.borrowWarmMediaSourceRoot(source.root)
			if err != nil || warm == nil {
				t.Fatalf("the source fixture did not have a warm anchor: %v", err)
			}
			releaseIO, err := mediaSourceAdmission.acquireRoot(fixture.ctx, false, root, domain)
			if err != nil {
				warm.release()
				t.Fatal(err)
			}
			defer func() { warm.release(); releaseIO() }()
			switch change {
			case "anchor-replacement":
				replacement, err := warm.reference.approved.OpenRoot(".")
				if err != nil {
					t.Fatal(err)
				}
				fixture.store.mu.Lock()
				fixture.store.installRootBindingAnchorLocked(source.root, replacement)
				fixture.store.mu.Unlock()
			case "ctime":
				if err := os.WriteFile(fixture.path, []byte(fixture.contents), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(fixture.path, source.mediaFile.ModifiedAt, source.mediaFile.ModifiedAt); err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(fixture.path)
				if err != nil || fileIdentity(info) != source.identity || info.Size() != source.mediaFile.Size ||
					!catalogModifiedTime(info).Equal(source.mediaFile.ModifiedAt) || media.FileChangeTime(info) == source.mediaFile.Item.Media.FileChangeTimeNs {
					t.Fatalf("the test did not isolate a changed ctime with the old source inode/size/mtime: %v", err)
				}
			case "named-symlink":
				backup := fixture.path + ".original"
				if err := os.Rename(fixture.path, backup); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Base(backup), fixture.path); err != nil {
					t.Fatal(err)
				}
			}
			file, err := warm.openMediaSource(fixture.ctx, source)
			if file != nil {
				_ = file.Close()
			}
			if file != nil || !errors.Is(err, ErrUnavailable) || !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("the warm filesystem primitive accepted changed %s identity: %v", change, err)
			}
		})
	}
}

func TestMediaSourceWarmPipelineColdFallbackKeepsConservativeIOOwnership(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	principal, play := playbackMediaTestPrincipal(t, fixture, false)
	trace := newMediaSourceWarmPublicationTracer(fixture.item.ID)
	fixture = mediaSourceWarmPipelineTestStore(t, fixture, trace, false)
	t.Cleanup(trace.release)
	_, root, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	before := fixture.store.MediaSourceAdmissionProfile()
	results := make(chan playbackMediaTestResult, 1)
	go func() {
		file, result, err := fixture.store.AuthorizePlaybackMediaFor(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false)
		results <- playbackMediaTestResult{file: file, result: result, err: err}
	}()
	mediaSourceAdmissionTestWait(t, trace.entered, "cold source publication readback")
	mediaSourceWarmPipelineTestCounts(t, root, 1, 1)
	after := fixture.store.MediaSourceAdmissionProfile()
	if after["cold_fallbacks"]-before["cold_fallbacks"] != 1 || after["warm_opens"] != before["warm_opens"] {
		t.Fatal("the cold fixture did not use the conservative fallback")
	}
	trace.release()
	result := awaitPlaybackMediaTest(t, ctx, results)
	if result.err != nil || result.file == nil {
		t.Fatalf("the conservative cold source path failed: %v", result.err)
	}
	_ = result.file.Close()
	playbackMediaPipelineTestDrain(t, fixture.store)
	mediaSourceWarmPipelineTestCounts(t, root, 0, 0)
}

func TestMediaSourceWarmPipelineBorrowFailurePreservesRevokedCredentialPrecedence(t *testing.T) {
	for _, application := range []bool{false, true} {
		t.Run(map[bool]string{false: "login", true: "application"}[application], func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, application)
			fixture = mediaSourceWarmPipelineTestStore(t, fixture, nil, true)
			hint, root, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
			fixture.store.mu.Lock()
			anchor := fixture.store.rootBindingAnchors[hint.root.id]
			if anchor.approved == nil {
				for _, approved := range fixture.store.roots {
					if approved.path == hint.root.allowedPath {
						anchor.approved = approved.root
						break
					}
				}
			}
			anchor.root = hint.root
			anchor.root.path += "/stale-memory-binding"
			fixture.store.rootBindingAnchors[hint.root.id] = anchor
			fixture.store.mu.Unlock()
			if anchor.approved == nil {
				t.Fatal("the fixture did not retain an approved descriptor for its stale memory mapping")
			}
			warm, borrowErr := fixture.store.borrowWarmMediaSourceRoot(hint.root)
			if warm != nil || !errors.Is(borrowErr, ErrUnavailable) {
				t.Fatalf("the fixture did not fail its pre-authorization memory-only binding lookup: %v", borrowErr)
			}
			ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			if _, err := fixture.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", principal.SessionID); err != nil {
				t.Fatal(err)
			}
			before := mediaSourceWarmPipelineTestFileCount(t, fixture.path)
			var checks atomic.Int32
			file, _, err := fixture.store.AuthorizePlaybackMediaForChecked(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false,
				func(PlaybackMediaAuthorization) error { checks.Add(1); return nil })
			if file != nil {
				_ = file.Close()
			}
			if file != nil || !errors.Is(err, identity.ErrUnauthorized) || checks.Load() != 0 {
				t.Fatalf("stale memory mapping replaced the credential error or reached delivery: checks=%d error=%v", checks.Load(), err)
			}
			playbackMediaPipelineTestDrain(t, fixture.store)
			mediaSourceWarmPipelineTestCounts(t, root, 0, 0)
			playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
			if got := mediaSourceWarmPipelineTestFileCount(t, fixture.path); got != before {
				t.Fatalf("a rejected credential opened or retained a source descriptor: %d, want %d", got, before)
			}
		})
	}
}

func TestMediaSourceWarmPipelinePublicationPanicClosesOpenedDescriptorAndOwners(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	principal, play := playbackMediaTestPrincipal(t, fixture, false)
	fixture = mediaSourceWarmPipelineTestStore(t, fixture, nil, true)
	_, root, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	before := mediaSourceWarmPipelineTestFileCount(t, fixture.path)
	profile := fixture.store.MediaSourceAdmissionProfile()
	pool := fixture.store.pool
	var checks atomic.Int32
	file, _, err := fixture.store.AuthorizePlaybackMediaForChecked(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false,
		func(PlaybackMediaAuthorization) error {
			if checks.Add(1) != 1 {
				return errors.New("publication panic fixture invoked delivery policy more than once")
			}
			// This private Store has exactly one source worker. All preparation
			// and committed authorization pool reads precede this callback in
			// that same worker. Warm filesystem opening never reads store.pool;
			// the next read is the deliberately failing publication boundary.
			fixture.store.pool = nil
			return nil
		})
	if file != nil {
		_ = file.Close()
	}
	// Restore only after the actual worker and its panic cleanup have drained.
	// The isolated fixture's shutdown uses the restored pool without racing any
	// source worker; the original fixture pool was never changed.
	playbackMediaPipelineTestDrain(t, fixture.store)
	fixture.store.pool = pool
	if file != nil || !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "worker panicked") || checks.Load() != 1 {
		t.Fatalf("the warm publication panic escaped its boundary or delivered a file: checks=%d error=%v", checks.Load(), err)
	}
	after := fixture.store.MediaSourceAdmissionProfile()
	if after["warm_opens"]-profile["warm_opens"] != 1 || after["file_open_ns"] <= profile["file_open_ns"] {
		t.Fatal("the injected publication panic did not follow warm filesystem opening")
	}
	mediaSourceWarmPipelineTestCounts(t, root, 0, 0)
	playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
	if got := mediaSourceWarmPipelineTestFileCount(t, fixture.path); got != before {
		t.Fatalf("the warm publication panic retained a source descriptor: %d, want %d", got, before)
	}
}

func TestMediaSourceWarmPipelineCommitsBeforeImmediateIOAdmission(t *testing.T) {
	for _, application := range []bool{false, true} {
		t.Run(map[bool]string{false: "login", true: "application"}[application], func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, application)
			trace := newMediaSourceWarmCommitTracer()
			fixture = mediaSourceWarmPipelineTestStore(t, fixture, trace, true)
			t.Cleanup(trace.release)
			_, root, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
			ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			profile := fixture.store.MediaSourceAdmissionProfile()
			var checks atomic.Int32
			results := make(chan playbackMediaTestResult, 1)
			go func() {
				file, result, err := fixture.store.AuthorizePlaybackMediaForChecked(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false,
					func(PlaybackMediaAuthorization) error {
						mediaSourceAdmission.mu.Lock()
						ioOwners := mediaSourceAdmission.roots[root].active
						mediaSourceAdmission.mu.Unlock()
						mediaSourceAuthorizationAdmission.mu.Lock()
						authorityOwners := mediaSourceAuthorizationAdmission.roots[root].active
						mediaSourceAuthorizationAdmission.mu.Unlock()
						mediaSourceHandoffAdmission.mu.Lock()
						handoffOwners := mediaSourceHandoffAdmission.roots[root].active
						mediaSourceHandoffAdmission.mu.Unlock()
						if checks.Add(1) != 1 || ioOwners != 1 || authorityOwners != 0 || handoffOwners != 1 {
							return errors.New("delivery did not hold only immediate IO and handoff after releasing completed AUTH")
						}
						return nil
					})
				results <- playbackMediaTestResult{file: file, result: result, err: err}
			}()
			mediaSourceAdmissionTestWait(t, trace.entered, "fresh authority transaction before completion")
			mediaSourceWarmPipelineTestCounts(t, root, 0, 1)
			playbackMediaPipelineTestOwnerCounts(t, root, 0, 1)
			if checks.Load() != 0 {
				t.Fatal("delivery or IO admission preceded authorization completion")
			}
			playbackMediaPipelineTestRowLocked(t, ctx, fixture, "SELECT id FROM play_sessions WHERE id=$1 FOR UPDATE NOWAIT", play.ID, true)
			trace.release()
			result := awaitPlaybackMediaTest(t, ctx, results)
			if result.err != nil || result.file == nil || checks.Load() != 1 {
				if result.file != nil {
					_ = result.file.Close()
				}
				t.Fatalf("post-completion immediate IO did not deliver once: checks=%d error=%v", checks.Load(), result.err)
			}
			contents, err := io.ReadAll(result.file)
			_ = result.file.Close()
			if err != nil || string(contents) != fixture.contents {
				t.Fatalf("post-completion immediate IO returned an invalid descriptor: %v", err)
			}
			playbackMediaPipelineTestDrain(t, fixture.store)
			mediaSourceWarmPipelineTestCounts(t, root, 0, 0)
			playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
			playbackMediaPipelineTestProfile(t, fixture.store, profile, 1, 1, 0, 0)
		})
	}
}

func TestMediaSourceWarmPipelineCommitCancellationAvoidsOpenAndDelivery(t *testing.T) {
	for _, application := range []bool{false, true} {
		t.Run(map[bool]string{false: "login", true: "application"}[application], func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, application)
			trace := newMediaSourceWarmCommitTracer()
			fixture = mediaSourceWarmPipelineTestStore(t, fixture, trace, true)
			t.Cleanup(trace.release)
			_, root, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
			ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			caller, cancelCaller := context.WithCancel(ctx)
			defer cancelCaller()
			profile := fixture.store.MediaSourceAdmissionProfile()
			before := mediaSourceWarmPipelineTestFileCount(t, fixture.path)
			var checks atomic.Int32
			results := make(chan playbackMediaTestResult, 1)
			go func() {
				file, result, err := fixture.store.AuthorizePlaybackMediaForChecked(caller, principal, play.ID, fixture.item.ID, play.MediaSourceID, false,
					func(PlaybackMediaAuthorization) error { checks.Add(1); return nil })
				results <- playbackMediaTestResult{file: file, result: result, err: err}
			}()
			mediaSourceAdmissionTestWait(t, trace.entered, "cancelable authority owner at completion")
			mediaSourceWarmPipelineTestCounts(t, root, 0, 1)
			playbackMediaPipelineTestOwnerCounts(t, root, 0, 1)
			cancelCaller()
			result := awaitPlaybackMediaTest(t, ctx, results)
			if result.file != nil || !errors.Is(result.err, context.Canceled) || checks.Load() != 0 {
				t.Fatalf("canceled completion delivered a file or reached policy: checks=%d error=%v", checks.Load(), result.err)
			}
			// Cancellation returns to the receiver while the actual transaction
			// still owns its AUTH and Store lifetime at this controlled boundary.
			mediaSourceWarmPipelineTestCounts(t, root, 0, 1)
			playbackMediaPipelineTestOwnerCounts(t, root, 0, 1)
			trace.release()
			playbackMediaPipelineTestDrain(t, fixture.store)
			mediaSourceWarmPipelineTestCounts(t, root, 0, 0)
			playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
			playbackMediaPipelineTestProfile(t, fixture.store, profile, 1, 0, 0, 0)
			if checks.Load() != 0 || fixture.store.MediaSourceAdmissionProfile()["file_open_ns"] != profile["file_open_ns"] ||
				mediaSourceWarmPipelineTestFileCount(t, fixture.path) != before {
				t.Fatal("failed authorization completion reached source opening or delivery")
			}
		})
	}
}
