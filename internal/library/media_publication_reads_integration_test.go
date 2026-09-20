//go:build linux

package library

import (
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func publicationReadTestReservation(t *testing.T, fixture mediaSourceFixture, phase string) string {
	t.Helper()
	id, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	state := "applying"
	if phase == "none" {
		state = "ready"
	}
	_, err = fixture.pool.Exec(fixture.ctx, `INSERT INTO media_operations
		(id,kind,item_id,library_id,root_id,source_item_id,source_library_id,source_root_id,
		 request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,source_revision,
		 stream_index,parameters,source_snapshot,execution_snapshot,state,publication_phase)
		SELECT $2,'remove_embedded_subtitle',i.id,i.library_id,i.root_id,i.id,i.library_id,i.root_id,
		 'publication-reader-actor','publication-reader-credential',$2,decode(repeat('00',32),'hex'),$3,`+MediaOperationSourceRevisionSQL+`,
		 0,'{}','{}','{}',$4,$5 FROM items i WHERE i.id=$1`, fixture.item.ID, id, media.SourceID(fixture.item.ID), state, phase)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPublicationBarrierBlocksNewReadersOnlyForThePublishingItem(t *testing.T) {
	for _, phase := range []string{"prepared", "catalog_committed"} {
		t.Run(phase, func(t *testing.T) {
			fixture, sidecar, _ := subtitleTestCatalog(t)
			owned := ownedSubtitleTestInsert(t, fixture, []byte(subtitleTestSRT))
			otherPath := libraryIntegrationFile(t, fixture.allowedRoot, "movies/Nested/Other.mkv", "video:other-publication-item")
			libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
			other := libraryIntegrationItemByPath(t, libraryIntegrationQuery(t, fixture.ctx, fixture.store,
				Query{UserID: fixture.userID, ParentID: fixture.library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}}).Items, otherPath)
			separatePath := libraryIntegrationFile(t, fixture.allowedRoot, "separate/Other.mkv", "video:other-publication-library")
			separate := libraryIntegrationCreate(t, fixture.ctx, fixture.store, "Separate publication library", "movies", filepath.Dir(separatePath))
			libraryIntegrationScan(t, fixture.ctx, fixture.store, separate.ID, "Completed")
			separateItem := libraryIntegrationItemByPath(t, libraryIntegrationQuery(t, fixture.ctx, fixture.store,
				Query{UserID: fixture.userID, ParentID: separate.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}}).Items, separatePath)
			publicationReadTestReservation(t, fixture, phase)
			if file, _, err := fixture.store.OpenMedia(fixture.ctx, fixture.userID, fixture.item.ID, ""); !errors.Is(err, ErrBusy) || file != nil {
				if file != nil {
					_ = file.Close()
				}
				t.Fatalf("a new playback reader crossed the publication barrier: %v", err)
			}
			if file, _, err := fixture.store.OpenDownload(fixture.ctx, fixture.userID, fixture.item.ID, ""); !errors.Is(err, ErrBusy) || file != nil {
				if file != nil {
					_ = file.Close()
				}
				t.Fatalf("a new download reader crossed the publication barrier: %v", err)
			}
			for _, track := range []Subtitle{sidecar, owned} {
				if content, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); !errors.Is(err, ErrBusy) || len(content.Data) != 0 {
					t.Fatalf("a new subtitle reader crossed the publication barrier: owned=%t err=%v", track.Owned, err)
				}
			}
			actor := metadataEditTestActor(t, fixture.ctx, fixture.pool, "publication-intro-reader")
			if _, err := fixture.store.GetItemIntro(fixture.ctx, actor, fixture.item.ID); !errors.Is(err, ErrBusy) {
				t.Fatalf("intro source inspection crossed the publication barrier: %v", err)
			}
			for _, item := range []Item{other, separateItem} {
				file, _, err := fixture.store.OpenMedia(fixture.ctx, fixture.userID, item.ID, "")
				if err != nil {
					t.Fatalf("unrelated item was blocked by publication: %v", err)
				}
				_ = file.Close()
			}
			libraryIntegrationUser(t, fixture.ctx, fixture.pool, "publication-hidden-user", false, false, nil)
			if _, _, err := fixture.store.OpenMedia(fixture.ctx, "publication-hidden-user", fixture.item.ID, ""); !errors.Is(err, ErrNotFound) {
				t.Fatalf("publication state escaped item authorization: %v", err)
			}
			// The private catalog reader remains available for the executor's CAS.
			tx, access, err := fixture.store.beginSubjectRead(fixture.ctx, Subject{UserID: fixture.userID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := readIndexedMediaSource(fixture.ctx, tx, access, fixture.item.ID, ""); err != nil {
				rollback(tx)
				t.Fatalf("publication executor's internal source reader was blocked: %v", err)
			}
			rollback(tx)
		})
	}
}

func TestPublicationBarrierRechecksAfterTheAuthorizedSnapshot(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	snapshot, err := fixture.store.readMediaSource(fixture.ctx, fixture.userID, fixture.item.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	existing, err := fixture.store.openPublicMediaSource(fixture.ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer existing.Close()
	opID := publicationReadTestReservation(t, fixture, "prepared")
	// The source bytes have not changed: only the fresh publication observation
	// can reject an old authorized snapshot that tries to open another reader.
	if file, err := fixture.store.openPublicMediaSource(fixture.ctx, snapshot); !errors.Is(err, ErrBusy) || file != nil {
		if file != nil {
			_ = file.Close()
		}
		t.Fatalf("snapshot captured before publication bypassed the post-open barrier: %v", err)
	}
	if data, err := io.ReadAll(existing); err != nil || string(data) != fixture.contents {
		t.Fatalf("library barrier unexpectedly modified an already-open original descriptor: %v", err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE media_operations SET state='completed',publication_phase='done' WHERE id=$1`, opID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET media=jsonb_set(media,'{DurationTicks}',to_jsonb((media->>'DurationTicks')::bigint+1)) WHERE id=$1`, fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	if file, err := fixture.store.openPublicMediaSource(fixture.ctx, snapshot); !errors.Is(err, ErrSourceChanged) || file != nil {
		if file != nil {
			_ = file.Close()
		}
		t.Fatalf("completed publication with a changed catalog revived an old source snapshot: %v", err)
	}
	current, _, err := fixture.store.OpenMedia(fixture.ctx, fixture.userID, fixture.item.ID, "")
	if err != nil {
		t.Fatalf("fresh source did not reopen after publication completion: %v", err)
	}
	_ = current.Close()
}

func TestReadyMediaEditDoesNotReserveItsSource(t *testing.T) {
	fixture, sidecar, _ := subtitleTestCatalog(t)
	publicationReadTestReservation(t, fixture, "none")
	file, _, err := fixture.store.OpenMedia(fixture.ctx, fixture.userID, fixture.item.ID, "")
	if err != nil {
		t.Fatalf("an unapplied media edit blocked ordinary playback: %v", err)
	}
	_ = file.Close()
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), sidecar.Index); err != nil {
		t.Fatalf("an unapplied media edit blocked subtitles: %v", err)
	}
}
