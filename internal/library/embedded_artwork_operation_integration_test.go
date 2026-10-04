//go:build linux

package library

import (
	"bytes"
	"fmt"
	"image/color"
	"path/filepath"
	"sync"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestScanEmbeddedArtworkOperationApprovalAndSourceFacts(t *testing.T) {
	for _, change := range []string{"binding_revision", "indexed_media"} {
		t.Run(change, func(t *testing.T) {
			picture := embeddedArtworkTestPicture(t, 3, "Front", color.NRGBA{R: 90, G: 140, A: 255})
			prober := &embeddedArtworkFixtureProber{result: media.EmbeddedArtworkResult{
				Version: media.EmbeddedArtworkVersion, Pictures: []media.EmbeddedPicture{picture},
			}}
			ctx, pool, store, approved, userID := libraryIntegrationStore(t, prober)
			path := libraryIntegrationFile(t, approved, "embedded-operation/Track.flac", "audio:operation-artwork")
			collection := libraryIntegrationCreate(t, ctx, store, "Embedded operation authority", "music", filepath.Dir(path))
			type observation struct {
				itemID, rootID, source string
				err                    error
			}
			observed := make(chan observation, 1)
			var once sync.Once
			prober.during = func() {
				once.Do(func() {
					var before observation
					before.err = pool.QueryRow(ctx, `SELECT i.id,i.root_id,`+embeddedArtworkSourceRevisionSQL+`
						FROM items i WHERE i.library_id=$1 AND i.path=$2 AND i.type='Audio'`, collection.ID, path).
						Scan(&before.itemID, &before.rootID, &before.source)
					if before.err == nil {
						statement, id := "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", before.rootID
						if change == "indexed_media" {
							statement, id = `UPDATE items SET media=jsonb_set(media,'{DurationTicks}',to_jsonb(420000000::bigint)) WHERE id=$1`, before.itemID
						}
						updated, err := pool.Exec(ctx, statement, id)
						before.err = err
						if err == nil && updated.RowsAffected() != 1 {
							before.err = fmt.Errorf("extraction mutation affected %d rows", updated.RowsAffected())
						}
					}
					observed <- before
				})
			}
			job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			var before observation
			select {
			case before = <-observed:
			default:
				t.Fatal("scan never reached the actual embedded extraction callback")
			}
			if before.err != nil {
				t.Fatalf("change approval or source facts during extraction: %v", before.err)
			}
			var current string
			if err := pool.QueryRow(ctx, `SELECT `+embeddedArtworkSourceRevisionSQL+` FROM items i WHERE i.id=$1`, before.itemID).Scan(&current); err != nil {
				t.Fatal(err)
			}
			if current == before.source {
				t.Fatal("extraction fixture did not change the external source revision")
			}
			if change == "indexed_media" {
				var entries int
				var duration int64
				if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM item_embedded_artwork WHERE item_id=i.id),
					(i.media->>'DurationTicks')::bigint FROM items i WHERE i.id=$1`, before.itemID).Scan(&entries, &duration); err != nil {
					t.Fatal(err)
				}
				if entries != 0 || duration != 420000000 || job.Error == "" {
					t.Fatalf("changed source facts published artwork or were overwritten: entries=%d duration=%d job=%+v", entries, duration, job)
				}
				return
			}
			var stored, status string
			var content []byte
			if err := pool.QueryRow(ctx, `SELECT source_revision,status,content FROM item_embedded_artwork WHERE item_id=$1`, before.itemID).
				Scan(&stored, &status, &content); err != nil {
				t.Fatal(err)
			}
			if job.Error != "" || status != "ready" || stored != current || stored == before.source || !bytes.Equal(content, picture.Data) {
				t.Fatalf("approval edit blocked extraction or retained an obsolete external stamp: status=%s stored=%s current=%s before=%s job=%+v",
					status, stored, current, before.source, job)
			}
			image := embeddedArtworkAssertOpen(t, ctx, store, userID, before.itemID, picture)
			if image.SourceRevision != current {
				t.Fatalf("external image projection did not use the current complete source stamp: image=%+v current=%s", image, current)
			}
			probes, extractions := prober.counts()
			cached := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			if afterProbes, afterExtractions := prober.counts(); cached.Error != "" || afterProbes != probes || afterExtractions != extractions {
				t.Fatalf("new scan could not reuse the correctly stamped artwork: probes=%d/%d extractions=%d/%d job=%+v",
					afterProbes, probes, afterExtractions, extractions, cached)
			}
		})
	}
}
