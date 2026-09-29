//go:build linux

package library

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func TestLibraryIntroOptInRetiresOnlyItsLegacyRejections(t *testing.T) {
	f := newAnalysisAdminFixture(t)
	f.seedReview(t)
	for index, provenance := range []string{"Manual", "Import"} {
		item := f.item(t, index)
		if _, err := f.store.UpdateItemIntro(f.ctx, f.actor, item.ID, IntroEdit{
			Revision: item.Detection.ManualRevision, SourceRevision: item.SourceRevision,
			StartTicks: media.TicksPerSecond, EndTicks: 5 * media.TicksPerSecond, Provenance: provenance}, false); err != nil {
			t.Fatal(err)
		}
	}
	item := f.item(t, 0)
	rejected, err := f.store.DecideAnalysisIntro(f.ctx, f.actor, item.ID, analysisDecisionForTest(item, "reject"))
	if err != nil || !rejected.Suppressed {
		t.Fatalf("seed the retained rejection: %+v %v", rejected, err)
	}
	previousRevision, err := strconv.ParseInt(rejected.Revision, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	untouched := f.item(t, 2)
	if _, err := f.store.DecideAnalysisIntro(f.ctx, f.actor, untouched.ID, analysisDecisionForTest(untouched, "reset")); err != nil {
		t.Fatal(err)
	}
	otherFile := libraryIntegrationFile(t, f.store.roots[0].path, "unrelated-intro/Other/Season 01/Other.S01E01.mp4", "video:unrelated-intro")
	otherLibrary := libraryIntegrationCreate(t, f.ctx, f.store, "Unrelated TV", "tvshows", filepath.Dir(filepath.Dir(filepath.Dir(otherFile))))
	libraryIntegrationScan(t, f.ctx, f.store, otherLibrary.ID, "Completed")
	var otherID string
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE path=$1 AND type='Episode'`, otherFile).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	other, err := f.store.GetAnalysisItem(f.ctx, f.actor, otherID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DecideAnalysisIntro(f.ctx, f.actor, otherID, analysisDecisionForTest(other, "reject")); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var value string
		if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
			'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM analysis_intro_audit a),
			'explicit',(SELECT jsonb_agg(to_jsonb(marker) ORDER BY item_id) FROM item_intro_state marker),
			'otherDecisions',(SELECT jsonb_agg(to_jsonb(decision) ORDER BY item_id) FROM analysis_intro_decisions decision WHERE item_id<>$1),
			'media',(SELECT jsonb_agg(jsonb_build_array(id,media) ORDER BY id) FROM items))::text`, item.ID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	editing, err := f.store.GetLibraryEditing(f.ctx, f.libraryID)
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	if _, err := f.store.UpdateLibraryAsAdministrator(f.ctx, f.actor, identity.AdministratorNative, f.libraryID,
		LibraryUpdate{Revision: editing.Library.Revision, LibraryOptions: &LibraryOptionsUpdate{EnableIntroDetection: &enabled}}); err != nil {
		t.Fatal(err)
	}
	var retired bool
	if err := f.pool.QueryRow(f.ctx, `SELECT NOT rejected AND revision=$2 AND updated_by=$3
		FROM analysis_intro_decisions WHERE item_id=$1`, item.ID, previousRevision+1, f.actor.User.ID).Scan(&retired); err != nil || !retired {
		t.Fatalf("library opt-in did not retire its rejection at a new revision: %v", err)
	}
	if after := snapshot(); after != before {
		t.Fatal("library opt-in changed explicit markers, original media, audit history or unrelated decisions")
	}
}
