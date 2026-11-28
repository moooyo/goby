//go:build linux

package library

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type localBackdropFixture struct {
	ctx                     context.Context
	pool                    *pgxpool.Pool
	store                   *Store
	actor                   identity.Principal
	collection              Library
	item                    Item
	dir, bad, good, otherID string
	data                    []byte
}

func newLocalBackdropFixture(t *testing.T, rootSeries bool) localBackdropFixture {
	t.Helper()
	ctx, pool, store, root, viewer := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	actor := metadataEditTestActor(t, ctx, pool, "backdrop-admin")
	dir := filepath.Join(root, "Physical Show")
	libraryRoot := root
	if rootSeries {
		libraryRoot = dir
	} else {
		// Give the unrelated directory a real image change to publish. A warm
		// scan no longer queues its unchanged NFO just to clear it again.
		libraryIntegrationFile(t, root, "A Other/Season 01/Other.S01E01.mp4", "independent series")
	}
	libraryIntegrationFile(t, dir, "Season 01/Show.S01E01.mp4", "synthetic episode")
	libraryIntegrationFile(t, dir, "tvshow.nfo", `<tvshow><title>Fixture</title><tmdbid>97531</tmdbid></tvshow>`)
	libraryIntegrationFile(t, dir, "Season 01/Show.S01E01.nfo", `<episodedetails><title>Fixture episode</title><season>1</season><episode>1</episode></episodedetails>`)
	for _, name := range []string{"poster.png", "banner.png", "clearlogo.png"} {
		imageScanTestWrite(t, filepath.Join(dir, name), color.White)
	}
	good := filepath.Join(dir, "fanart.jpg")
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 8, 4)), nil); err != nil {
		t.Fatal(err)
	}
	// Keep the initial image on disk under a recognized but different name.
	// The successful reread must replace the old Backdrop[0], not append to it.
	old := filepath.Join(dir, "background.jpg")
	if err := os.WriteFile(old, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	collection, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative, "Backdrop fixture", "tvshows", []string{libraryRoot},
		LibraryOptions{EnableLocalMetadata: true, EnableLocalImages: true, EnableOnlineImages: true})
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if err := os.Rename(old, good); err != nil {
		t.Fatal(err)
	}
	bad := libraryIntegrationFile(t, dir, "backdrop.jpg", "")
	if !rootSeries {
		imageScanTestWrite(t, filepath.Join(root, "A Other", "poster.png"), color.White)
	}
	store.deferLocalMetadata = true
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	var item Item
	if rootSeries {
		if err := pool.QueryRow(ctx, `SELECT id,parent_id FROM items WHERE library_id=$1 AND relative_path=$2`, collection.ID, tvRootSeriesPath).
			Scan(&item.ID, &item.ParentID); err != nil {
			t.Fatal(err)
		}
	} else {
		item = nfoCatalogItem(t, ctx, store, viewer, collection.ID, dir)
	}
	var other string
	if err := pool.QueryRow(ctx, `SELECT item_id FROM local_metadata_ingest WHERE item_id<>$1 ORDER BY item_id LIMIT 1`, item.ID).Scan(&other); err != nil {
		t.Fatal(err)
	}
	// The unchanged NFO stays accepted on the fast rescan, while the damaged
	// backdrop still needs its own import. Do not edit the queue to create it.
	var nfo, images bool
	if err := pool.QueryRow(ctx, `SELECT import_nfo,import_images FROM local_metadata_ingest WHERE item_id=$1`, item.ID).
		Scan(&nfo, &images); err != nil || nfo || !images {
		t.Fatalf("expected an image-only import, got NFO=%t images=%t: %v", nfo, images, err)
	}
	return localBackdropFixture{ctx: ctx, pool: pool, store: store, actor: actor, collection: collection, item: item,
		dir: dir, good: good, bad: bad, otherID: other, data: encoded.Bytes()}
}

func (f localBackdropFixture) exec(t *testing.T, statement string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, statement, args...); err != nil {
		t.Fatal(err)
	}
}

