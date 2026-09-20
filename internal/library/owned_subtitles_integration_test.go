//go:build linux

package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func ownedSubtitleManagementActor(t *testing.T, fixture mediaSourceFixture, name string) identity.Principal {
	t.Helper()
	actor := metadataEditTestActor(t, fixture.ctx, fixture.pool, name)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET policy=jsonb_set(policy,'{EnableSubtitleManagement}','true'::jsonb,true) WHERE id=$1`, actor.User.ID); err != nil {
		t.Fatal(err)
	}
	return actor
}

func ownedSubtitleTestInsert(t *testing.T, fixture mediaSourceFixture, data []byte) Subtitle {
	t.Helper()
	tx, err := fixture.store.beginOwnedTx(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	var rootID, revision string
	if err := tx.QueryRow(fixture.ctx, `SELECT i.root_id,`+MediaOperationSourceRevisionSQL+` FROM items i WHERE i.id=$1 FOR UPDATE OF i`, fixture.item.ID).
		Scan(&rootID, &revision); err != nil {
		t.Fatal(err)
	}
	_, highest, _, err := subtitleCatalogCapacity(fixture.ctx, tx, fixture.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	item, err := readIndexedMediaSource(fixture.ctx, tx, unrestrictedLibraryAccess(), fixture.item.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if embedded := highestEmbeddedStreamIndex(item.mediaFile.Item.Media); embedded > highest {
		highest = embedded
	}
	digest := sha256.Sum256(data)
	if _, err := tx.Exec(fixture.ctx, `INSERT INTO item_owned_subtitles
		(item_id,root_id,stream_index,source_revision,codec,language,title,is_forced,is_hearing_impaired,content,content_sha256)
		VALUES($1,$2,$3,$4,'srt','eng','Reviewed captions',true,true,$5,$6)`, fixture.item.ID, rootID, highest+1, revision, data, hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	for _, track := range subtitleTestTracks(t, fixture) {
		if track.Index == highest+1 {
			return track
		}
	}
	t.Fatal("published owned fixture is missing from item subtitles")
	return Subtitle{}
}

func TestOwnedSubtitlesShareStableIndexesWithSidecars(t *testing.T) {
	fixture, sidecar, _ := subtitleTestCatalog(t)
	owned := ownedSubtitleTestInsert(t, fixture, []byte(subtitleTestSRT))
	if owned.Index <= sidecar.Index || !owned.Owned || owned.Filename != "" || !owned.IsForced || !owned.IsHearingImpaired {
		t.Fatalf("owned stream projection is not independent from the sidecar namespace: %+v", owned)
	}
	content, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), owned.Index)
	if err != nil || !bytes.Equal(content.Data, []byte(subtitleTestSRT)) || !reflect.DeepEqual(content.Info, owned) {
		t.Fatalf("owned read differs from the applied source: err=%v", err)
	}
	actor := ownedSubtitleManagementActor(t, fixture, "owned-subtitle-editor")
	if err := fixture.store.DeleteSubtitleAsUser(fixture.ctx, actor, fixture.item.ID, owned.Index); err != nil {
		t.Fatal(err)
	}
	var active bool
	var retired bool
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT active,retired_at IS NOT NULL FROM item_owned_subtitles WHERE item_id=$1 AND stream_index=$2`,
		fixture.item.ID, owned.Index).Scan(&active, &retired); err != nil || active || !retired {
		t.Fatalf("owned deletion lost its index tombstone: active=%t retired=%t err=%v", active, retired, err)
	}
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), owned.Index); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retired OCR URL remained readable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(fixture.path), "Feature.zh.srt"), []byte(subtitleTestSRT), 0600); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	tracks := subtitleTestTracks(t, fixture)
	if len(tracks) != 2 || tracks[0].Index != sidecar.Index || tracks[1].Index <= owned.Index || tracks[1].Owned {
		t.Fatalf("rescan reused an owned subtitle identity: %+v", tracks)
	}
	second := ownedSubtitleTestInsert(t, fixture, []byte(subtitleTestSRT))
	if second.Index <= tracks[1].Index {
		t.Fatal("a new owned track reused the sidecar's index")
	}
	data, err := os.ReadFile(fixture.path)
	if err != nil || string(data) != fixture.contents {
		t.Fatal("owned deletion or subtitle rescan changed the primary media")
	}
}

