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
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

type analysisWorkFixture struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	store   *Store
	actor   identity.Principal
	viewer  string
	library Library
	paths   []string
	ids     []string
}

func newAnalysisWorkFixture(t *testing.T, count int) analysisWorkFixture {
	t.Helper()
	ctx, pool, store, root, viewer := libraryIntegrationStore(t, mediaSourceTestProber{inner: &libraryFixtureProber{}})
	paths := make([]string, count)
	for index := range paths {
		paths[index] = libraryIntegrationFile(t, root, fmt.Sprintf("tv/Analysis Show/Season 01/Analysis Show S01E%02d.mkv", index+1), fmt.Sprintf("video:independent-analysis-episode-%d", index))
	}
	collection := libraryIntegrationCreate(t, ctx, store, "Analysis episodes", "tvshows", filepath.Join(root, "tv"))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	ids := make([]string, count)
	for index, path := range paths {
		if err := pool.QueryRow(ctx, `SELECT id FROM items WHERE path=$1 AND type='Episode'`, path).Scan(&ids[index]); err != nil {
			t.Fatal(err)
		}
	}
	return analysisWorkFixture{ctx, pool, store, metadataEditTestActor(t, ctx, pool, "analysis-work-editor"), viewer, collection, paths, ids}
}

func (f analysisWorkFixture) admit(t *testing.T, key string, items []string, executions ...AnalysisExecutionProfile) (string, []string) {
	t.Helper()
	runID, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	selection := AnalysisSelection{LibraryIDs: []string{f.library.ID}, ItemIDs: items}
	input, _ := json.Marshal(selection)
	execution := analysisAdmissionTestIntroExecution()
	if key == TaskPreviewGenerationKey {
		execution = analysisAdmissionTestPreviewExecution()
	}
	if len(executions) > 1 {
		t.Fatal("one explicit fixture execution profile is required")
	}
	if len(executions) == 1 {
		execution = executions[0]
	}
	err = f.store.WithOwnedTx(f.ctx, func(tx OwnedTx) error {
		binding, err := PrepareAnalysis(tx, key, selection, execution)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO task_definitions(id,key,name) VALUES($1,$2,'Analysis fixture') ON CONFLICT(key) DO UPDATE SET name=EXCLUDED.name`, taskID, key); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,analysis_input,analysis_config_fingerprint)
   VALUES($1,(SELECT id FROM task_definitions WHERE key=$2),'pending','manual',$3,$4,'admin',$2,'Analysis fixture',$5,$6)`, runID, key, f.actor.User.ID, f.actor.SessionID, input, binding.ConfigurationFingerprint); err != nil {
			return err
		}
		if err := binding.Bind(tx, runID); err != nil {
			return err
		}
		_, err = binding.SnapshotChildren(tx, runID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := f.pool.Query(f.ctx, `SELECT id FROM task_run_children WHERE run_id=$1 ORDER BY ordinal`, runID)
	if err != nil {
		t.Fatal(err)
	}
	children := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		children = append(children, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	return runID, children
}

func (f analysisWorkFixture) fence(child string) AnalysisFence {
	// This callback deliberately uses real durable claim state. The tasks package
	// separately tests its sealed process capability and complete actor checks.
	return func(tx OwnedTx) error {
		var allowed bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_run_children c JOIN task_runs r ON r.id=c.run_id WHERE c.id=$1 AND c.state='running' AND c.executor_token=$2 AND r.state='running')`, child, strings.Repeat("1", 32)).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return ErrForbidden
		}
		return nil
	}
}
func (f analysisWorkFixture) claim(t *testing.T, runID, child string) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_runs SET state='running',started_at=clock_timestamp() WHERE id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_run_children SET state='running',started_at=clock_timestamp(),executor_token=$2 WHERE id=$1`, child, strings.Repeat("1", 32)); err != nil {
		t.Fatal(err)
	}
}

func TestAnalysisAdmissionWindowsHaveOnePublicationOwnerAndFrozenCohorts(t *testing.T) {
	f := newAnalysisWorkFixture(t, 40)
	runID, children := f.admit(t, TaskIntroAnalysisKey, nil)
	if len(children) != 3 {
		t.Fatalf("whole season was not partitioned into bounded target windows: %d", len(children))
	}
	var targets, sources int
	var duplicate bool
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM analysis_work_sources s JOIN analysis_work w ON w.child_id=s.child_id WHERE w.run_id=$1 AND target),
  (SELECT max(n) FROM (SELECT count(*) n FROM analysis_work_sources s JOIN analysis_work w ON w.child_id=s.child_id WHERE w.run_id=$1 GROUP BY s.child_id) q),
  EXISTS(SELECT item_id FROM analysis_work_sources s JOIN analysis_work w ON w.child_id=s.child_id WHERE w.run_id=$1 AND target GROUP BY item_id HAVING count(*)<>1)`, runID).Scan(&targets, &sources, &duplicate); err != nil || targets != 40 || sources > 32 || duplicate {
		t.Fatalf("target ownership or support cap failed: %d %d %v: %v", targets, sources, duplicate, err)
	}
	f.claim(t, runID, children[0])
	work, err := f.store.GetAnalysisWork(f.ctx, children[0], f.fence(children[0]))
	if err != nil || len(work.Sources) != 32 {
		t.Fatalf("read immutable work: %+v %v", work, err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE analysis_work_sources SET target=NOT target WHERE child_id=$1`, children[0]); err == nil {
		t.Fatal("an admitted source snapshot was mutable")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET index_number=99 WHERE id=$1`, f.ids[39]); err != nil {
		t.Fatal(err)
	}
	if actual, err := f.store.GetAnalysisWork(f.ctx, children[0], f.fence(children[0])); !errors.Is(err, ErrAnalysisSourceChanged) || !reflect.DeepEqual(actual, AnalysisWork{}) {
		t.Fatalf("a changed non-window cohort member retained authority: %+v %v", actual, err)
	}
}

func TestAnalysisLeafSelectionUsesSupportWithoutPublishingOtherEpisodes(t *testing.T) {
	f := newAnalysisWorkFixture(t, 4)
	runID, children := f.admit(t, TaskIntroAnalysisKey, []string{f.ids[1]})
	if len(children) != 1 {
		t.Fatal(children)
	}
	f.claim(t, runID, children[0])
	fence := f.fence(children[0])
	work, err := f.store.GetAnalysisWork(f.ctx, children[0], fence)
	if err != nil {
		t.Fatal(err)
	}
	targets := 0
	for _, source := range work.Sources {
		if source.Target {
			targets++
			if source.ItemID != f.ids[1] {
				t.Fatal("nonselected publication target")
			}
		}
	}
	if targets != 1 || len(work.Sources) != 4 {
		t.Fatal("leaf selection lost independent supports")
	}
	if err := f.store.PublishAnalysisAbstention(f.ctx, children[0], fence, "comparison_budget_exceeded"); err != nil {
		t.Fatal(err)
	}
	var count int
	var itemID, status string
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*),min(item_id),min(status) FROM analysis_detections`).Scan(&count, &itemID, &status); err != nil || count != 1 || itemID != f.ids[1] || status != "no_result" {
		t.Fatalf("support-only sources were published: %d %s %s %v", count, itemID, status, err)
	}
	var audit []byte
	if err := f.pool.QueryRow(f.ctx, `SELECT evidence FROM analysis_intro_audit WHERE item_id=$1`, f.ids[1]).Scan(&audit); err != nil || ValidateStoredAnalysisAudit(audit, "no_result") != nil {
		t.Fatalf("published abstention audit was not restorable: %v", err)
	}
}

