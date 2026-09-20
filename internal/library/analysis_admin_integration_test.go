//go:build linux

package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

type analysisAdminFixture struct {
	ctx        context.Context
	pool       *pgxpool.Pool
	store      *Store
	actor      identity.Principal
	libraryID  string
	ids, paths []string
}

func newAnalysisAdminFixture(t *testing.T) analysisAdminFixture {
	t.Helper()
	ctx, pool, store, root, _ := libraryIntegrationStore(t, mediaSourceTestProber{inner: &libraryFixtureProber{}})
	f := analysisAdminFixture{ctx: ctx, pool: pool, store: store, actor: metadataEditTestActor(t, ctx, pool, "analysis-admin-editor")}
	for index := 1; index <= 3; index++ {
		f.paths = append(f.paths, libraryIntegrationFile(t, root, fmt.Sprintf("analysis/Show/Season 01/Show.S01E%02d.mp4", index), fmt.Sprintf("video:analysis-source-%d", index)))
	}
	collection := libraryIntegrationCreate(t, ctx, store, "Analysis shows", "tvshows", filepath.Join(root, "analysis"))
	f.libraryID = collection.ID
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	for _, path := range f.paths {
		var id string
		if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE path=$1 AND type='Episode'`, path).Scan(&id); err != nil {
			t.Fatal(err)
		}
		f.ids = append(f.ids, id)
	}
	return f
}

func (f analysisAdminFixture) item(t *testing.T, index int) AnalysisItem {
	t.Helper()
	item, err := f.store.GetAnalysisItem(f.ctx, f.actor, f.ids[index])
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func (f analysisAdminFixture) seedReview(t *testing.T) AnalysisItem {
	t.Helper()
	tx, err := f.store.beginAnalysisAdminRead(f.ctx, f.actor)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	sources := make([]AnalysisSource, 0, len(f.ids))
	support := make([]introdetect.Support, 0, len(f.ids))
	interval := introdetect.Interval{StartTicks: 10 * media.TicksPerSecond, EndTicks: 25 * media.TicksPerSecond}
	for index, id := range f.ids {
		source, err := readAnalysisSourceUsing(f.ctx, tx, unrestrictedLibraryAccess(), id, false)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, source)
		digest := sha256.Sum256([]byte(fmt.Sprintf("video:analysis-source-%d", index+1)))
		support = append(support, introdetect.Support{EpisodeKey: source.EpisodeKey, SourceKey: source.SourceRevision, ContentIdentity: hex.EncodeToString(digest[:]), Interval: interval})
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	candidate := introdetect.Candidate{Interval: interval, GroupID: "review-group", Status: introdetect.Review,
		Reasons: []introdetect.Reason{introdetect.ShortInterval}, Support: support,
		Metrics: introdetect.Metrics{AudioSamples: 50, AudioDistinct: 50, PairCount: 3}}
	value := AnalysisStoredResult{Version: introdetect.Version, Episode: introdetect.EpisodeResult{EpisodeKey: sources[0].EpisodeKey, SourceKey: sources[0].SourceRevision,
		ContentIdentity: support[0].ContentIdentity, Status: introdetect.Review, Reasons: []introdetect.Reason{introdetect.ShortInterval}, Candidates: []introdetect.Candidate{candidate}}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateStoredAnalysisResult(raw, "review", &interval.StartTicks, &interval.EndTicks); err != nil {
		t.Fatal("review fixture violates the stored result contract")
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,child_id,cohort_revision,status,result,start_ticks,end_ticks)
		SELECT $1,1,$2,$3,revision,publication_epoch,'analysis-admin-fixture',$4,'review',$5,$6,$7 FROM analysis_settings WHERE id=1`,
		f.ids[0], sources[0].SourceRevision, strings.Repeat("a", 64), analysisCohortHash(sources), raw, interval.StartTicks, interval.EndTicks); err != nil {
		t.Fatal(err)
	}
	for index, source := range sources {
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO analysis_detection_sources(item_id,source_item_id,library_id,root_id,source_revision,hierarchy_revision,episode_key,content_sha256)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, f.ids[0], source.ItemID, source.LibraryID, source.RootID, source.SourceRevision, source.HierarchyRevision, source.EpisodeKey, support[index].ContentIdentity); err != nil {
			t.Fatal(err)
		}
	}
	item := f.item(t, 0)
	if item.Detection.Status != "review" || item.Detection.Candidate == nil {
		t.Fatal("current physical review fixture was not available")
	}
	return item
}

func analysisDecisionForTest(item AnalysisItem, action string) AnalysisDecision {
	return AnalysisDecision{Revision: item.Detection.Revision, SourceRevision: item.SourceRevision, ManualRevision: item.Detection.ManualRevision, Action: action}
}

