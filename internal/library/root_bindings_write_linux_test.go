//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/storagebinding"
)

// Strong-profile failures must fail these tests, not silently skip acceptance.
// The remote runner supplies an owned ext4 GOTMPDIR for all real observations.
func rootBindingWriteLiveCapture(t *testing.T, store *Store, root libraryRoot) rootBindingWriteCapture {
	t.Helper()
	capture, err := store.captureRootBindingWrite(context.Background(), root)
	if err != nil {
		t.Fatalf("capture real named root binding: %v", err)
	}
	t.Cleanup(func() { _ = capture.Close() })
	return capture
}

func TestRootBindingWriteFilesystemRetainsCurrentNamedAnchorAndRejectsReplacement(t *testing.T) {
	parent := rootStorageTestDirectory(t)
	approved, registered := filepath.Join(parent, "approved"), filepath.Join(parent, "approved", "registered")
	if err := os.MkdirAll(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	store := &Store{roots: []approvedRoot{{path: approved}}}
	if err := openApprovedRoot(&store.roots[0]); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.roots[0].root.Close() })
	root := libraryRoot{id: "live-root", libraryID: "live-library", path: registered, allowedPath: approved, relativePath: "registered"}
	original := rootBindingWriteLiveCapture(t, store, root)
	originalSnapshot, err := original.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(approved, filepath.Join(parent, "retained-original")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := original.Revalidate(context.Background()); !errors.Is(err, ErrRootTopologyChanged) {
		t.Fatalf("replaced named anchor passed held validation: %v", err)
	}
	current := rootBindingWriteLiveCapture(t, store, root)
	currentSnapshot, err := current.Snapshot()
	if err != nil || currentSnapshot.Anchor.Equal(originalSnapshot.Anchor) || currentSnapshot.RegisteredRoot.Equal(originalSnapshot.RegisteredRoot) {
		t.Fatalf("current capture inherited the cached original identity: %v", err)
	}
	clone, err := current.CloneApprovedAnchor()
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Close()
	if err := current.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := clone.Open("registered")
	if err != nil {
		t.Fatalf("closing the capture invalidated its successor anchor clone: %v", err)
	}
	defer file.Close()
	observed, err := ObserveRootStorageIdentity(file)
	if err != nil || !observed.Identity.Equal(currentSnapshot.RegisteredRoot) {
		t.Fatalf("successor anchor clone did not retain the captured root: %v", err)
	}
}

type rootBindingWriteLiveGate struct {
	rootBindingWriteCapture
	checks int
	inside func(context.Context) error
}

func (capture *rootBindingWriteLiveGate) Revalidate(ctx context.Context) error {
	capture.checks++
	if capture.checks == 2 && capture.inside != nil {
		if err := capture.inside(ctx); err != nil {
			return err
		}
	}
	return capture.rootBindingWriteCapture.Revalidate(ctx)
}