func analysisFixtureQualifiedResult(t *testing.T, f analysisWorkFixture, work AnalysisWork) introdetect.Result {
	t.Helper()
	interval := introdetect.Interval{StartTicks: 10 * media.TicksPerSecond, EndTicks: 45 * media.TicksPerSecond}
	supports := []introdetect.Support{}
	for _, source := range work.Sources {
		var path string
		if err := f.pool.QueryRow(f.ctx, `SELECT path FROM items WHERE id=$1`, source.ItemID).Scan(&path); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		supports = append(supports, introdetect.Support{EpisodeKey: source.EpisodeKey, SourceKey: source.SourceRevision, ContentIdentity: hex.EncodeToString(digest[:]), Interval: interval})
	}
	metrics := introdetect.Metrics{AudioAgreementPermille: 1000, AudioInformativePermille: 1000, AudioSimilarityPermille: 1000, AudioSamples: 100, AudioDistinct: 100, VisualAgreementPermille: 1000, VisualSimilarityPermille: 1000, VisualCoveragePermille: 1000, VisualSamples: 35, VisualTransitions: 34, VisualChangeCoveragePermille: 1000, VisualDominancePermille: 100, PairCount: len(supports) * (len(supports) - 1) / 2}
	metrics.VisualAnchorCount, metrics.VisualMinBandMatchedPermille, metrics.VisualMatchedTimePermille = 35, 1000, 1000
	metrics.VisualDistinctStates, metrics.VisualDominantStatePermille = 8, 125
	group := introdetect.Group{ID: "fixture-independent-evidence", AlgorithmProfile: work.Execution.IntroProfile, Status: introdetect.Qualified, Reasons: []introdetect.Reason{}, Members: supports, Metrics: metrics}
	result := introdetect.Result{Version: introdetect.Version, CohortKey: work.ScopeKey, Options: work.Execution.DetectorOptions, Groups: []introdetect.Group{group}, Episodes: []introdetect.EpisodeResult{}}
	for _, support := range supports {
		result.Episodes = append(result.Episodes, introdetect.EpisodeResult{EpisodeKey: support.EpisodeKey, SourceKey: support.SourceKey, ContentIdentity: support.ContentIdentity, Status: introdetect.Qualified, Reasons: []introdetect.Reason{}, Candidates: []introdetect.Candidate{{Interval: interval, GroupID: group.ID, Status: group.Status, Reasons: []introdetect.Reason{}, Metrics: metrics, Support: supports}}})
	}
	return result
}

