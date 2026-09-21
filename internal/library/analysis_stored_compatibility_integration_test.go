//go:build linux

package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

func analysisCompatibilitySources(t *testing.T, f analysisAdminFixture) []AnalysisSource {
	t.Helper()
	var sources []AnalysisSource
	err := f.store.WithOwnedTx(f.ctx, func(tx OwnedTx) error {
		for _, id := range f.ids {
			source, err := readAnalysisSource(tx, id, false)
			if err != nil {
				return err
			}
			sources = append(sources, source)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sources
}

func analysisCompatibilityLiteral(t *testing.T, version, name string) []byte {
	t.Helper()
	switch version {
	case "v1":
		return analysisV1Literal(t, name)
	case "v2":
		return analysisV2Literal(t, name)
	default:
		t.Fatal("unsupported historical fixture version")
		return nil
	}
}

func seedAnalysisCompatibilityDetection(t *testing.T, f analysisAdminFixture, version string) {
	t.Helper()
	sources := analysisCompatibilitySources(t, f)
	raw := string(analysisCompatibilityLiteral(t, version, "analysis-result-"+version+"-qualified"))
	contents := make([]string, len(sources))
	for index, source := range sources {
		encodedEpisode, _ := json.Marshal(source.EpisodeKey)
		encodedSource, _ := json.Marshal(source.SourceRevision)
		raw = strings.ReplaceAll(raw, fmt.Sprintf(`"episode-%d"`, index+1), string(encodedEpisode))
		raw = strings.ReplaceAll(raw, fmt.Sprintf(`"source-%d"`, index+1), string(encodedSource))
		content := sha256.Sum256([]byte(fmt.Sprintf("video:analysis-source-%d", index+1)))
		contents[index] = hex.EncodeToString(content[:])
		raw = strings.ReplaceAll(raw, strings.Repeat(fmt.Sprint(index+1), 64), contents[index])
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,
		profile_revision,publication_epoch,child_id,cohort_revision,status,result,start_ticks,end_ticks,auto_published)
		SELECT $1,1,$2,$3,revision,publication_epoch,$6,$4,'qualified',$5,100000000,400000000,true
		FROM analysis_settings WHERE id=1`, f.ids[0], sources[0].SourceRevision, strings.Repeat("a", 64), analysisCohortHash(sources), raw, "historical-"+version+"-child"); err != nil {
		t.Fatal(err)
	}
	for index, source := range sources {
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO analysis_detection_sources(item_id,source_item_id,library_id,root_id,
			source_revision,hierarchy_revision,episode_key,content_sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
			f.ids[0], source.ItemID, source.LibraryID, source.RootID, source.SourceRevision, source.HierarchyRevision, source.EpisodeKey, contents[index]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAnalysisV1DetectionRemainsReadableButCannotBecomeEffectiveOrAccepted(t *testing.T) {
	checkAnalysisRetiredDetectionCannotBecomeEffectiveOrAccepted(t, "v1")
}

func TestAnalysisV2DetectionRemainsReadableButCannotBecomeEffectiveOrAccepted(t *testing.T) {
	checkAnalysisRetiredDetectionCannotBecomeEffectiveOrAccepted(t, "v2")
}

func checkAnalysisRetiredDetectionCannotBecomeEffectiveOrAccepted(t *testing.T, version string) {
	t.Helper()
	f := newAnalysisAdminFixture(t)
	seedAnalysisCompatibilityDetection(t, f, version)
	item := f.item(t, 0)
	if item.Detection.Status != "stale" || item.Detection.Candidate != nil || item.Detection.Effective != nil ||
		!reflect.DeepEqual(item.Detection.Reasons, []string{"algorithm_changed"}) {
		t.Fatalf("old qualification became a current candidate: %+v", item.Detection)
	}
	page, err := f.store.ListAnalysisItems(f.ctx, f.actor, AnalysisItemQuery{LibraryID: f.libraryID, Limit: 25})
	if err != nil || page.TotalRecordCount != 3 || len(page.Items) != 3 {
		t.Fatalf("one old row made valid inventory unavailable: %+v %v", page, err)
	}
	const viewer = "analysis-compatibility-viewer"
	libraryIntegrationUser(t, f.ctx, f.pool, viewer, false, false, []string{f.libraryID})
	if interval, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: viewer}, item.ID, item.MediaSourceID); err != nil || interval != nil {
		t.Fatalf("playback resolved an old automatic interval: %+v %v", interval, err)
	}
	if _, err := f.store.DecideAnalysisIntro(f.ctx, f.actor, item.ID, analysisDecisionForTest(item, "accept")); !errors.Is(err, ErrAnalysisConflict) {
		t.Fatalf("old qualification could be promoted into a new manual marker: %v", err)
	}
	var active bool
	if err := f.pool.QueryRow(f.ctx, `SELECT auto_published FROM analysis_detections WHERE item_id=$1`, item.ID).Scan(&active); err != nil || !active {
		t.Fatal("read-only compatibility rewrote historical publication state")
	}
	var explicit *IntroInterval
	for _, provenance := range []string{"Manual", "Import"} {
		marker, err := f.store.UpdateItemIntro(f.ctx, f.actor, item.ID, IntroEdit{Revision: item.Detection.ManualRevision, SourceRevision: item.SourceRevision,
			StartTicks: media.TicksPerSecond, EndTicks: 5 * media.TicksPerSecond, Provenance: provenance}, false)
		if err != nil {
			t.Fatal(err)
		}
		explicit = marker.Effective
		item = f.item(t, 0)
		if !reflect.DeepEqual(item.Detection.Effective, explicit) || explicit == nil || explicit.Provenance != provenance {
			t.Fatalf("retiring an algorithm hid the independent %s marker", provenance)
		}
	}
	for _, action := range []string{"reject", "reset"} {
		result, err := f.store.DecideAnalysisIntro(f.ctx, f.actor, item.ID, analysisDecisionForTest(item, action))
		if err != nil || result.Status != "stale" || result.Candidate != nil || result.Suppressed != (action == "reject") ||
			!reflect.DeepEqual(result.Effective, explicit) {
			t.Fatalf("old evidence blocked safe %s or changed explicit state: %+v %v", action, result, err)
		}
		item = f.item(t, 0)
	}
}

func TestAnalysisV1DetectionPreservesCurrentExplicitChapterIntro(t *testing.T) {
	checkAnalysisRetiredDetectionPreservesCurrentChapterIntro(t, "v1")
}

func TestAnalysisV2DetectionPreservesCurrentExplicitChapterIntro(t *testing.T) {
	checkAnalysisRetiredDetectionPreservesCurrentChapterIntro(t, "v2")
}

func checkAnalysisRetiredDetectionPreservesCurrentChapterIntro(t *testing.T, version string) {
	t.Helper()
	f := newAnalysisAdminFixture(t)
	chapters, err := json.Marshal([]media.Chapter{
		{StartTicks: 0, EndTicks: 5 * media.TicksPerSecond, Title: "IntroStart"},
		{StartTicks: 5 * media.TicksPerSecond, EndTicks: 10 * media.TicksPerSecond, Title: "IntroEnd"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET media=jsonb_set(media,'{Chapters}',$2::jsonb) WHERE id=$1`, f.ids[0], chapters); err != nil {
		t.Fatal(err)
	}
	seedAnalysisCompatibilityDetection(t, f, version)
	item := f.item(t, 0)
	want := &IntroInterval{StartTicks: 0, EndTicks: 5 * media.TicksPerSecond, Provenance: "Chapter"}
	if item.Detection.Status != "stale" || item.Detection.Candidate != nil || !reflect.DeepEqual(item.Detection.Effective, want) {
		t.Fatalf("obsolete matcher history hid the current explicit chapter pair: %+v", item.Detection)
	}
	const viewer = "analysis-compatibility-chapter-viewer"
	libraryIntegrationUser(t, f.ctx, f.pool, viewer, false, false, []string{f.libraryID})
	if interval, err := f.store.ResolveAnalysisIntroFor(f.ctx, Subject{UserID: viewer}, item.ID, item.MediaSourceID); err != nil || !reflect.DeepEqual(interval, want) {
		t.Fatalf("playback lost the independent chapter interval: %+v %v", interval, err)
	}
}

func TestAnalysisV1AdmissionCannotUseAValidCurrentWorkerFence(t *testing.T) {
	checkAnalysisRetiredAdmissionCannotUseCurrentWorkerFence(t, "v1")
}

func TestAnalysisV2AdmissionCannotUseAValidCurrentWorkerFence(t *testing.T) {
	checkAnalysisRetiredAdmissionCannotUseCurrentWorkerFence(t, "v2")
}

func checkAnalysisRetiredAdmissionCannotUseCurrentWorkerFence(t *testing.T, version string) {
	t.Helper()
	f := newAnalysisAdminFixture(t)
	sources := analysisCompatibilitySources(t, f)
	canonical := strings.Replace(string(analysisCompatibilityLiteral(t, version, "analysis-admission-"+version+"-intro")), `"Revision":7,"Epoch":3`, `"Revision":1,"Epoch":1`, 1)
	digest := sha256.Sum256([]byte(canonical))
	fingerprint := hex.EncodeToString(digest[:])
	var envelope struct{ Profile, Execution json.RawMessage }
	if err := json.Unmarshal([]byte(canonical), &envelope); err != nil || ValidateStoredAnalysisAdmission(envelope.Profile, envelope.Execution, 1, 1, fingerprint) != nil {
		t.Fatal("legacy fixture is not an independently valid historical admission")
	}
	run := strings.Repeat("e", 32)
	scope := "analysis:" + strings.Repeat("1", 64)
	childDigest := sha256.Sum256([]byte(run + ":" + scope))
	child := hex.EncodeToString(childDigest[:16])
	err := f.store.WithOwnedTx(f.ctx, func(tx OwnedTx) error {
		if _, err := tx.Exec(`INSERT INTO task_definitions(id,key,name) VALUES(repeat('d',32),$1,'Historical intro')
			ON CONFLICT(key) DO UPDATE SET name=EXCLUDED.name`, TaskIntroAnalysisKey); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
			analysis_input,analysis_config_fingerprint,total_children,started_at)
			VALUES($1,(SELECT id FROM task_definitions WHERE key=$4),'running','manual',$2,$3,'admin',$4,'Historical intro','{}',$5,1,clock_timestamp())`,
			run, f.actor.User.ID, f.actor.SessionID, TaskIntroAnalysisKey, fingerprint); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)
			VALUES($1,1,1,$2,$3,$4)`, run, fingerprint, envelope.Profile, envelope.Execution); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key,state,started_at,executor_token)
			VALUES($1,$2,$3,'Historical intro',0,$4,'running',clock_timestamp(),$5)`, child, run, f.libraryID, scope, strings.Repeat("1", 32)); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO analysis_work(child_id,run_id,task_key,library_id,scope_key,cohort_revision,force)
			VALUES($1,$2,$3,$4,$5,$6,false)`, child, run, TaskIntroAnalysisKey, f.libraryID, scope, analysisCohortHash(sources)); err != nil {
			return err
		}
		for index, source := range sources {
			if _, err := tx.Exec(`INSERT INTO analysis_work_sources(child_id,item_id,position,target,library_id,root_id,series_id,season_id,
				episode_key,item_type,source_revision,hierarchy_revision,duration_ticks,size,manual_revision,decision_revision,preview_revision)
				VALUES($1,$2,$3,true,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,0,0,0)`, child, source.ItemID, index, source.LibraryID,
				source.RootID, source.SeriesID, source.SeasonID, source.EpisodeKey, source.ItemType, source.SourceRevision, source.HierarchyRevision, source.DurationTicks, source.Size); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	fence := func(tx OwnedTx) error {
		var valid bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_run_children c JOIN task_runs r ON r.id=c.run_id
			WHERE c.id=$1 AND c.state='running' AND r.state='running' AND c.executor_token=$2)`, child, strings.Repeat("1", 32)).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return ErrForbidden
		}
		return nil
	}
	if work, err := f.store.GetAnalysisWork(f.ctx, child, fence); !errors.Is(err, ErrUnavailable) || !reflect.DeepEqual(work, AnalysisWork{}) {
		t.Fatalf("a valid task fence upgraded old execution semantics: %+v %v", work, err)
	}
	if _, found, err := f.store.GetAnalysisFeatures(f.ctx, child, sources[0].ItemID, fence); !errors.Is(err, ErrUnavailable) || found {
		t.Fatalf("a valid task fence acquired an old feature cache: found=%v err=%v", found, err)
	}
	if err := f.store.PublishAnalysisAbstention(f.ctx, child, fence, "source_unavailable"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("old work published a newly stamped abstention: %v", err)
	}
	var detections int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM analysis_detections`).Scan(&detections); err != nil || detections != 0 {
		t.Fatal("rejected historical worker changed durable detection state")
	}
}