func (f localBackdropFixture) state(t *testing.T, exceptTarget bool) string {
	t.Helper()
	id := ""
	if exceptTarget {
		id = f.item.ID
	}
	var snapshot string
	if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
		'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i),
		'metadata',(SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_metadata_state m),
		'viewer',(SELECT jsonb_agg(to_jsonb(u) ORDER BY user_id,item_id) FROM user_item_data u),
		'images',(SELECT jsonb_agg(to_jsonb(im) ORDER BY item_id,image_type,image_index) FROM item_images im WHERE item_id<>$1 OR image_type<>'Backdrop'),
		'provider',(SELECT jsonb_agg(to_jsonb(p) ORDER BY item_id,image_type,image_index) FROM item_provider_images p),
		'managed',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM artwork_state a),
		'managed_bytes',(SELECT jsonb_agg(to_jsonb(a) ORDER BY state_id,image_type,image_index) FROM artwork_images a),
		'queue',(SELECT jsonb_agg(to_jsonb(q) ORDER BY item_id) FROM local_metadata_ingest q WHERE item_id<>$1),
		'jobs',(SELECT jsonb_agg(to_jsonb(j) ORDER BY id) FROM scan_jobs j),
		'runs',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM task_runs r),
		'triggers',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM task_triggers t),
		'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY name) FROM task_system_events e),
		'activity',(SELECT count(*) FROM activity_entries))::text`, id).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func (f localBackdropFixture) isolateBlocker(t *testing.T) {
	t.Helper()
	if err := os.Rename(f.bad, f.bad+".quarantine"); err != nil {
		t.Fatal(err)
	}
}

func TestLocalBackdropRereadAcceptsOnlyTheTargetAndNotifiesClients(t *testing.T) {
	for _, variant := range []string{"nested-series", "registered-root-series"} {
		t.Run(variant, func(t *testing.T) {
			f := newLocalBackdropFixture(t, variant == "registered-root-series")
			notifications := catalogChangesTestListener(t, f.store)
			before := f.state(t, false)
			if _, err := f.store.RereadLocalBackdrop(f.ctx, f.actor, f.item.ID); !errors.Is(err, ErrLocalBackdropRejected) {
				t.Fatalf("valid fanart plus an empty backdrop did not reject the whole set: %v", err)
			}
			if f.state(t, false) != before {
				t.Fatal("an invalid backdrop changed the catalog or queue")
			}
			assertNoCatalogTestNotification(t, notifications)
			f.isolateBlocker(t)
			// These non-Backdrop overrides must neither be reset nor substituted
			// for the raw local row comparison.
			f.exec(t, `INSERT INTO artwork_state(item_id,managed_types) VALUES($1,ARRAY['Banner'])`, f.item.ID)
			f.exec(t, `INSERT INTO item_provider_images(item_id,image_type,image_index,provider,provider_id,image_id,content,mime_type,width,height,source_hash)
				VALUES($1,'Logo',0,'tmdb','97531','/logo.jpg',$2,'image/jpeg',8,4,repeat('a',64))`, f.item.ID, f.data)
			before = f.state(t, true)
			got, err := f.store.RereadLocalBackdrop(f.ctx, f.actor, f.item.ID)
			if err != nil || got != (LocalBackdropRereadResult{ItemID: f.item.ID, Changed: true}) {
				t.Fatalf("local backdrop reread = %+v, %v", got, err)
			}
			if f.state(t, true) != before {
				t.Fatal("reread changed an unrelated item, type, queue row, task, scan, or system event")
			}
			var pending, sibling, online bool
			if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM local_metadata_ingest WHERE item_id=$1),
				EXISTS(SELECT 1 FROM local_metadata_ingest WHERE item_id=$2), EXISTS(
				SELECT 1 FROM items i JOIN item_metadata_state ms ON ms.item_id=i.id WHERE i.id=$1 AND `+onlineImageCandidateSQL+`)`, f.item.ID, f.otherID).
				Scan(&pending, &sibling, &online); err != nil || pending || !sibling || online {
				t.Fatalf("wrong local or online admission after commit: pending=%v sibling=%v online=%v (%v)", pending, sibling, online, err)
			}
			assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{Kind: CatalogUpdated,
				ItemID: f.item.ID, LibraryID: f.collection.ID, ParentID: f.item.ParentID, IsFolder: true}})
			assertNoCatalogTestNotification(t, notifications)
			body, info, err := f.store.OpenPublicImage(f.ctx, f.item.ID, "Backdrop", 0)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(body)
			if closeErr := body.Close(); err != nil || closeErr != nil || info.Filename != "fanart.jpg" || !bytes.Equal(data, f.data) {
				t.Fatalf("the indexed original could not be served: image=%+v read=%v close=%v", info, err, closeErr)
			}
			if data, err := os.ReadFile(f.bad + ".quarantine"); err != nil || len(data) != 0 {
				t.Fatalf("reread modified the isolated blocker: length=%d err=%v", len(data), err)
			}
			if _, err := f.store.RereadLocalBackdrop(f.ctx, f.actor, f.item.ID); !errors.Is(err, ErrLocalBackdropNotPending) {
				t.Fatalf("a retry manufactured new authority: %v", err)
			}
		})
	}
}