func TestAnalysisPublicationRequiresFinalFenceAndCurrentIndependentSources(t *testing.T) {
	f := newAnalysisWorkFixture(t, 3)
	runID, children := f.admit(t, TaskIntroAnalysisKey, nil)
	f.claim(t, runID, children[0])
	fence := f.fence(children[0])
	work, err := f.store.GetAnalysisWork(f.ctx, children[0], fence)
	if err != nil {
		t.Fatal(err)
	}
	result := analysisFixtureQualifiedResult(t, f, work)
	rejecting := func(tx OwnedTx) error {
		if err := fence(tx); err != nil {
			return err
		}
		var written bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM analysis_detections)`).Scan(&written); err != nil {
			return err
		}
		if written {
			return ErrForbidden
		}
		return nil
	}
	if err := f.store.PublishIntroAnalysis(f.ctx, children[0], rejecting, result); !errors.Is(err, ErrForbidden) {
		t.Fatalf("publication did not honor its final fence: %v", err)
	}
	var detections, audits int
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM analysis_detections),(SELECT count(*) FROM analysis_intro_audit)`).Scan(&detections, &audits); err != nil || detections != 0 || audits != 0 {
		t.Fatal("failed publication partially committed", err)
	}
	if err := f.store.PublishIntroAnalysis(f.ctx, children[0], fence, result); err != nil {
		t.Fatal(err)
	}
	intro, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, work.Sources[0].ItemID, "")
	if err != nil || intro == nil || intro.Provenance != "Detected" {
		t.Fatalf("qualified evidence was not delivered: %+v %v", intro, err)
	}
	// Replacing a supporting file without a rescan must immediately withdraw the
	// derived interval, while its retained audit facts remain unchanged.
	var supportPath string
	if err := f.pool.QueryRow(f.ctx, `SELECT path FROM items WHERE id=$1`, work.Sources[1].ItemID).Scan(&supportPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(supportPath, []byte("video:replacement-support-content"), 0600); err != nil {
		t.Fatal(err)
	}
	if intro, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: f.viewer}, work.Sources[0].ItemID, ""); err != nil || intro != nil {
		t.Fatalf("unscanned supporting replacement remained effective: %+v %v", intro, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM analysis_intro_audit`).Scan(&audits); err != nil || audits != 3 {
		t.Fatalf("source invalidation rewrote detection audit: %d %v", audits, err)
	}
}

func TestAnalysisWorkManualDecisionClearAndOldTokensFencePublication(t *testing.T) {
	f := newAnalysisWorkFixture(t, 3)
	runID, children := f.admit(t, TaskIntroAnalysisKey, nil)
	f.claim(t, runID, children[0])
	fence := f.fence(children[0])
	work, err := f.store.GetAnalysisWork(f.ctx, children[0], fence)
	if err != nil {
		t.Fatal(err)
	}
	source := work.Sources[0]
	if _, err := f.store.UpdateItemIntro(f.ctx, f.actor, source.ItemID, IntroEdit{Revision: source.ManualRevision, SourceRevision: source.SourceRevision, StartTicks: 1, EndTicks: 2, Provenance: "Manual"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetAnalysisWork(f.ctx, children[0], fence); !errors.Is(err, ErrAnalysisConflict) {
		t.Fatalf("manual edit did not fence old worker: %v", err)
	}
	previewRun, previewChildren := f.admit(t, TaskPreviewGenerationKey, []string{source.ItemID})
	f.claim(t, previewRun, previewChildren[0])
	previewFence := f.fence(previewChildren[0])
	if _, err := f.store.GetAnalysisWork(f.ctx, previewChildren[0], previewFence); err != nil {
		t.Fatal(err)
	}
	if err := f.store.ClearAnalysisPreviews(f.ctx, f.actor, source.ItemID, source.SourceRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetAnalysisWork(f.ctx, previewChildren[0], previewFence); !errors.Is(err, ErrAnalysisConflict) {
		t.Fatalf("clear did not fence unpublished preview work: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_run_children SET state='interrupted',finished_at=clock_timestamp(),executor_token=NULL WHERE id=$1`, previewChildren[0]); err != nil {
		t.Fatal(err)
	}
	if value, err := f.store.GetAnalysisWork(f.ctx, previewChildren[0], previewFence); !errors.Is(err, ErrForbidden) || !reflect.DeepEqual(value, AnalysisWork{}) {
		t.Fatalf("interrupted durable claim retained authority: %+v %v", value, err)
	}
}