func TestAnalysisAdminInventoryUsesActualScopeCountAndLiteralSearchWithoutOpeningFiles(t *testing.T) {
	f := newAnalysisAdminFixture(t)
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET name='Percent 100%',sort_name='a' WHERE id=$1`, f.ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.paths[1]); err != nil {
		t.Fatal(err)
	}
	page, err := f.store.ListAnalysisItems(f.ctx, f.actor, AnalysisItemQuery{LibraryID: f.libraryID, Limit: 1, StartIndex: 1})
	if err != nil || page.TotalRecordCount != 3 || len(page.Items) != 1 || page.Items[0].LibraryID != f.libraryID || page.Items[0].Previews == nil || page.Items[0].Detection.Reasons == nil {
		t.Fatalf("scoped indexed page: %+v %v", page, err)
	}
	page, err = f.store.ListAnalysisItems(f.ctx, f.actor, AnalysisItemQuery{LibraryID: f.libraryID, SearchTerm: "%", Limit: 25})
	if err != nil || page.TotalRecordCount != 1 || len(page.Items) != 1 || page.Items[0].ID != f.ids[0] {
		t.Fatal("search interpreted a literal percent sign as a wildcard")
	}
	page, err = f.store.ListAnalysisItems(f.ctx, f.actor, AnalysisItemQuery{LibraryID: f.libraryID, Limit: 25, StartIndex: 3})
	if err != nil || page.TotalRecordCount != 3 || page.Items == nil || len(page.Items) != 0 {
		t.Fatal("empty tail lost its scoped total or empty array")
	}
	if _, err := f.store.ListAnalysisItems(f.ctx, f.actor, AnalysisItemQuery{LibraryID: "unknown"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown explicit scope silently widened")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if result, err := f.store.ListAnalysisItems(f.ctx, f.actor, AnalysisItemQuery{}); !errors.Is(err, ErrForbidden) || len(result.Items) != 0 || result.TotalRecordCount != 0 {
		t.Fatal("revoked administrator received inventory")
	}
}

func TestAnalysisAdminInitialDecisionTombstoneCASAndManualLayerRemainIndependent(t *testing.T) {
	f := newAnalysisAdminFixture(t)
	item := f.item(t, 0)
	if item.Detection.Revision != "0" || item.Detection.ManualRevision != "0" {
		t.Fatal("absent detection manufactured a revision")
	}
	request := analysisDecisionForTest(item, "reject")
	rejected, err := f.store.DecideAnalysisIntro(f.ctx, f.actor, item.ID, request)
	if err != nil || rejected.Revision != "1" || !rejected.Suppressed || rejected.Candidate != nil {
		t.Fatalf("initial rejection: %+v %v", rejected, err)
	}
	if _, err := f.store.DecideAnalysisIntro(f.ctx, f.actor, item.ID, request); !errors.Is(err, ErrAnalysisConflict) {
		t.Fatal("old zero revision passed after a decision tombstone")
	}
	request.Revision, request.Action = rejected.Revision, "reset"
	reset, err := f.store.DecideAnalysisIntro(f.ctx, f.actor, item.ID, request)
	if err != nil || reset.Revision != "2" || reset.Suppressed || reset.Effective != nil {
		t.Fatalf("decision reset: %+v %v", reset, err)
	}
	manual, err := f.store.UpdateItemIntro(f.ctx, f.actor, item.ID, IntroEdit{Revision: "0", SourceRevision: item.SourceRevision, StartTicks: media.TicksPerSecond, EndTicks: 5 * media.TicksPerSecond, Provenance: "Manual"}, false)
	if err != nil {
		t.Fatal(err)
	}
	request.Revision, request.Action = reset.Revision, "reject"
	if _, err := f.store.DecideAnalysisIntro(f.ctx, f.actor, item.ID, request); !errors.Is(err, ErrAnalysisConflict) {
		t.Fatal("decision ignored a concurrent manual edit")
	}
	request.ManualRevision = manual.Revision
	rejected, err = f.store.DecideAnalysisIntro(f.ctx, f.actor, item.ID, request)
	if err != nil || rejected.Revision != "3" || rejected.ManualRevision != "1" || !reflect.DeepEqual(rejected.Effective, manual.Effective) {
		t.Fatal("rejection replaced the manual playback interval")
	}
}

func TestAnalysisAdminAcceptUsesSharedManualCASAndPreservesExactLargeRevision(t *testing.T) {
	f := newAnalysisAdminFixture(t)
	item := f.seedReview(t)
	request := analysisDecisionForTest(item, "accept")
	accepted, err := f.store.DecideAnalysisIntro(f.ctx, f.actor, item.ID, request)
	if err != nil || accepted.Revision != "2" || accepted.ManualRevision != "1" || accepted.Effective == nil || accepted.Effective.Provenance != "Manual" || accepted.Effective.StartTicks != item.Detection.Candidate.Interval.StartTicks {
		t.Fatalf("accept did not publish a manual CAS: %+v %v", accepted, err)
	}
	if _, err := f.store.DecideAnalysisIntro(f.ctx, f.actor, item.ID, request); !errors.Is(err, ErrAnalysisConflict) {
		t.Fatal("stale accept overwrote the manual layer")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE item_intro_state SET revision=9007199254740993 WHERE item_id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	current := f.item(t, 0)
	if current.Detection.ManualRevision != "9007199254740993" {
		t.Fatal("manual revision lost decimal precision")
	}
	accepted, err = f.store.DecideAnalysisIntro(f.ctx, f.actor, item.ID, analysisDecisionForTest(current, "accept"))
	if err != nil || accepted.ManualRevision != "9007199254740994" {
		t.Fatalf("shared manual CAS narrowed the bigint revision: %+v %v", accepted, err)
	}
	var raw []byte
	if err := f.pool.QueryRow(f.ctx, `SELECT evidence FROM analysis_intro_audit WHERE item_id=$1 ORDER BY id DESC LIMIT 1`, item.ID).Scan(&raw); err != nil || ValidateStoredAnalysisAudit(raw, "accept") != nil {
		t.Fatal("administrator audit violated its safe stored shape")
	}
}

func TestAnalysisAdminChangedSupportCannotBeAcceptedOrAdvertisedAsCurrent(t *testing.T) {
	f := newAnalysisAdminFixture(t)
	item := f.seedReview(t)
	if err := os.WriteFile(f.paths[1], []byte("video:replacement-support-with-different-bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	changed := f.item(t, 0)
	if changed.Detection.Status != "stale" || changed.Detection.Candidate == nil || changed.Detection.Effective != nil {
		t.Fatal("changed physical support retained current detection authority")
	}
	if _, err := f.store.DecideAnalysisIntro(f.ctx, f.actor, item.ID, analysisDecisionForTest(item, "accept")); err == nil {
		t.Fatal("accept used stale supporting bytes")
	}
	var manual, decisions int
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM item_intro_state WHERE item_id=$1),(SELECT count(*) FROM analysis_intro_decisions WHERE item_id=$1)`, item.ID).Scan(&manual, &decisions); err != nil || manual != 0 || decisions != 0 {
		t.Fatal("failed accept committed partial administrator state")
	}
}