func TestLocalBackdropRereadRetiresIdenticalRowsWithoutNotification(t *testing.T) {
	f := newLocalBackdropFixture(t, false)
	f.isolateBlocker(t)
	if err := os.Rename(f.good, filepath.Join(f.dir, "background.jpg")); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, f.store)
	got, err := f.store.RereadLocalBackdrop(f.ctx, f.actor, f.item.ID)
	if err != nil || got != (LocalBackdropRereadResult{ItemID: f.item.ID, Changed: false}) {
		t.Fatalf("reread of identical rows = %+v, %v", got, err)
	}
	var pending bool
	if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM local_metadata_ingest WHERE item_id=$1)`, f.item.ID).
		Scan(&pending); err != nil || pending {
		t.Fatalf("identical rows did not retire the queue: pending=%v, err=%v", pending, err)
	}
	assertNoCatalogTestNotification(t, notifications)
}

func TestLocalBackdropRereadRejectsCollateralWork(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, localBackdropFixture)
		want   error
	}{
		{"missing backdrop", func(t *testing.T, f localBackdropFixture) {
			if err := os.Remove(f.good); err != nil {
				t.Fatal(err)
			}
		}, ErrLocalBackdropRejected},
		{"invalid otherwise absent art", func(t *testing.T, f localBackdropFixture) {
			libraryIntegrationFile(t, f.dir, "clearart.png", "not an image")
		}, ErrLocalBackdropOtherImages},
		{"changed other image", func(t *testing.T, f localBackdropFixture) {
			imageScanTestWrite(t, filepath.Join(f.dir, "banner.png"), color.Black)
		}, ErrLocalBackdropOtherImages},
		{"different raw file identity", func(t *testing.T, f localBackdropFixture) {
			f.exec(t, `UPDATE item_images SET file_identity='stale' WHERE item_id=$1 AND image_type='Banner'`, f.item.ID)
		}, ErrLocalBackdropOtherImages},
		{"pending NFO", func(t *testing.T, f localBackdropFixture) {
			f.exec(t, `UPDATE local_metadata_ingest SET import_nfo=true WHERE item_id=$1`, f.item.ID)
		}, ErrLocalBackdropNotPending},
		{"pending enrichment", func(t *testing.T, f localBackdropFixture) {
			f.exec(t, `UPDATE local_metadata_ingest SET enrich_online=true WHERE item_id=$1`, f.item.ID)
		}, ErrLocalBackdropNotPending},
		{"stale generation", func(t *testing.T, f localBackdropFixture) {
			f.exec(t, `INSERT INTO scan_jobs(id,library_id,status,finished_at) VALUES('newer-scan',$1,'Completed',clock_timestamp())`, f.collection.ID)
		}, ErrSourceChanged},
		{"stale root binding", func(t *testing.T, f localBackdropFixture) {
			f.exec(t, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE library_id=$1`, f.collection.ID)
		}, ErrSourceChanged},
		{"stale directory identity", func(t *testing.T, f localBackdropFixture) {
			f.exec(t, `UPDATE local_metadata_ingest SET directory_identity='stale' WHERE item_id=$1`, f.item.ID)
		}, ErrSourceChanged},
		{"managed backdrop deletion", func(t *testing.T, f localBackdropFixture) {
			f.exec(t, `INSERT INTO artwork_state(item_id,managed_types) VALUES($1,ARRAY['Backdrop'])`, f.item.ID)
		}, ErrLocalBackdropShadowed},
		{"provider backdrop", func(t *testing.T, f localBackdropFixture) {
			f.exec(t, `INSERT INTO item_provider_images(item_id,image_type,image_index,provider,provider_id,image_id,content,mime_type,width,height,source_hash)
				VALUES($1,'Backdrop',0,'tmdb','97531','/art.jpg',$2,'image/jpeg',8,4,repeat('a',64))`, f.item.ID, f.data)
		}, ErrLocalBackdropShadowed},
		{"online image gap despite locally matching absence", func(t *testing.T, f localBackdropFixture) {
			if err := os.Remove(filepath.Join(f.dir, "clearlogo.png")); err != nil {
				t.Fatal(err)
			}
			f.exec(t, `DELETE FROM item_images WHERE item_id=$1 AND image_type='Logo'`, f.item.ID)
		}, ErrLocalBackdropOnlineGap},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newLocalBackdropFixture(t, false)
			f.isolateBlocker(t)
			tc.mutate(t, f)
			notifications := catalogChangesTestListener(t, f.store)
			before := f.state(t, false)
			if got, err := f.store.RereadLocalBackdrop(f.ctx, f.actor, f.item.ID); !errors.Is(err, tc.want) || got != (LocalBackdropRereadResult{}) {
				t.Fatalf("reread = %+v, %v; want %v", got, err, tc.want)
			}
			if f.state(t, false) != before {
				t.Fatal("a rejected reread changed catalog rows or pending work")
			}
			assertNoCatalogTestNotification(t, notifications)
		})
	}
}