func TestAnalysisDetectedProjectionBindsTheCapturedSingleItemSource(t *testing.T) {
	f := newAnalysisWorkFixture(t, 1)
	item, err := f.store.GetItemFor(f.ctx, Subject{UserID: f.viewer}, f.ids[0])
	if err != nil || item.AnalysisSourceRevision == "" {
		t.Fatalf("single item omitted its private source stamp: %v", err)
	}
	file, opened, err := f.store.OpenMediaFor(f.ctx, Subject{UserID: f.viewer}, item.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if opened.Item.AnalysisSourceRevision != item.AnalysisSourceRevision {
		t.Fatal("source delivery and item detail did not capture the same stamp")
	}
	raw, err := json.Marshal(item)
	if err != nil || strings.Contains(string(raw), "AnalysisSourceRevision") {
		t.Fatal("internal source stamp entered generic item JSON", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE library_id=$1`, f.library.ID); err != nil {
		t.Fatal(err)
	}
	if value, err := f.store.ResolveAnalysisIntroForRevision(f.ctx, Subject{UserID: f.viewer}, item.ID, "", item.AnalysisSourceRevision); !errors.Is(err, ErrAnalysisSourceChanged) || value != nil {
		t.Fatalf("old item snapshot borrowed a newly bound source: %+v %v", value, err)
	}
}
