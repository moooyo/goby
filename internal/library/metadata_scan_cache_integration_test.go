package library

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreCachedFileScanDoesNotRewriteManualMetadataOrEntities(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	actor := metadataEditTestActor(t, ctx, pool, "metadata-cache-editor")
	path := libraryIntegrationFile(t, allowedRoot, "television/Show/Season 01/Show.S01E02.mp4", "video:metadata-cache")
	libraryIntegrationFile(t, allowedRoot, "television/Show/Season 01/Show.S01E02.nfo",
		`<episodedetails><title>Automatic episode</title><plot>Source overview.</plot><episode>2</episode></episodedetails>`)
	library := libraryIntegrationCreate(t, ctx, store, "Cached metadata", "tvshows", filepath.Join(allowedRoot, "television"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	detail := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	detail = metadataEditTestUpdate(t, ctx, store, actor, detail, map[string]json.RawMessage{
		"Name": json.RawMessage(`"Manual episode"`), "IndexNumber": json.RawMessage(`9`),
		"Genres": json.RawMessage(`["Manual genre"]`),
		"People": json.RawMessage(`[{"Name":"Manual actor","Type":"Actor"}]`),
	}, []string{"IndexNumber"})
	before := metadataEditTestSnapshot(t, ctx, pool, item.ID)
	// The fixture's folders have no entities. Any association write during the
	// next visits would therefore be an unnecessary rewrite of this file.
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_cached_metadata_entity_write() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'Unchanged file metadata rewrote entity associations'; END;
		$$;
		CREATE TRIGGER reject_cached_metadata_entity_write BEFORE INSERT OR UPDATE OR DELETE ON item_entities
		FOR EACH ROW EXECUTE FUNCTION reject_cached_metadata_entity_write()`); err != nil {
		t.Fatalf("protect cached metadata entity associations: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, `DROP TRIGGER IF EXISTS reject_cached_metadata_entity_write ON item_entities;
			DROP FUNCTION IF EXISTS reject_cached_metadata_entity_write()`); err != nil {
			t.Errorf("remove cached metadata write protection: %v", err)
		}
	})
	for visit := 0; visit < 2; visit++ {
		job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
		if job.Updated != 0 || job.Added != 0 || job.Error != "" {
			t.Fatalf("cached manual metadata was reported as a source update: %+v", job)
		}
		if after := metadataEditTestSnapshot(t, ctx, pool, item.ID); after != before {
			t.Fatal("cached file visit changed item timestamps, metadata state, or entity associations")
		}
		current := metadataEditTestDetail(t, ctx, store, actor, item.ID)
		if current.Revision != detail.Revision || current.Effective.Name != "Manual episode" ||
			current.Effective.IndexNumber == nil || *current.Effective.IndexNumber != 9 {
			t.Fatalf("cached file visit changed the administrator layer: %+v", current)
		}
	}
	if calls := len(prober.calls()); calls != 1 {
		t.Errorf("cached file was probed %d times, want 1", calls)
	}
}
