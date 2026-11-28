//go:build linux

package library

import (
	"context"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestLocalIngestPhasesCompleteAgainstCurrentQueueInEitherOrder(t *testing.T) {
	for _, nfoFirst := range []bool{false, true} {
		name := "artwork first"
		if nfoFirst {
			name = "NFO first"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, store, approved, viewer := libraryIntegrationStore(t, forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
				return forceProbeTestInfo(file)
			}))
			store.deferLocalMetadata = true
			path := libraryIntegrationFile(t, approved, "movies/Film.mp4", "movie fixture")
			libraryIntegrationFile(t, approved, "movies/Film.nfo", `<movie><title>Accepted Title</title><mpaa>PG</mpaa></movie>`)
			imageScanTestWrite(t, filepath.Join(approved, "movies", "Film-poster.png"), color.White)
			lib := libraryIntegrationCreate(t, ctx, store, "Independent local imports", "movies", filepath.Dir(path))
			job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed")
			item := nfoCatalogItem(t, ctx, store, viewer, lib.ID, path)
			nfoChild, nfoFence := metadataIngestTestWorker(t, ctx, pool, lib, TaskLocalMetadataIngestKey)
			imageChild, imageFence := metadataIngestTestWorker(t, ctx, pool, lib, TaskLocalArtworkIngestKey)
			work := func(child string, fence AnalysisFence) (context.Context, *internalTaskSourceGrant) {
				t.Helper()
				work, close, err := store.BeginTaskSourceOperation(ctx, child, fence)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := close(); err != nil {
						t.Error(err)
					}
				})
				return work, taskSourceGrant(work)
			}
			nfoCtx, nfoGrant := work(nfoChild, nfoFence)
			imageCtx, imageGrant := work(imageChild, imageFence)
			readTarget := func() metadataIngestTarget {
				t.Helper()
				target, err := readMetadataIngestTarget(pool.QueryRow(ctx, metadataIngestTargetReadOne, lib.ID, item.ID, job.ID))
				if err != nil || !target.request.nfo || !target.request.images {
					t.Fatalf("expected both phases in the same scan generation: %+v, %v", target.request, err)
				}
				return target
			}
			// Simulate two workers which independently read the same initial page.
			nfoTarget, imageTarget := readTarget(), readTarget()
			var listing metadataIngestImageListing
			nfo := func() {
				t.Helper()
				if _, err := store.importLocalMetadataTarget(nfoCtx, nfoGrant, lib, nfoTarget, localIngestNFO, nil); err != nil {
					t.Fatalf("NFO phase did not accept the current generation: %v", err)
				}
			}
			artwork := func() {
				t.Helper()
				if _, err := store.importLocalMetadataTarget(imageCtx, imageGrant, lib, imageTarget, localIngestArtwork, &listing); err != nil {
					t.Fatalf("artwork phase did not accept the current generation: %v", err)
				}
			}
			assertState := func(wantNFO, wantArtwork bool, title string, images int) {
				t.Helper()
				var nfoPending, imagePending, rowPresent bool
				var gotTitle string
				var gotImages int
				err := pool.QueryRow(ctx, `SELECT COALESCE(q.import_nfo,false),COALESCE(q.import_images,false),q.item_id IS NOT NULL,i.name,
					(SELECT count(*) FROM item_images WHERE item_id=i.id)
					FROM items i LEFT JOIN local_metadata_ingest q ON q.item_id=i.id WHERE i.id=$1`, item.ID).
					Scan(&nfoPending, &imagePending, &rowPresent, &gotTitle, &gotImages)
				if err != nil || nfoPending != wantNFO || imagePending != wantArtwork || rowPresent != (wantNFO || wantArtwork) || gotTitle != title || gotImages != images {
					t.Fatalf("phase completion corrupted the other phase: NFO=%t artwork=%t row=%t title=%q images=%d, %v", nfoPending, imagePending, rowPresent, gotTitle, gotImages, err)
				}
			}
			if nfoFirst {
				nfo()
				assertState(false, true, "Accepted Title", 0)
				artwork()
			} else {
				artwork()
				assertState(true, false, "Film", 1)
				nfo()
			}
			assertState(false, false, "Accepted Title", 1)
			progress := func(ProviderProgress) error { return errors.New("wrong-phase task reached progress") }
			if err := store.ImportLocalScanMetadata(ctx, imageChild, lib.ID, imageFence, progress); err == nil {
				t.Fatal("an artwork child authorized NFO verification")
			}
			if err := store.ImportLocalScanArtwork(ctx, nfoChild, lib.ID, nfoFence, progress); err == nil {
				t.Fatal("an NFO child authorized artwork")
			}
		})
	}
}