func TestAnalysisAdminConcurrentDecisionsHaveOneWinnerAndFinalAuthorityRollsBack(t *testing.T) {
	t.Run("concurrent", func(t *testing.T) {
		f := newAnalysisAdminFixture(t)
		item := f.item(t, 0)
		start := make(chan struct{})
		results := make(chan error, 2)
		var workers sync.WaitGroup
		for _, action := range []string{"reject", "reset"} {
			workers.Add(1)
			go func(action string) {
				defer workers.Done()
				<-start
				_, err := f.store.DecideAnalysisIntro(f.ctx, f.actor, item.ID, analysisDecisionForTest(item, action))
				results <- err
			}(action)
		}
		close(start)
		workers.Wait()
		close(results)
		wins, conflicts := 0, 0
		for err := range results {
			if err == nil {
				wins++
			} else if errors.Is(err, ErrAnalysisConflict) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
		if wins != 1 || conflicts != 1 {
			t.Fatal("concurrent initial decisions lost compare-and-swap")
		}
	})
	t.Run("final-authority", func(t *testing.T) {
		f := newAnalysisAdminFixture(t)
		item := f.item(t, 0)
		if _, err := f.pool.Exec(f.ctx, `CREATE FUNCTION revoke_analysis_decision_actor() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN UPDATE sessions SET revoked_at=clock_timestamp() WHERE user_id='analysis-admin-editor'; RETURN NEW; END $$;
			CREATE TRIGGER revoke_analysis_decision_actor AFTER INSERT OR UPDATE ON analysis_intro_decisions FOR EACH ROW EXECUTE FUNCTION revoke_analysis_decision_actor()`); err != nil {
			t.Fatal(err)
		}
		if result, err := f.store.DecideAnalysisIntro(f.ctx, f.actor, item.ID, analysisDecisionForTest(item, "reject")); !errors.Is(err, ErrForbidden) || result.ItemID != "" {
			t.Fatal("late credential revocation committed a decision")
		}
		current := f.item(t, 0)
		if current.Detection.Revision != "0" || current.Detection.Suppressed {
			t.Fatal("failed final authorization left a decision tombstone")
		}
		var audit int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM analysis_intro_audit WHERE item_id=$1`, item.ID).Scan(&audit); err != nil || audit != 0 {
			t.Fatal("failed authority committed an audit receipt")
		}
	})
}

func TestAnalysisAdminReadRevalidatesOutsideTheRepeatableSnapshot(t *testing.T) {
	f := newAnalysisAdminFixture(t)
	tx, err := f.store.beginAnalysisAdminRead(f.ctx, f.actor)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	item, err := readAnalysisAdminItem(f.ctx, tx, f.ids[0])
	if err != nil || item.ID != f.ids[0] {
		t.Fatal("initial snapshot did not contain the authorized item")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.actor.SessionID); err != nil {
		t.Fatal(err)
	}
	// This is the reason final authorization cannot reuse the inventory's
	// repeatable snapshot: its old credential view still authorizes the reader.
	if err := (&catalogAdministrator{actor: f.actor, audience: identity.AdministratorNative}).check(f.ctx, tx, false); err != nil {
		t.Fatal("fixture failed to retain the original repeatable credential view")
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.store.finishAnalysisAdminRead(f.ctx, f.actor); !errors.Is(err, ErrForbidden) {
		t.Fatal("post-snapshot authority failed to observe committed revocation")
	}
}
