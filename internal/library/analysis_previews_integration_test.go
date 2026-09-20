//go:build linux

package library

import (
	"errors"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func analysisPreviewCatalogFixture(t *testing.T) (mediaSourceFixture, AnalysisPreview) {
	t.Helper()
	fixture := mediaSourceTestCatalog(t, nil)
	value, _ := analysisPreviewTestValue()
	value.ItemID, value.ProfileRevision, value.PublicationEpoch = fixture.item.ID, "1", 1
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT `+introSourceRevisionSQL+` FROM items i WHERE i.id=$1`, fixture.item.ID).Scan(&value.SourceRevision); err != nil {
		t.Fatal(err)
	}
	value.FrameCount = int((fixture.item.Media.DurationTicks-1)/value.IntervalTicks + 1)
	value.Bytes = max(value.Bytes, 72+12*int64(value.FrameCount))
	value.NominalTicks, value.ActualTicks = make([]int64, value.FrameCount), make([]int64, value.FrameCount)
	for index := range value.NominalTicks {
		value.NominalTicks[index] = int64(index) * value.IntervalTicks
		value.ActualTicks[index] = value.NominalTicks[index]
	}
	if err := ValidateStoredAnalysisPreview(value, fixture.item.Media.DurationTicks); err != nil {
		t.Fatal(err)
	}
	timeline, err := EncodeAnalysisPreviewTimeline(value.NominalTicks, value.ActualTicks)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(fixture.ctx, `INSERT INTO analysis_previews
		(item_id,width,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,child_id,
		cache_key,seal,height,content_sha256,bytes,frame_count,interval_ticks,timeline)
		VALUES($1,$2,1,$3,$4,1,1,'preview-read-fixture',$5,$6,$7,$8,$9,$10,$11,$12) RETURNING updated_at`,
		value.ItemID, value.Width, value.SourceRevision, value.ProfileFingerprint, value.CacheKey, value.Seal,
		value.Height, value.SHA256, value.Bytes, value.FrameCount, value.IntervalTicks, timeline).Scan(&value.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	return fixture, value
}

func TestAnalysisPreviewReadsRequireCurrentSourceConfigurationAndEpoch(t *testing.T) {
	fixture, value := analysisPreviewCatalogFixture(t)
	actor := metadataEditTestActor(t, fixture.ctx, fixture.pool, "preview-reference-reader")
	assertVisible := func(want bool) {
		t.Helper()
		read, err := fixture.store.GetAnalysisPreviewsFor(fixture.ctx, Subject{UserID: fixture.userID}, value.ItemID, media.SourceID(value.ItemID))
		if err != nil || read == nil || want && len(read) != 1 || !want && len(read) != 0 || want && !reflect.DeepEqual(read[0], value) {
			t.Fatalf("source-bound preview projection differs: %d want%v: %v", len(read), want, err)
		}
		keys, err := fixture.store.CurrentAnalysisPreviewCacheKeys(fixture.ctx, actor)
		if err != nil || keys == nil || want && len(keys) != 1 || !want && len(keys) != 0 || want && keys[0] != value.CacheKey {
			t.Fatalf("current pruning references differ: %v want%v: %v", keys, want, err)
		}
	}
	assertVisible(true)
	for _, field := range []string{"profile_revision", "publication_epoch"} {
		if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE analysis_previews SET `+field+`=2`); err != nil {
			t.Fatal(err)
		}
		assertVisible(false)
		if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE analysis_previews SET `+field+`=1`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE analysis_previews SET source_revision='stale-source'`); err != nil {
		t.Fatal(err)
	}
	assertVisible(false)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE analysis_previews SET source_revision=$1`, value.SourceRevision); err != nil {
		t.Fatal(err)
	}
	assertVisible(true)
	if _, err := fixture.store.GetAnalysisPreviewsFor(fixture.ctx, Subject{UserID: fixture.userID}, value.ItemID, "unbound-source"); err == nil {
		t.Fatal("an unbound requested media source received preview references")
	}
	libraryIntegrationUser(t, fixture.ctx, fixture.pool, "preview-hidden-reader", false, false, nil)
	if read, err := fixture.store.GetAnalysisPreviewsFor(fixture.ctx, Subject{UserID: "preview-hidden-reader"}, value.ItemID, ""); err == nil || len(read) != 0 {
		t.Fatal("an unauthorized library reader received preview references")
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if keys, err := fixture.store.CurrentAnalysisPreviewCacheKeys(fixture.ctx, actor); !errors.Is(err, ErrForbidden) || keys != nil {
		t.Fatal("revoked administrator obtained a pruning reference list")
	}
}

func TestAnalysisPreviewStorageRejectsSizesWithoutRoomForEveryFrame(t *testing.T) {
	fixture, value := analysisPreviewCatalogFixture(t)
	minimum := int64(72 + 12*value.FrameCount)
	for _, size := range []int64{1, 72, 64 + 8*int64(value.FrameCount+1), minimum - 1} {
		if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE analysis_previews SET bytes=$2 WHERE item_id=$1`, value.ItemID, size); err == nil {
			t.Fatalf("database accepted %d bytes for %d declared frames", size, value.FrameCount)
		}
	}
	// Both independent row bounds hold here; only the cross-field lower bound
	// can reject the claimed tiny BIF with a complete maximum-sized index.
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE analysis_previews SET bytes=72,frame_count=4096,timeline=decode(repeat('00',65536),'hex') WHERE item_id=$1`, value.ItemID); err == nil {
		t.Fatal("database accepted a constant header-only bound for 4096 frames")
	}
	var bytes int64
	var frames int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT bytes,frame_count FROM analysis_previews WHERE item_id=$1`, value.ItemID).Scan(&bytes, &frames); err != nil || bytes != value.Bytes || frames != value.FrameCount {
		t.Fatalf("rejected preview mutation changed stored metadata: %d bytes %d frames: %v", bytes, frames, err)
	}
}