func TestOwnedSubtitleSourceReplacementInvalidatesWithoutLosingHistory(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	owned := ownedSubtitleTestInsert(t, fixture, []byte(subtitleTestSRT))
	if err := os.WriteFile(fixture.path, []byte("video:replacement-owned-subtitle-source"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), owned.Index); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("unscanned media replacement reused OCR bytes: %v", err)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	if tracks := subtitleTestTracks(t, fixture); len(tracks) != 0 {
		t.Fatalf("a new source inherited its old OCR captions: %+v", tracks)
	}
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), owned.Index); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rescanned replacement revived the OCR stream URL: %v", err)
	}
	var preserved []byte
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT content FROM item_owned_subtitles WHERE item_id=$1 AND stream_index=$2`,
		fixture.item.ID, owned.Index).Scan(&preserved); err != nil || !bytes.Equal(preserved, []byte(subtitleTestSRT)) {
		t.Fatalf("source replacement destroyed preserved derivative bytes: %v", err)
	}
	next := ownedSubtitleTestInsert(t, fixture, []byte(subtitleTestSRT))
	if next.Index <= owned.Index {
		t.Fatal("a source replacement reused the old derivative stream index")
	}
}

func TestOwnedSubtitleAuthorizationAndHashAreCheckedBeforeDelivery(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	owned := ownedSubtitleTestInsert(t, fixture, []byte(subtitleTestSRT))
	libraryIntegrationUser(t, fixture.ctx, fixture.pool, "owned-reader", false, false, []string{fixture.library.ID})
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, "owned-reader", fixture.item.ID, media.SourceID(fixture.item.ID), owned.Index); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}' WHERE id='owned-reader'`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, "owned-reader", fixture.item.ID, media.SourceID(fixture.item.ID), owned.Index); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked library access remained valid for owned captions: %v", err)
	}
	changed := []byte(subtitleTestSRT)
	changed[len(changed)-2] = 'X'
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE item_owned_subtitles SET content=$3 WHERE item_id=$1 AND stream_index=$2`,
		fixture.item.ID, owned.Index, changed); err != nil {
		t.Fatal(err)
	}
	if content, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), owned.Index); !errors.Is(err, ErrUnavailable) || len(content.Data) != 0 {
		t.Fatalf("a stored hash mismatch delivered subtitle bytes: %v", err)
	}
}

func TestOwnedSubtitlesInvalidateRememberedSelectionsAndParticipateInQueries(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	var before, applied, retired string
	stamp := func(destination *string) {
		t.Helper()
		if err := fixture.pool.QueryRow(fixture.ctx, `SELECT `+playbackSelectionStampSQL+` FROM items i WHERE i.id=$1`, fixture.item.ID).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	stamp(&before)
	owned := ownedSubtitleTestInsert(t, fixture, []byte(subtitleTestSRT))
	stamp(&applied)
	if applied == before {
		t.Fatal("applied owned captions did not invalidate remembered stream selection")
	}
	has := true
	result, err := fixture.store.QueryItems(fixture.ctx, Query{UserID: fixture.userID, ParentID: fixture.library.ID,
		Recursive: true, IncludeItemTypes: []string{"Movie"}, HasSubtitles: &has})
	if err != nil || len(result.Items) != 1 || result.Items[0].ID != fixture.item.ID {
		t.Fatalf("subtitle filter ignored the owned track: count=%d err=%v", len(result.Items), err)
	}
	actor := ownedSubtitleManagementActor(t, fixture, "owned-query-editor")
	if err := fixture.store.DeleteSubtitleAsUser(fixture.ctx, actor, fixture.item.ID, owned.Index); err != nil {
		t.Fatal(err)
	}
	stamp(&retired)
	if retired != before || retired == applied {
		t.Fatal("retirement did not remove the owned content from the selection stamp")
	}
}