func TestAnalysisCurrentUnavailableAdmissionCanPublishOnlyTruthfulAbstention(t *testing.T) {
	for _, reason := range []string{"disabled", "not_configured", "dependencies_unavailable", "cache_unavailable", "unsupported_platform"} {
		t.Run(reason, func(t *testing.T) {
			f := newAnalysisWorkFixture(t, 3)
			execution := AnalysisExecutionProfile{Version: AnalysisExecutionProfileVersion, UnavailableReason: reason}
			run, children := f.admit(t, TaskIntroAnalysisKey, nil, execution)
			f.claim(t, run, children[0])
			fence := f.fence(children[0])
			work, err := f.store.GetAnalysisWork(f.ctx, children[0], fence)
			if err != nil || work.Execution.Available || work.Execution.DetectorVersion != "" {
				t.Fatalf("unavailable admission fabricated detector authority: %+v %v", work.Execution, err)
			}
			if err := f.store.PublishAnalysisAbstention(f.ctx, children[0], fence, "source_unavailable"); err != nil {
				t.Fatalf("truthful unavailable outcome could not be retained: %v", err)
			}
			var valid bool
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*)=3 AND bool_and(status='no_result' AND NOT auto_published
				AND result->>'Version'=$1 AND result->>'Reason'='source_unavailable'
				AND result#>'{Episode,Candidates}'='[]'::jsonb AND result#>'{Episode,Reasons}'='[]'::jsonb
				AND result#>>'{Episode,ContentIdentity}'='') FROM analysis_detections`, introdetect.Version).Scan(&valid); err != nil || !valid {
				t.Fatal("unavailable abstention introduced candidate, content, or automatic evidence")
			}
			result := analysisFixtureQualifiedResult(t, f, work)
			if err := f.store.PublishIntroAnalysis(f.ctx, children[0], fence, result); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("unavailable admission published a fabricated qualified result: %v", err)
			}
		})
	}
}

func TestAnalysisPublicationBindsEachCandidateToItsExactGroupEvidence(t *testing.T) {
	f := newAnalysisWorkFixture(t, 3)
	run, children := f.admit(t, TaskIntroAnalysisKey, nil)
	f.claim(t, run, children[0])
	work, err := f.store.GetAnalysisWork(f.ctx, children[0], f.fence(children[0]))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"metrics", "reasons"} {
		t.Run(field, func(t *testing.T) {
			result := analysisFixtureQualifiedResult(t, f, work)
			if _, err := validateAnalysisResultForWork(work, result); err != nil {
				t.Fatal("complete current fixture must be valid before changing its group evidence")
			}
			if field == "metrics" {
				result.Groups[0].Metrics.VisualMinBandMatchedPermille--
			} else {
				result.Groups[0].Reasons = append(result.Groups[0].Reasons, "weak_visual_evidence")
			}
			if _, err := validateAnalysisResultForWork(work, result); !errors.Is(err, ErrInvalidInput) {
				t.Fatal("candidate evidence diverged from its referenced qualified group")
			}
		})
	}
}
