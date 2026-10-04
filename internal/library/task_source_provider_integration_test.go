//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/moooyo/goby/internal/providers"
)

func taskSourceProviderFixture(t *testing.T) (analysisWorkFixture, string, context.Context, func() error) {
	t.Helper()
	fixture := newAnalysisWorkFixture(t, 1)
	runID, children := fixture.admit(t, TaskPreviewGenerationKey, fixture.ids)
	fixture.claim(t, runID, children[0])
	ctx, release, err := fixture.store.BeginTaskSourceOperation(fixture.ctx, children[0], fixture.fence(children[0]))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	})
	return fixture, children[0], ctx, release
}

func TestTaskSourceProviderApprovalChangesApplyToNextOperation(t *testing.T) {
	fixture, childID, ctx, _ := taskSourceProviderFixture(t)
	snapshot, err := fixture.store.readProviderSubtitleSnapshot(ctx, nil, fixture.ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1`, snapshot.primary.root.id); err != nil {
		t.Fatal(err)
	}
	fixture.store.mu.Lock()
	configured := fixture.store.roots
	fixture.store.roots = nil
	fixture.store.mu.Unlock()
	defer func() {
		fixture.store.mu.Lock()
		fixture.store.roots = configured
		fixture.store.mu.Unlock()
	}()
	current, err := fixture.store.readProviderSubtitleSnapshot(ctx, nil, fixture.ids[0])
	if err != nil || !snapshot.same(current) {
		t.Fatalf("active operation changed its source approval: %v", err)
	}
	download := providers.SubtitleDownload{Provider: "opensubtitles", RemoteID: "task-source:1", Language: "en", Format: "srt", Data: []byte(subtitleTestSRT)}
	if err := fixture.store.registerDownloadedSubtitleForSource(ctx, nil, fixture.ids[0], mediaSnapshotTag(snapshot.primary), download); err != nil {
		t.Fatalf("active operation did not retain its authorized roots: %v", err)
	}
	var count int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM item_subtitle_provider_sources WHERE item_id=$1 AND provider_id=$2`, fixture.ids[0], download.RemoteID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("provider publication missing: count=%d error=%v", count, err)
	}
	if _, release, err := fixture.store.BeginTaskSourceOperation(fixture.ctx, childID, fixture.fence(childID)); err == nil {
		_ = release()
		t.Fatal("new operation inherited removed configured roots")
	}
	if _, err := fixture.store.CheckWritableSubtitleTarget(fixture.ctx, fixture.actor, fixture.ids[0], ""); err == nil {
		t.Fatal("external subtitle operation inherited internal approval")
	}
}