func TestLocalBackdropRereadRechecksAuthorityAndSourceBeforeCommit(t *testing.T) {
	for _, scenario := range []string{"same-size edit", "new candidate", "replaced directory", "new automatic importer", "revoked administrator", "changed options", "new scan generation", "changed root"} {
		t.Run(scenario, func(t *testing.T) {
			f := newLocalBackdropFixture(t, false)
			f.isolateBlocker(t)
			notifications := catalogChangesTestListener(t, f.store)
			want := ErrSourceChanged
			var before string
			got, err := f.store.rereadLocalBackdrop(f.ctx, f.actor, f.item.ID, "", func(ctx context.Context, op *PrimaryRootIO,
				plan seriesNFOAuditRootPlan, target localBackdropTarget) (localBackdropObservation, error) {
				observed, err := f.store.observeLocalBackdrop(ctx, op, plan, target)
				if err != nil {
					return observed, err
				}
				switch scenario {
				case "same-size edit":
					modified := bytes.Clone(f.data)
					modified[len(modified)-1] ^= 1
					if err := rewriteIngestFixture(f.good, modified); err != nil {
						t.Fatal(err)
					}
				case "new candidate":
					imageScanTestWrite(t, filepath.Join(f.dir, "art1.png"), color.White)
				case "replaced directory":
					if err := os.Rename(f.dir, f.dir+"-detached"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(f.dir, 0700); err != nil {
						t.Fatal(err)
					}
				case "new automatic importer":
					startBackdropFixtureImporter(t, f)
					// The worker's first, unrelated import committed before the
					// pause. Distinguish that notification from the reread.
					workerNotice := nextCatalogTestNotification(t, notifications)
					if workerNotice.Resync || len(workerNotice.Changes) != 1 || workerNotice.Changes[0].ItemID == f.item.ID {
						t.Fatalf("expected the automatic import of an unrelated row: %+v", workerNotice)
					}
					assertNoCatalogTestNotification(t, notifications)
					want = ErrBusy
				case "revoked administrator":
					f.exec(t, `DELETE FROM sessions WHERE id=$1`, f.actor.SessionID)
					want = ErrForbidden
				case "changed options":
					f.exec(t, `UPDATE libraries SET options=options||'{"EnableEmbeddedArtwork":true}' WHERE id=$1`, f.collection.ID)
				case "new scan generation":
					f.exec(t, `INSERT INTO scan_jobs(id,library_id,status,finished_at) VALUES('newer-scan',$1,'Completed',clock_timestamp())`, f.collection.ID)
				case "changed root":
					f.exec(t, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE library_id=$1`, f.collection.ID)
				}
				before = f.state(t, false)
				return observed, nil
			})
			if !errors.Is(err, want) || got != (LocalBackdropRereadResult{}) || before == "" {
				t.Fatalf("reread = %+v, %v; want %v after observation", got, err, want)
			}
			if f.state(t, false) != before {
				t.Fatal("reread failed to roll back artwork and queue completion together")
			}
			assertNoCatalogTestNotification(t, notifications)
		})
	}
}

func startBackdropFixtureImporter(t *testing.T, f localBackdropFixture) {
	t.Helper()
	child, fence := metadataIngestTestWorker(t, f.ctx, f.pool, f.collection, TaskLocalArtworkIngestKey)
	ctx, cancel := context.WithCancel(f.ctx)
	entered, done := make(chan struct{}), make(chan struct{})
	var importErr error
	go func() {
		defer close(done)
		importErr = f.store.ImportLocalScanArtwork(ctx, child, f.collection.ID, fence, func(ProviderProgress) error {
			// Hold the real startup importer between two targets, without a
			// catalog transaction. The target's own queue row is still pending.
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
			if !errors.Is(importErr, context.Canceled) {
				t.Errorf("unexpected automatic importer exit: %v", importErr)
			}
		case <-time.After(5 * time.Second):
			t.Error("automatic importer did not retire")
		}
	})
	select {
	case <-entered:
	case <-done:
		t.Fatalf("automatic importer exited before the concurrent boundary: %v", importErr)
	case <-time.After(5 * time.Second):
		t.Fatal("automatic importer did not reach its first progress boundary")
	}
	var pending bool
	if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM local_metadata_ingest WHERE item_id=$1)`, f.item.ID).
		Scan(&pending); err != nil || !pending {
		t.Fatalf("automatic importer already passed the target: pending=%v, err=%v", pending, err)
	}
}