func TestRootBindingWriteIntegrationReplacementCommitPublishesExactAnchorBeforeScan(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, _, userID := libraryIntegrationStore(t, mediaSourceTestProber{inner: prober})
	parent := rootStorageTestDirectory(t)
	approved := filepath.Join(parent, "approved")
	registered := filepath.Join(approved, "registered")
	originalPath := libraryIntegrationFile(t, registered, "Original.mkv", "original approved storage")
	store.mu.Lock()
	store.roots = append(store.roots, approvedRoot{path: approved})
	store.mu.Unlock()
	actor := metadataEditTestActor(t, ctx, pool, "root-binding-live-writer")
	library := libraryIntegrationCreate(t, ctx, store, "Live binding update", "movies", registered)
	roots, err := store.ListRegisteredRoots(ctx, actor, library.ID)
	if err != nil || len(roots) != 1 {
		t.Fatalf("discover live write root: roots = %+v, error = %v", roots, err)
	}
	root := libraryRoot{id: roots[0].RootID, libraryID: library.ID, path: registered, allowedPath: approved, relativePath: "registered"}
	read, err := store.GetRootBinding(ctx, actor, library.ID, root.id)
	if err != nil || read.Status != RootBindingVerified || read.Revision != "1" || read.ObservedFingerprint == "" ||
		read.ApprovedFingerprint != read.ObservedFingerprint {
		t.Fatalf("observe original live storage: status = %s, error = %v", read.Status, err)
	}
	bound := read
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	oldLease, err := store.leaseLibraryRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer oldLease.Close()
	if err := os.Rename(approved, filepath.Join(parent, "retained-original")); err != nil {
		t.Fatal(err)
	}
	replacementPath := libraryIntegrationFile(t, registered, "Replacement.mkv", "replacement approved storage")
	read, err = store.GetRootBinding(ctx, actor, library.ID, root.id)
	if err != nil || read.Status != RootBindingMismatch || read.ObservedFingerprint == bound.ObservedFingerprint {
		t.Fatalf("observe current replacement independently: status = %s, error = %v", read.Status, err)
	}
	expected, err := store.observeRootBinding(ctx, root)
	if err != nil {
		t.Fatalf("independently capture expected persisted identities: %v", err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	type outcome struct {
		binding RootBindingInfo
		err     error
	}
	updated := make(chan outcome, 1)
	go func() {
		result, err := store.updateRootBinding(ctx, actor, library.ID, root.id, RootBindingUpdate{
			Revision: read.Revision, ObservedFingerprint: read.ObservedFingerprint, AcknowledgeMissingRemoval: true},
			func(ctx context.Context, row libraryRoot) (rootBindingWriteCapture, error) {
				capture, err := store.captureRootBindingWrite(ctx, row)
				if err != nil {
					return nil, err
				}
				return &rootBindingWriteLiveGate{rootBindingWriteCapture: capture, inside: func(ctx context.Context) error {
					close(entered)
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}}, nil
			})
		updated <- outcome{result, err}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("the update did not reach protected topology validation")
	}
	if store.mu.TryLock() {
		store.mu.Unlock()
		t.Fatal("binding update released scan admission before its commit")
	}
	if store.ownership.mu.TryLock() {
		store.ownership.mu.Unlock()
		t.Fatal("topology revalidation ran outside catalog ownership")
	}
	type scanOutcome struct {
		job Job
		err error
	}
	scanEntered, scanned := make(chan struct{}), make(chan scanOutcome, 1)
	go func() {
		close(scanEntered)
		job, err := store.StartScan(ctx, library.ID)
		scanned <- scanOutcome{job, err}
	}()
	<-scanEntered
	select {
	case result := <-scanned:
		t.Fatalf("scan entered before binding commit and anchor installation: %v", result.err)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	result := <-updated
	if result.err != nil || result.binding.Status != RootBindingVerified || result.binding.Revision != "2" ||
		result.binding.ApprovedFingerprint != read.ObservedFingerprint || result.binding.ObservedFingerprint != read.ObservedFingerprint ||
		result.binding.BoundAt == nil || result.binding.BoundBy != actor.User.ID {
		t.Fatalf("replacement binding did not commit exactly: result = %+v, error = %v", result.binding, result.err)
	}
	var document []byte
	if err := pool.QueryRow(ctx, `SELECT storage_binding::text FROM library_roots WHERE id = $1`, root.id).Scan(&document); err != nil {
		t.Fatal(err)
	}
	persisted, err := storagebinding.DecodeSnapshot(document)
	if err != nil || !reflect.DeepEqual(persisted, expected) {
		t.Fatalf("committed storage identity differs from the held named observation: %v", err)
	}
	scan := <-scanned
	if scan.err != nil {
		t.Fatalf("scan admission failed after accepted replacement: %v", scan.err)
	}
	libraryIntegrationWaitJob(t, ctx, store, scan.job.ID, "Completed")
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, IncludeItemTypes: []string{"Movie"}, Recursive: true, Limit: 100})
	replacement := libraryIntegrationItemByPath(t, items.Items, replacementPath)
	media, _, err := store.OpenMedia(ctx, userID, replacement.ID, "")
	if err != nil {
		t.Fatalf("media open did not follow the accepted replacement without restart: %v", err)
	}
	content, readErr := io.ReadAll(media)
	_ = media.Close()
	if readErr != nil || string(content) != "replacement approved storage" {
		t.Fatalf("media open reached a different directory after rebind: %v", readErr)
	}
	heldRoot, err := oldLease.Open()
	if err != nil {
		t.Fatalf("anchor replacement invalidated an existing publication lease: %v", err)
	}
	defer heldRoot.Close()
	if _, err := heldRoot.Stat(filepath.Base(originalPath)); err != nil {
		t.Fatalf("the retained old publication lease lost its original storage: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "retained-original", "registered", filepath.Base(originalPath))); err != nil {
		t.Fatalf("rebind deleted an original media file: %v", err)
	}
}

func TestRootBindingWriteIntegrationTopologyChangeInsideTransactionRollsBack(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	parent := rootStorageTestDirectory(t)
	registered := filepath.Join(parent, "registered")
	if err := os.Mkdir(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.roots = append(store.roots, approvedRoot{path: parent})
	store.mu.Unlock()
	actor := metadataEditTestActor(t, ctx, pool, "root-binding-live-race")
	library := libraryIntegrationCreate(t, ctx, store, "Changing live binding", "movies", registered)
	roots, err := store.ListRegisteredRoots(ctx, actor, library.ID)
	if err != nil || len(roots) != 1 {
		t.Fatalf("discover changing root: roots = %+v, error = %v", roots, err)
	}
	read, err := store.GetRootBinding(ctx, actor, library.ID, roots[0].RootID)
	if err != nil || read.ObservedFingerprint == "" {
		t.Fatalf("observe changing root: %v", err)
	}
	store.mu.Lock()
	anchorsBefore := len(store.rootBindingAnchors)
	anchorBefore, foundAnchorBefore := store.rootBindingAnchors[roots[0].RootID]
	store.mu.Unlock()
	if !foundAnchorBefore || anchorBefore.approved == nil {
		t.Fatal("registered root did not retain its initial anchor")
	}
	before := catalogAuditSnapshot(t, ctx, pool)
	result, err := store.updateRootBinding(ctx, actor, library.ID, roots[0].RootID, RootBindingUpdate{
		Revision: read.Revision, ObservedFingerprint: read.ObservedFingerprint, AcknowledgeMissingRemoval: true},
		func(ctx context.Context, root libraryRoot) (rootBindingWriteCapture, error) {
			capture, err := store.captureRootBindingWrite(ctx, root)
			if err != nil {
				return nil, err
			}
			return &rootBindingWriteLiveGate{rootBindingWriteCapture: capture, inside: func(context.Context) error {
				if err := os.Rename(registered, filepath.Join(parent, "retained-original")); err != nil {
					return err
				}
				return os.Mkdir(registered, 0o700)
			}}, nil
		})
	if !errors.Is(err, ErrRootBindingConflict) || !reflect.DeepEqual(result, RootBindingInfo{}) {
		t.Fatalf("changed real storage survived transaction validation: result = %+v, error = %v", result, err)
	}
	if after := catalogAuditSnapshot(t, ctx, pool); after != before {
		t.Fatal("changed topology committed either its binding or audit event")
	}
	store.mu.Lock()
	anchorsAfter := len(store.rootBindingAnchors)
	anchorAfter, foundAnchorAfter := store.rootBindingAnchors[roots[0].RootID]
	store.mu.Unlock()
	if anchorsAfter != anchorsBefore || !foundAnchorAfter || anchorAfter.approved != anchorBefore.approved || anchorAfter.root != anchorBefore.root {
		t.Fatal("a rejected binding changed the retained anchor or its registered mapping")
	}
	if _, err := anchorBefore.approved.Stat("."); err != nil {
		t.Fatalf("a rejected binding closed the original registered anchor: %v", err)
	}
}
