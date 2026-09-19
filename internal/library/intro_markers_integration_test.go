package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

type introFixtureProber struct{}

func (introFixtureProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := (&libraryFixtureProber{}).ProbeFile(ctx, file)
	info.Chapters = []media.Chapter{{StartTicks: 0, EndTicks: 10 * media.TicksPerSecond, Title: "IntroStart"},
		{StartTicks: 10 * media.TicksPerSecond, EndTicks: info.DurationTicks, Title: "IntroEnd"}}
	return info, err
}

func TestStoreIntroLifecycle(t *testing.T) {
	ctx, pool, store, root, viewerID := libraryIntegrationStore(t, introFixtureProber{})
	path := libraryIntegrationFile(t, root, "movies/Intro.mp4", "intro-source")
	collection := libraryIntegrationCreate(t, ctx, store, "Intro movies", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var itemID string
	if err := pool.QueryRow(ctx, "SELECT id FROM items WHERE path=$1", path).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	actor := metadataEditTestActor(t, ctx, pool, "intro-administrator")
	detail, err := store.GetItemIntro(ctx, actor, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Revision != "0" || detail.Override != nil || detail.Effective == nil || detail.Effective.Provenance != "Chapter" {
		t.Fatalf("initial state: %+v", detail)
	}
	edit := IntroEdit{Revision: detail.Revision, SourceRevision: detail.SourceRevision, StartTicks: 2 * media.TicksPerSecond, EndTicks: 12 * media.TicksPerSecond, Provenance: "Manual"}
	detail, err = store.UpdateItemIntro(ctx, actor, itemID, edit, false)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Revision != "1" || detail.Effective == nil || detail.Effective.StartTicks != edit.StartTicks || detail.LastEditedBy != actor.User.ID {
		t.Fatalf("edited state: %+v", detail)
	}
	if _, err := store.UpdateItemIntro(ctx, actor, itemID, edit, false); !errors.Is(err, ErrIntroRevisionConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	intro, err := store.GetIntroFor(ctx, Subject{UserID: viewerID}, itemID, media.SourceID(itemID))
	if err != nil || intro == nil || intro.StartTicks != edit.StartTicks {
		t.Fatalf("playback intro: %+v, %v", intro, err)
	}
	if _, err := store.GetIntroFor(ctx, Subject{UserID: viewerID}, itemID, "another-source"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong source identity: %v", err)
	}
	// A source replacement is rejected even before the scanner updates its facts.
	if err := os.WriteFile(path, []byte("replacement-intro-source"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetIntroFor(ctx, Subject{UserID: viewerID}, itemID, ""); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("unscanned replacement: %v", err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	changed, err := store.GetItemIntro(ctx, actor, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if !changed.OverrideStale || changed.Override == nil || changed.SourceRevision == detail.SourceRevision || changed.Effective == nil || changed.Effective.Provenance != "Chapter" {
		t.Fatalf("replaced source: %+v", changed)
	}
	edit.Revision = changed.Revision
	if _, err := store.UpdateItemIntro(ctx, actor, itemID, edit, false); !errors.Is(err, ErrIntroRevisionConflict) {
		t.Fatalf("old source edit: %v", err)
	}
	edit.SourceRevision, edit.Provenance = changed.SourceRevision, "Import"
	changed, err = store.UpdateItemIntro(ctx, actor, itemID, edit, false)
	if err != nil || changed.Effective == nil || changed.Effective.Provenance != "Import" {
		t.Fatalf("import: %+v, %v", changed, err)
	}
	reset := IntroEdit{Revision: changed.Revision, SourceRevision: changed.SourceRevision}
	changed, err = store.UpdateItemIntro(ctx, actor, itemID, reset, true)
	if err != nil || changed.Override != nil || changed.Effective == nil || changed.Effective.Provenance != "Chapter" || changed.Revision != "3" {
		t.Fatalf("reset: %+v, %v", changed, err)
	}
	// Restart reads the same tombstone and source facts without replaying edits.
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(pool, introFixtureProber{}, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := reopened.Close(closeCtx); err != nil {
			t.Error(err)
		}
	}()
	restored, err := reopened.GetItemIntro(ctx, actor, itemID)
	if err != nil || restored.Revision != changed.Revision || restored.SourceRevision != changed.SourceRevision || restored.Override != nil {
		t.Fatalf("restart state: %+v, %v", restored, err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET policy='{\"EnableMediaPlayback\":false}' WHERE id=$1", viewerID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.GetIntroFor(ctx, Subject{UserID: viewerID}, itemID, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked playback authority: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.GetItemIntro(ctx, actor, itemID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked administrator: %v", err)
	}
}

func TestStoreIntroConcurrentCompareAndSwap(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	path := libraryIntegrationFile(t, root, "movies/CAS.mp4", "intro-source")
	collection := libraryIntegrationCreate(t, ctx, store, "Intro CAS", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var itemID string
	if err := pool.QueryRow(ctx, "SELECT id FROM items WHERE path=$1", path).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	actor := metadataEditTestActor(t, ctx, pool, "intro-cas-administrator")
	detail, err := store.GetItemIntro(ctx, actor, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Effective != nil {
		t.Fatal("ordinary opening chapter inferred an intro")
	}
	edit := IntroEdit{Revision: detail.Revision, SourceRevision: detail.SourceRevision, StartTicks: 0, EndTicks: media.TicksPerSecond, Provenance: "Manual"}
	var group sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := store.UpdateItemIntro(ctx, actor, itemID, edit, false)
			results <- err
		}()
	}
	group.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrIntroRevisionConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("CAS outcomes: %d success, %d conflict", success, conflict)
	}
}