func TestClearAnalysisPreviewsRetainsSourceBytesAndFencesEarlierWork(t *testing.T) {
	fixture, value := analysisPreviewCatalogFixture(t)
	actor := metadataEditTestActor(t, fixture.ctx, fixture.pool, "preview-clear-editor")
	if err := fixture.store.ClearAnalysisPreviews(fixture.ctx, actor, value.ItemID, "stale-source"); !errors.Is(err, ErrAnalysisSourceChanged) {
		t.Fatalf("stale source clear succeeded: %v", err)
	}
	for revision := int64(1); revision <= 2; revision++ {
		if err := fixture.store.ClearAnalysisPreviews(fixture.ctx, actor, value.ItemID, value.SourceRevision); err != nil {
			t.Fatal(err)
		}
		var actual int64
		var count int
		if err := fixture.pool.QueryRow(fixture.ctx, `SELECT revision,(SELECT count(*) FROM analysis_previews WHERE item_id=$1)
			FROM analysis_preview_state WHERE item_id=$1`, value.ItemID).Scan(&actual, &count); err != nil || actual != revision || count != 0 {
			t.Fatalf("clear lost its persistent worker fence: revision%d count%d: %v", actual, count, err)
		}
		if err := fixture.store.WithOwnedTx(fixture.ctx, func(tx OwnedTx) error {
			current, err := readAnalysisSource(tx, value.ItemID, false)
			if err == nil && current.PreviewRevision != strconv.FormatInt(revision, 10) {
				t.Fatal("new source snapshots do not observe the preview clear tombstone")
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if contents, err := os.ReadFile(fixture.path); err != nil || string(contents) != fixture.contents {
		t.Fatalf("preview clear changed the original source: %v", err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.ClearAnalysisPreviews(fixture.ctx, actor, value.ItemID, value.SourceRevision); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked authority cleared preview state: %v", err)
	}
}

func TestClearAnalysisPreviewsFinalAuthorityFailureRollsBackReferencesAndFence(t *testing.T) {
	fixture, value := analysisPreviewCatalogFixture(t)
	actor := metadataEditTestActor(t, fixture.ctx, fixture.pool, "preview-final-authority-editor")
	if _, err := fixture.pool.Exec(fixture.ctx, `CREATE FUNCTION revoke_preview_clear_actor() RETURNS trigger LANGUAGE plpgsql AS $$
	BEGIN UPDATE sessions SET revoked_at=clock_timestamp() WHERE user_id='preview-final-authority-editor'; RETURN NEW; END $$;
	CREATE TRIGGER revoke_preview_clear_actor AFTER INSERT OR UPDATE ON analysis_preview_state
	FOR EACH ROW EXECUTE FUNCTION revoke_preview_clear_actor()`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.ClearAnalysisPreviews(fixture.ctx, actor, value.ItemID, value.SourceRevision); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a late-revoked administrator committed preview clear: %v", err)
	}
	var references, states int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT (SELECT count(*) FROM analysis_previews WHERE item_id=$1),
		(SELECT count(*) FROM analysis_preview_state WHERE item_id=$1)`, value.ItemID).Scan(&references, &states); err != nil || references != 1 || states != 0 {
		t.Fatalf("failed authority check lost references or committed a fence: %d %d: %v", references, states, err)
	}
	if keys, err := fixture.store.CurrentAnalysisPreviewCacheKeys(fixture.ctx, actor); err != nil || !reflect.DeepEqual(keys, []string{value.CacheKey}) {
		t.Fatalf("rolled-back clear changed cache retention authority: %v: %v", keys, err)
	}
}
