//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

type creditsFixtureProber struct{}

// The synchronous delegate finishes all source access before returning.
func (creditsFixtureProber) ProbeFileJoinedContract() bool { return true }

func (creditsFixtureProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := (&libraryFixtureProber{}).ProbeFile(ctx, file)
	info.Chapters = []media.Chapter{
		{StartTicks: 0, Title: "IntroStart"},
		{StartTicks: 10 * media.TicksPerSecond, Title: "IntroEnd"},
		{StartTicks: info.DurationTicks * 9 / 10, Title: "CreditsStart"},
	}
	return info, err
}

func TestStoreCreditsLifecycle(t *testing.T) {
	prober := mediaSourceTestProber{inner: creditsFixtureProber{}}
	ctx, pool, store, root, viewerID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "movies/Credits.mp4", "credits-source")
	collection := libraryIntegrationCreate(t, ctx, store, "Credits movies", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var itemID string
	if err := pool.QueryRow(ctx, "SELECT id FROM items WHERE path=$1", path).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	actor := metadataEditTestActor(t, ctx, pool, "credits-administrator")
	detail, err := store.GetItemCredits(ctx, actor, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Revision != "0" || detail.Override != nil || detail.OverrideStale || detail.Effective == nil || detail.Effective.Provenance != "Chapter" || detail.MediaSourceID != media.SourceID(itemID) {
		t.Fatalf("initial credits: %+v", detail)
	}
	intro, err := store.GetItemIntro(ctx, actor, itemID)
	if err != nil {
		t.Fatal(err)
	}
	intro, err = store.UpdateItemIntro(ctx, actor, itemID, IntroEdit{Revision: intro.Revision, SourceRevision: intro.SourceRevision, StartTicks: 0, EndTicks: 5 * media.TicksPerSecond, Provenance: "Manual"}, false)
	if err != nil {
		t.Fatal(err)
	}
	edit := CreditsEdit{Revision: detail.Revision, SourceRevision: detail.SourceRevision, StartTicks: detail.DurationTicks * 4 / 5, Provenance: "Manual"}
	detail, err = store.UpdateItemCredits(ctx, actor, itemID, edit, false)
	if err != nil || detail.Revision != "1" || detail.Effective == nil || detail.Effective.StartTicks != edit.StartTicks || detail.LastEditedBy != actor.User.ID || detail.LastEditedAt == nil {
		t.Fatalf("manual credits: %+v, %v", detail, err)
	}
	if _, err := store.UpdateItemCredits(ctx, actor, itemID, edit, false); !errors.Is(err, ErrCreditsRevisionConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	unchangedIntro, err := store.GetItemIntro(ctx, actor, itemID)
	if err != nil || !reflect.DeepEqual(unchangedIntro, intro) {
		t.Fatalf("credits changed the independent intro state: %+v, %v", unchangedIntro, err)
	}
	item, err := store.GetItemFor(ctx, Subject{UserID: viewerID}, itemID)
	if err != nil || !reflect.DeepEqual(item.Credits, detail.Effective) {
		t.Fatalf("item projection: %+v, %v", item.Credits, err)
	}
	file, source, err := store.OpenMediaFor(ctx, Subject{UserID: viewerID}, itemID, detail.MediaSourceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source.Item.Credits, detail.Effective) {
		t.Fatalf("opened source projection: %+v", source.Item.Credits)
	}

	// Refuse a changed file even before the scanner replaces the indexed facts.
	if err := os.WriteFile(path, []byte("replacement-credits-source"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetItemCredits(ctx, actor, itemID); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("unscanned replacement read: %v", err)
	}
	currentEdit := edit
	currentEdit.Revision = detail.Revision
	if _, err := store.UpdateItemCredits(ctx, actor, itemID, currentEdit, false); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("unscanned replacement edit: %v", err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	changed, err := store.GetItemCredits(ctx, actor, itemID)
	if err != nil || !changed.OverrideStale || changed.Override == nil || changed.SourceRevision == detail.SourceRevision || changed.Effective == nil || changed.Effective.Provenance != "Chapter" {
		t.Fatalf("replaced source: %+v, %v", changed, err)
	}
	item, err = store.GetItemFor(ctx, Subject{UserID: viewerID}, itemID)
	if err != nil || !reflect.DeepEqual(item.Credits, changed.Effective) {
		t.Fatalf("stale override remained public: %+v, %v", item.Credits, err)
	}
	if _, err := store.UpdateItemCredits(ctx, actor, itemID, currentEdit, false); !errors.Is(err, ErrCreditsRevisionConflict) {
		t.Fatalf("old source revision: %v", err)
	}
	currentEdit.SourceRevision, currentEdit.Provenance = changed.SourceRevision, "Import"
	changed, err = store.UpdateItemCredits(ctx, actor, itemID, currentEdit, false)
	if err != nil || changed.Revision != "2" || changed.Effective == nil || changed.Effective.Provenance != "Import" || changed.OverrideStale {
		t.Fatalf("imported credits: %+v, %v", changed, err)
	}
	reset := CreditsEdit{Revision: changed.Revision, SourceRevision: changed.SourceRevision}
	changed, err = store.UpdateItemCredits(ctx, actor, itemID, reset, true)
	if err != nil || changed.Revision != "3" || changed.Override != nil || changed.OverrideStale || changed.Effective == nil || changed.Effective.Provenance != "Chapter" {
		t.Fatalf("reset: %+v, %v", changed, err)
	}
	if _, err := store.UpdateItemCredits(ctx, actor, itemID, currentEdit, false); !errors.Is(err, ErrCreditsRevisionConflict) {
		t.Fatalf("reset revived an old edit: %v", err)
	}
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(pool, prober, []string{root})
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
	restored, err := reopened.GetItemCredits(ctx, actor, itemID)
	if err != nil || !reflect.DeepEqual(restored, changed) {
		t.Fatalf("persisted reset: %+v, %v", restored, err)
	}
	var effects int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM user_item_data) + (SELECT count(*) FROM play_sessions) + (SELECT count(*) FROM encoding_jobs)`).Scan(&effects); err != nil {
		t.Fatal(err)
	}
	if effects != 0 {
		t.Fatal("credits administration or projection created playback state")
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.GetItemCredits(ctx, actor, itemID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked administrator read: %v", err)
	}
	reset.Revision = restored.Revision
	if _, err := reopened.UpdateItemCredits(ctx, actor, itemID, reset, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked administrator write: %v", err)
	}
}

func TestStoreCreditsConcurrentCompareAndSwap(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, mediaSourceTestProber{inner: &libraryFixtureProber{}})
	path := libraryIntegrationFile(t, root, "movies/Credits CAS.mp4", "credits-source")
	collection := libraryIntegrationCreate(t, ctx, store, "Credits CAS", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var itemID string
	if err := pool.QueryRow(ctx, "SELECT id FROM items WHERE path=$1", path).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	actor := metadataEditTestActor(t, ctx, pool, "credits-cas-administrator")
	detail, err := store.GetItemCredits(ctx, actor, itemID)
	if err != nil || detail.Effective != nil {
		t.Fatalf("ordinary chapters inferred credits: %+v, %v", detail, err)
	}
	initial := CreditsEdit{Revision: detail.Revision, SourceRevision: detail.SourceRevision, StartTicks: 0, Provenance: "Manual"}
	var group sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := store.UpdateItemCredits(ctx, actor, itemID, initial, false)
			results <- err
		}()
	}
	group.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrCreditsRevisionConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("CAS outcomes: %d success, %d conflict", success, conflict)
	}
	detail, err = store.GetItemCredits(ctx, actor, itemID)
	if err != nil || detail.Revision != "1" || detail.Effective == nil || detail.Effective.StartTicks != 0 {
		t.Fatalf("CAS state: %+v, %v", detail, err)
	}
	detail, err = store.UpdateItemCredits(ctx, actor, itemID, CreditsEdit{Revision: detail.Revision, SourceRevision: detail.SourceRevision}, true)
	if err != nil || detail.Revision != "2" || detail.Effective != nil || detail.Override != nil {
		t.Fatalf("reset without automatic marker: %+v, %v", detail, err)
	}
	if _, err := store.UpdateItemCredits(ctx, actor, itemID, initial, false); !errors.Is(err, ErrCreditsRevisionConflict) {
		t.Fatalf("reset lost the CAS tombstone: %v", err)
	}
}