func TestTaskSourceProviderRejectsSourceChangeAndCancellation(t *testing.T) {
	fixture, _, ctx, release := taskSourceProviderFixture(t)
	snapshot, err := fixture.store.readProviderSubtitleSnapshot(ctx, nil, fixture.ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET file_identity=file_identity||'-changed' WHERE id=$1`, fixture.ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.checkProviderSubtitleSnapshot(ctx, nil, snapshot); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("changed physical source survived operation approval: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := fixture.store.readProviderSubtitleSnapshot(cancelled, nil, fixture.ids[0]); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled operation read a source: %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.readProviderSubtitleSnapshot(ctx, nil, fixture.ids[0]); err == nil {
		t.Fatal("closed operation remained usable")
	}
}

func TestTaskSourceOperationRetainsLiveRootAndRejectsReplacement(t *testing.T) {
	fixture, _, ctx, release := taskSourceProviderFixture(t)
	snapshot, err := fixture.store.readProviderSubtitleSnapshot(ctx, nil, fixture.ids[0])
	if err != nil {
		t.Fatal(err)
	}
	grant := taskSourceGrant(ctx)
	held := grant.registered[snapshot.primary.root.id].approved
	if _, err := held.Stat("."); err != nil {
		t.Fatalf("operation did not retain its registered root: %v", err)
	}
	rootPath := snapshot.primary.root.path
	renamed := filepath.Join(filepath.Dir(rootPath), filepath.Base(rootPath)+"-retired")
	if err := os.Rename(rootPath, renamed); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Remove(rootPath)
		_ = os.Rename(renamed, rootPath)
	}()
	if err := os.Mkdir(rootPath, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.checkWritableSubtitleTarget(ctx, nil, fixture.ids[0], ""); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("replacement registered root survived operation approval: %v", err)
	}
	if _, err := held.Stat("."); err != nil {
		t.Fatalf("replacement retired the operation's original root: %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if _, err := held.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("operation cleanup did not retire its registered root: %v", err)
	}
}

func TestTaskSourceOperationRouteConsumersCannotMutateGrant(t *testing.T) {
	fixture, _, ctx, _ := taskSourceProviderFixture(t)
	hint, err := fixture.store.readMediaSourceRootHint(ctx, fixture.ids[0])
	if err != nil {
		t.Fatal(err)
	}
	expected, err := fixture.store.primaryRootRouteContext(ctx, hint)
	if err != nil || len(expected.route.Roots) != 1 || len(expected.route.Domains) == 0 {
		t.Fatalf("operation omitted its complete route: %+v error=%v", expected, err)
	}
	expected.route.Roots = slices.Clone(expected.route.Roots)
	expected.route.Domains = slices.Clone(expected.route.Domains)
	for _, consumer := range []struct {
		name string
		read func() (primaryRootIORoute, error)
	}{
		{"root_io", func() (primaryRootIORoute, error) {
			return fixture.store.primaryRootRouteContext(ctx, hint)
		}},
		{"metadata", func() (primaryRootIORoute, error) {
			routes, err := fixture.store.sourceMetadataRoutesContext(ctx, hint)
			return routes[hint.root.id], err
		}},
	} {
		t.Run(consumer.name, func(t *testing.T) {
			escaped, err := consumer.read()
			if err != nil {
				t.Fatal(err)
			}
			escaped.route.Roots[0].RootID = "another-root"
			escaped.route.Domains[0] = "another-domain"
			current, err := fixture.store.primaryRootRouteContext(ctx, hint)
			if err != nil || !reflect.DeepEqual(current, expected) {
				t.Fatalf("consumer changed the operation's retained route: %+v error=%v", current, err)
			}
			root, domain, err := fixture.store.mediaSourceRootLaneContext(ctx, hint)
			if err != nil || root.catalog != expected.route.Roots[0].Catalog || root.id != hint.root.id || domain != expected.domain {
				t.Fatalf("consumer changed the retained admission lane: %+v domain=%q error=%v", root, domain, err)
			}
		})
	}
}

func TestTaskSourceOperationStartupFailureReleasesBorrowedRoots(t *testing.T) {
	fixture := newAnalysisWorkFixture(t, 1)
	runID, children := fixture.admit(t, TaskPreviewGenerationKey, fixture.ids)
	fixture.claim(t, runID, children[0])
	borrowedBefore := make(map[*os.Root]int)
	fixture.store.mu.Lock()
	for root, reference := range fixture.store.rootAnchorReferences {
		borrowedBefore[root] = reference.borrowed
	}
	fixture.store.mu.Unlock()
	fence := fixture.fence(children[0])
	rejected := errors.New("startup publication rejected after root acquisition")
	calls := 0
	ctx, release, err := fixture.store.BeginTaskSourceOperation(fixture.ctx, children[0], func(tx OwnedTx) error {
		calls++
		if calls == 3 {
			return rejected
		}
		return fence(tx)
	})
	if release != nil {
		if closeErr := release(); closeErr != nil {
			t.Error(closeErr)
		}
	}
	if !errors.Is(err, rejected) || calls != 3 || ctx != nil || release != nil {
		t.Fatalf("late startup rejection returned an operation: calls=%d context=%v cleanup=%v error=%v", calls, ctx != nil, release != nil, err)
	}
	fixture.store.mu.Lock()
	defer fixture.store.mu.Unlock()
	for root, reference := range fixture.store.rootAnchorReferences {
		before, existed := borrowedBefore[root]
		if reference.borrowed != before || !existed && reference.retired {
			t.Errorf("failed startup retained a root: borrowed=%d previous=%d retired=%v", reference.borrowed, before, reference.retired)
		}
	}
}
