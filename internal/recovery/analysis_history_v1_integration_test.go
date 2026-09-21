//go:build linux

package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/library"
)

// Complete v1 wire data is frozen from 92577f2, including the canonical order
// used by its admission fingerprint. Current matcher structs must not supply it.
const analysisRecoveryV1Profile = `{"AutoPublishIntros":true,"PreviewIntervalSeconds":10,"PreviewQuality":80,"MaxSourceBytes":137438953472,"MaxItemRuntimeSeconds":1200,"FeatureCacheMaxBytes":134217728}`
const analysisRecoveryV1Unavailable = `{"Version":1,"Available":false,"UnavailableReason":"not_configured","FFmpegSHA256":"","FFprobeSHA256":"","FingerprintSHA256":"","DetectorVersion":"","DetectorOptions":{"MaxEpisodes":0,"MaxAudioSamples":0,"MaxVisualSamples":0,"MaxFeatureBytes":0,"MaxComparisons":0,"MaxOffsetCandidates":0,"MaxCandidatesPerPair":0,"MaxGroups":0,"WindowTicks":0,"MinDurationTicks":0,"MaxDurationTicks":0,"AutoMinDurationTicks":0,"OffsetBinTicks":0,"AudioAlignmentTicks":0,"MaxAudioGapTicks":0,"VisualAlignmentTicks":0,"MaxVisualGapTicks":0,"BoundaryToleranceTicks":0,"MinSupport":0,"MaxAudioHamming":0,"MaxVisualHamming":0,"MinAudioAgreement":0,"MinAudioInformation":0,"MinVisualAgreement":0,"MinAudioSimilarity":0,"MinVisualSimilarity":0,"MinVisualContrast":0,"MinVisualSamples":0,"MinVisualTransitions":0,"MinVisualChangeCoverage":0,"MaxVisualDominance":0},"VisualIntervalTicks":0,"PreviewProfile":"","PreviewWidths":null,"IntroProfile":""}`

// The first schema-50 preview profile did not claim the later geometry suffix.
const analysisRecoveryV1BarePreview = `{"Version":1,"Available":true,"UnavailableReason":"","FFmpegSHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","FFprobeSHA256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","FingerprintSHA256":"","DetectorVersion":"","DetectorOptions":{"MaxEpisodes":0,"MaxAudioSamples":0,"MaxVisualSamples":0,"MaxFeatureBytes":0,"MaxComparisons":0,"MaxOffsetCandidates":0,"MaxCandidatesPerPair":0,"MaxGroups":0,"WindowTicks":0,"MinDurationTicks":0,"MaxDurationTicks":0,"AutoMinDurationTicks":0,"OffsetBinTicks":0,"AudioAlignmentTicks":0,"MaxAudioGapTicks":0,"VisualAlignmentTicks":0,"MaxVisualGapTicks":0,"BoundaryToleranceTicks":0,"MinSupport":0,"MaxAudioHamming":0,"MaxVisualHamming":0,"MinAudioAgreement":0,"MinAudioInformation":0,"MinVisualAgreement":0,"MinAudioSimilarity":0,"MinVisualSimilarity":0,"MinVisualContrast":0,"MinVisualSamples":0,"MinVisualTransitions":0,"MinVisualChangeCoverage":0,"MaxVisualDominance":0},"VisualIntervalTicks":0,"PreviewProfile":"source-pts-display-preceding-hold-jpeg-v3","PreviewWidths":[240,320,400],"IntroProfile":""}`
const analysisRecoveryV1Abstention = `{"Version":"introdetect-v1","Episode":{"EpisodeKey":"","SourceKey":"unavailable-source-v1","ContentIdentity":"","Status":"no_result","Reasons":[],"Candidates":[]},"Reason":"source_unavailable"}`
const analysisRecoveryV1Execution = `{"Version":1,"Available":true,"UnavailableReason":"","FFmpegSHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","FFprobeSHA256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","FingerprintSHA256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","DetectorVersion":"introdetect-v1","DetectorOptions":{"MaxEpisodes":32,"MaxAudioSamples":6000,"MaxVisualSamples":2400,"MaxFeatureBytes":262144,"MaxComparisons":100000000,"MaxOffsetCandidates":6,"MaxCandidatesPerPair":12,"MaxGroups":128,"WindowTicks":6000000000,"MinDurationTicks":150000000,"MaxDurationTicks":1800000000,"AutoMinDurationTicks":300000000,"OffsetBinTicks":5000000,"AudioAlignmentTicks":3333333,"MaxAudioGapTicks":10000000,"VisualAlignmentTicks":10000000,"MaxVisualGapTicks":30000000,"BoundaryToleranceTicks":30000000,"MinSupport":3,"MaxAudioHamming":6,"MaxVisualHamming":10,"MinAudioAgreement":900,"MinAudioInformation":600,"MinVisualAgreement":850,"MinAudioSimilarity":850,"MinVisualSimilarity":850,"MinVisualContrast":40,"MinVisualSamples":8,"MinVisualTransitions":3,"MinVisualChangeCoverage":300,"MaxVisualDominance":600},"VisualIntervalTicks":5000000,"PreviewProfile":"","PreviewWidths":null,"IntroProfile":"archive-intro-v1"}`
const analysisRecoveryV1Result = `{"Version":"introdetect-v1","Episode":{"EpisodeKey":"episode-two","SourceKey":"source-two","ContentIdentity":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","Status":"qualified","Reasons":[],"Candidates":[{"Interval":{"StartTicks":50000000,"EndTicks":400000000},"GroupID":"retained-v1-group","Status":"qualified","Reasons":[],"Metrics":{"AudioAgreementPermille":1000,"AudioInformativePermille":1000,"AudioSimilarityPermille":1000,"AudioSamples":100,"AudioDistinct":100,"VisualAgreementPermille":1000,"VisualSimilarityPermille":1000,"VisualCoveragePermille":1000,"VisualSamples":30,"VisualTransitions":10,"VisualChangeCoveragePermille":1000,"VisualDominancePermille":0,"BoundaryUncertaintyTicks":0,"PairCount":3},"Support":[{"EpisodeKey":"episode-one","SourceKey":"source-one","ContentIdentity":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","Interval":{"StartTicks":50000000,"EndTicks":400000000}},{"EpisodeKey":"episode-two","SourceKey":"source-two","ContentIdentity":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","Interval":{"StartTicks":50000000,"EndTicks":400000000}},{"EpisodeKey":"episode-three","SourceKey":"source-three","ContentIdentity":"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","Interval":{"StartTicks":50000000,"EndTicks":400000000}}]}]},"Reason":""}`

func seedAnalysisRecoveryV1History(t *testing.T, f *engineRecoveryFixture) string {
	t.Helper()
	canonical := `{"Version":1,"Revision":1,"Epoch":1,"Profile":` + analysisRecoveryV1Profile + `,"Execution":` + analysisRecoveryV1Execution + `}`
	digest := sha256.Sum256([]byte(canonical))
	fingerprint := hex.EncodeToString(digest[:])
	run, scope := strings.Repeat("9", 32), "analysis:"+strings.Repeat("4", 64)
	childDigest := sha256.Sum256([]byte(run + ":" + scope))
	child := hex.EncodeToString(childDigest[:16])
	if _, err := f.source.Exec(f.ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
		analysis_input,analysis_config_fingerprint,total_children,terminal_children,completed_children,started_at,finished_at)
		VALUES($1,repeat('7',32),'completed','manual','retired-v1-user','retired-v1-session','admin','media.intro_analysis','Historical intro analysis',
		'{}',$2,1,1,1,'2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, run, fingerprint); err != nil {
		t.Fatal("seed completed v1 history with retired credentials")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)
		VALUES($1,1,1,$2,$3,$4)`, run, fingerprint, analysisRecoveryV1Profile, analysisRecoveryV1Execution); err != nil {
		t.Fatal("seed literal original v1 admission")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key,state,started_at,finished_at)
		VALUES($1,$2,$3,'Historical analysis',0,$4,'completed','2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, child, run, f.libraryID, scope); err != nil {
		t.Fatal("seed completed historical child")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_work(child_id,run_id,task_key,library_id,scope_key,cohort_revision,force)
		VALUES($1,$2,'media.intro_analysis',$3,$4,repeat('c',64),false)`, child, run, f.libraryID, scope); err != nil {
		t.Fatal("seed historical immutable cohort")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_work_sources(child_id,item_id,position,target,library_id,root_id,series_id,season_id,episode_key,item_type,
		source_revision,hierarchy_revision,duration_ticks,size,manual_revision,decision_revision,preview_revision)
		SELECT $1,source.item_id,source.position,source.item_id='analysis-restore-two',source.library_id,source.root_id,source.series_id,source.season_id,
		source.episode_key,source.item_type,source.source_revision,source.hierarchy_revision,source.duration_ticks,source.size,
		source.manual_revision,source.decision_revision,source.preview_revision
		FROM analysis_work_sources source JOIN analysis_work work ON work.child_id=source.child_id WHERE work.run_id=repeat('5',32)`, child); err != nil {
		t.Fatal("copy admitted source facts without reading media")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,
		child_id,cohort_revision,status,result,start_ticks,end_ticks,auto_published)
		VALUES('analysis-restore-two',1,'source-two',$1,1,1,$2,repeat('c',64),'qualified',$3,50000000,400000000,true)`, fingerprint, child, analysisRecoveryV1Result); err != nil {
		t.Fatal("seed original v1 qualified result")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_detection_sources(item_id,source_item_id,library_id,root_id,source_revision,hierarchy_revision,episode_key,content_sha256)
		SELECT 'analysis-restore-two',source_item_id,library_id,root_id,source_revision,hierarchy_revision,episode_key,content_sha256
		FROM analysis_detection_sources WHERE item_id='analysis-restore-one'`); err != nil {
		t.Fatal("retain every independent v1 support reference")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_intro_audit(item_id,revision,source_revision,profile_fingerprint,action,evidence)
		VALUES('analysis-restore-two',1,'source-two',$1,'qualified',$2)`, fingerprint, `{"Result":`+analysisRecoveryV1Result+`,"Decision":null}`); err != nil {
		t.Fatal("seed literal historical qualification audit")
	}
	seedAnalysisRecoveryUnavailableHistory(t, f)
	return fingerprint
}

func seedAnalysisRecoveryUnavailableHistory(t *testing.T, f *engineRecoveryFixture) {
	t.Helper()
	canonical := `{"Version":1,"Revision":1,"Epoch":1,"Profile":` + analysisRecoveryV1Profile + `,"Execution":` + analysisRecoveryV1Unavailable + `}`
	digest := sha256.Sum256([]byte(canonical))
	current := library.AnalysisExecutionProfile{Version: library.AnalysisExecutionProfileVersion, UnavailableReason: "not_configured"}
	currentRaw, err := json.Marshal(current)
	if err != nil {
		t.Fatal("encode the current unavailable execution profile")
	}
	for _, entry := range []struct{ version, run, scope, execution, fingerprint string }{
		{"v1", strings.Repeat("a", 32), "analysis:" + strings.Repeat("5", 64), analysisRecoveryV1Unavailable, hex.EncodeToString(digest[:])},
		{"v2", strings.Repeat("b", 32), "analysis:" + strings.Repeat("6", 64), string(currentRaw), analysisRecoveryFingerprint(library.DefaultAnalysisProfile(), current)},
	} {
		item, source := "analysis-restore-unavailable-"+entry.version, "unavailable-source-"+entry.version
		result := strings.ReplaceAll(analysisRecoveryV1Abstention, "v1", entry.version)
		childDigest := sha256.Sum256([]byte(entry.run + ":" + entry.scope))
		child := hex.EncodeToString(childDigest[:16])
		if _, err := f.source.Exec(f.ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES($1,$2,$1,$1,'Movie',false)`, item, f.libraryID); err != nil {
			t.Fatal("seed an independent unavailable source owner")
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
			analysis_input,analysis_config_fingerprint,total_children,terminal_children,completed_children,started_at,finished_at)
			VALUES($1,repeat('7',32),'completed','manual','retired-user','retired-session','admin','media.intro_analysis','Unavailable analysis history',
			'{}',$2,1,1,1,'2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, entry.run, entry.fingerprint); err != nil {
			t.Fatal("seed completed unavailable admission")
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)
			VALUES($1,1,1,$2,$3,$4)`, entry.run, entry.fingerprint, analysisRecoveryV1Profile, entry.execution); err != nil {
			t.Fatal("retain the original unavailable execution envelope")
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key,state,started_at,finished_at)
			VALUES($1,$2,$3,'Unavailable history',0,$4,'completed','2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, child, entry.run, f.libraryID, entry.scope); err != nil {
			t.Fatal("seed unavailable history child")
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_work(child_id,run_id,task_key,library_id,scope_key,cohort_revision,force)
			VALUES($1,$2,'media.intro_analysis',$3,$4,repeat('c',64),false)`, child, entry.run, f.libraryID, entry.scope); err != nil {
			t.Fatal("seed unavailable source work")
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_work_sources(child_id,item_id,position,target,library_id,root_id,series_id,season_id,episode_key,item_type,
			source_revision,hierarchy_revision,duration_ticks,size,manual_revision,decision_revision,preview_revision)
			VALUES($1,$2,0,true,$3,'retired-root','','','','Movie',$4,'retired-hierarchy',600000000,1024,0,0,0)`, child, item, f.libraryID, source); err != nil {
			t.Fatal("retain unavailable source facts without reading media")
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,
			child_id,cohort_revision,status,result) VALUES($1,1,$2,$3,1,1,$4,repeat('c',64),'no_result',$5)`, item, source, entry.fingerprint, child, result); err != nil {
			t.Fatal("seed safe unavailable abstention without matcher evidence")
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_intro_audit(item_id,revision,source_revision,profile_fingerprint,action,evidence)
			VALUES($1,1,$2,$3,'no_result',$4)`, item, source, entry.fingerprint, `{"Result":`+result+`,"Decision":null}`); err != nil {
			t.Fatal("seed version-matched unavailable audit evidence")
		}
	}
	previewFingerprint := analysisRecoveryV1AdmissionFingerprint(analysisRecoveryV1BarePreview)
	if _, err := f.source.Exec(f.ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
		analysis_input,analysis_config_fingerprint,started_at,finished_at)
		VALUES(repeat('c',32),repeat('8',32),'completed','manual','retired-user','retired-session','admin','media.preview_generation','Early preview history',
		'{}',$1,'2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, previewFingerprint); err != nil {
		t.Fatal("seed completed early preview admission without media")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)
		VALUES(repeat('c',32),1,1,$1,$2,$3)`, previewFingerprint, analysisRecoveryV1Profile, analysisRecoveryV1BarePreview); err != nil {
		t.Fatal("retain the bare preview profile in its original fingerprint")
	}
}

func analysisRecoveryV1AdmissionFingerprint(execution string) string {
	canonical := `{"Version":1,"Revision":1,"Epoch":1,"Profile":` + analysisRecoveryV1Profile + `,"Execution":` + execution + `}`
	digest := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(digest[:])
}

func assertUnavailableProfilesRejectQualifiedHistory(t *testing.T, f *engineRecoveryFixture) {
	t.Helper()
	current := library.AnalysisExecutionProfile{Version: library.AnalysisExecutionProfileVersion, UnavailableReason: "not_configured"}
	currentRaw, err := json.Marshal(current)
	if err != nil {
		t.Fatal("encode the unavailable current profile")
	}
	for _, entry := range []struct{ version, run, item, execution, fingerprint string }{
		{"v1", strings.Repeat("9", 32), "analysis-restore-two", analysisRecoveryV1Unavailable, analysisRecoveryV1AdmissionFingerprint(analysisRecoveryV1Unavailable)},
		{"v2", strings.Repeat("5", 32), "analysis-restore-one", string(currentRaw), analysisRecoveryFingerprint(library.DefaultAnalysisProfile(), current)},
	} {
		t.Run("unavailable_"+entry.version+"_cannot_own_qualified_history", func(t *testing.T) {
			tx, err := f.source.Begin(f.ctx)
			if err != nil {
				t.Fatal("begin isolated unavailable profile substitution")
			}
			defer tx.Rollback(f.ctx)
			if _, err := tx.Exec(f.ctx, `ALTER TABLE analysis_run_profiles DISABLE TRIGGER analysis_profile_immutable`); err != nil {
				t.Fatal("allow the isolated historical profile fixture")
			}
			if _, err := tx.Exec(f.ctx, `UPDATE analysis_run_profiles SET execution=$2,fingerprint=$3 WHERE run_id=$1`, entry.run, entry.execution, entry.fingerprint); err != nil {
				t.Fatal("replace only the execution with a valid unavailable envelope")
			}
			if _, err := tx.Exec(f.ctx, `ALTER TABLE analysis_run_profiles ENABLE TRIGGER analysis_profile_immutable`); err != nil {
				t.Fatal("restore the exact catalog before recovery inspection")
			}
			if _, err := tx.Exec(f.ctx, `UPDATE task_runs SET analysis_config_fingerprint=$2 WHERE id=$1`, entry.run, entry.fingerprint); err != nil {
				t.Fatal("keep the substituted admission fingerprint consistent")
			}
			for _, statement := range []string{
				`UPDATE analysis_detections SET profile_fingerprint=$2 WHERE item_id=$1`,
				`UPDATE analysis_intro_audit SET profile_fingerprint=$2 WHERE item_id=$1 AND action='qualified'`,
			} {
				if _, err := tx.Exec(f.ctx, statement, entry.item, entry.fingerprint); err != nil {
					t.Fatal("keep original qualified evidence bound to the substituted profile")
				}
			}
			if _, err := backuppg.InspectRecoveryTransaction(f.ctx, tx, f.engine.options.Schema); !errors.Is(err, backuppg.ErrSchema) {
				t.Fatalf("unavailable admission authorized otherwise valid qualified evidence: %v", err)
			}
		})
	}
}

func TestEngineMixedAnalysisHistoryRetainsV1EvidenceButRevokesItsExecutionProof(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newEngineRecoveryFixture(t)
	seedAnalysisRecoveryFixture(t, f)
	fingerprint := seedAnalysisRecoveryV1History(t, f)
	before := recoveryEngineJSONState(t, f.ctx, f.source, analysisRecoveryRetainedSQL)
	assertUnavailableProfilesRejectQualifiedHistory(t, f)
	if after := recoveryEngineJSONState(t, f.ctx, f.source, analysisRecoveryRetainedSQL); after != before {
		t.Fatal("isolated unavailable profile witnesses changed original history")
	}
	manifest, metadata := f.create(t)
	reader, err := f.objects.Snapshot(f.ctx, metadata.ID)
	if err != nil {
		t.Fatal("open mixed-version encrypted archive")
	}
	defer reader.Close()
	passphrase := []byte("recovery-integration-passphrase")
	defer clear(passphrase)
	archive, err := f.engine.OpenArchive(f.ctx, reader, passphrase)
	if err != nil {
		t.Fatalf("open archive retaining exact v1 wire: %v", err)
	}
	defer archive.Close()
	lease, err := database.AcquireLease(f.ctx, f.target)
	if err != nil {
		t.Fatal("acquire target recovery lease")
	}
	defer lease.Close()
	refused := errors.New("mixed-version raw history witness completed")
	called := false
	_, err = backuppg.RestoreFinalized(f.ctx, f.source, f.target, archive.Database(), manifest.Source, f.engine.options, func(ctx context.Context, tx pgx.Tx, raw backuppg.RestoreResult) error {
		called = true
		if !reflect.DeepEqual(raw.Tables, manifest.Source.Tables) {
			t.Error("mixed-version raw table fingerprints changed before normalization")
		}
		var exact bool
		if err := tx.QueryRow(ctx, `SELECT (SELECT publication_epoch=1 FROM analysis_settings WHERE id=1)
			AND (SELECT count(*) FROM analysis_run_profiles WHERE execution->>'Version'='1' AND fingerprint=$1 AND execution=$2::jsonb)=1
			AND (SELECT count(*) FROM analysis_run_profiles WHERE execution->>'Version'='1')=3
			AND (SELECT count(*) FROM analysis_run_profiles WHERE execution->>'Version'='2')=3
			AND (SELECT count(*) FROM analysis_run_profiles WHERE execution->'Available'='false'::jsonb AND execution->>'DetectorVersion'='')=2
			AND (SELECT count(*) FROM analysis_detections d JOIN analysis_work w ON w.child_id=d.child_id JOIN analysis_run_profiles p ON p.run_id=w.run_id
				WHERE d.status='no_result' AND d.result->>'Reason'='source_unavailable' AND NOT d.auto_published
				AND d.result->>'Version'='introdetect-v'||(p.execution->>'Version') AND p.execution->'Available'='false'::jsonb)=2
			AND (SELECT count(*) FROM analysis_detections WHERE auto_published)=2
			AND (SELECT result=$3::jsonb FROM analysis_detections WHERE item_id='analysis-restore-two')
			AND (SELECT count(*) FROM analysis_previews)=3 AND (SELECT count(*) FROM analysis_feature_cache)=1`,
			fingerprint, analysisRecoveryV1Execution, analysisRecoveryV1Result).Scan(&exact); err != nil || !exact {
			t.Error("raw restore lost a version, rehashed v1, or normalized before proof")
		}
		return refused
	})
	if !called || !errors.Is(err, refused) {
		t.Fatal("raw mixed-history witness did not roll back")
	}
	if _, err := archive.Database().Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the same authenticated mixed-version archive")
	}
	result, err := archive.RestoreInto(f.ctx, f.target, lease, f.targetConfig)
	if err != nil || result.AnalysisPublicationEpoch != 2 || result.DisabledAnalysisDetections != 2 || result.RemovedAnalysisPreviews != 3 || result.RemovedAnalysisFeatures != 1 {
		t.Fatalf("mixed-version invalidation counts=%+v error=%v", result, err)
	}
	if after := recoveryEngineJSONState(t, f.ctx, f.target, analysisRecoveryRetainedSQL); after != before {
		t.Fatal("normalization rewrote immutable v1/v2 admission, result, support, audit, or manual history")
	}
	var safe bool
	if err := f.target.QueryRow(f.ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM analysis_previews) AND NOT EXISTS(SELECT 1 FROM analysis_feature_cache)
		AND NOT EXISTS(SELECT 1 FROM analysis_detections WHERE auto_published)
		AND (SELECT publication_epoch=2 FROM analysis_settings WHERE id=1)
		AND (SELECT count(*) FROM task_runs WHERE id IN (repeat('5',32),repeat('6',32)) AND state='interrupted')=2
		AND (SELECT state='completed' AND analysis_config_fingerprint=$1 FROM task_runs WHERE id=repeat('9',32))
		AND (SELECT count(*) FROM task_runs WHERE id IN (repeat('a',32),repeat('b',32)) AND state='completed')=2
		AND (SELECT state='completed' FROM task_runs WHERE id=repeat('c',32))
		AND (SELECT count(*) FROM task_run_children WHERE run_id IN (repeat('5',32),repeat('6',32)) AND state='interrupted')=2
		AND (SELECT state='completed' FROM task_run_children WHERE run_id=repeat('9',32))`, fingerprint).Scan(&safe); err != nil || !safe {
		t.Fatal("restore retained an execution proof or resumed historical v1 work")
	}
	if err := f.target.QueryRow(f.ctx, `SELECT p.execution=$1::jsonb AND p.fingerprint=$2 AND r.analysis_config_fingerprint=$2
		FROM analysis_run_profiles p JOIN task_runs r ON r.id=p.run_id WHERE p.run_id=repeat('c',32)`,
		analysisRecoveryV1BarePreview, analysisRecoveryV1AdmissionFingerprint(analysisRecoveryV1BarePreview)).Scan(&safe); err != nil || !safe {
		t.Fatal("recovery rewrote an early preview profile or its canonical fingerprint")
	}
	var raw []byte
	if err := f.target.QueryRow(f.ctx, `SELECT result FROM analysis_detections WHERE item_id='analysis-restore-two'`).Scan(&raw); err != nil {
		t.Fatal("read retained historical result")
	}
	start, end := int64(50000000), int64(400000000)
	stored, err := library.ReadStoredAnalysisResult(raw, "qualified", &start, &end)
	if err != nil || stored.Version != "introdetect-v1" || stored.Episode.SourceKey != "source-two" || len(stored.Episode.Candidates) != 1 || len(stored.Episode.Candidates[0].Support) != 3 {
		t.Fatalf("neutral historical reader lost v1 evidence: %v", err)
	}
	for _, version := range []string{"v1", "v2"} {
		var resultRaw, auditRaw []byte
		if err := f.target.QueryRow(f.ctx, `SELECT d.result,a.evidence FROM analysis_detections d
			JOIN analysis_intro_audit a ON a.item_id=d.item_id WHERE d.item_id=$1`, "analysis-restore-unavailable-"+version).Scan(&resultRaw, &auditRaw); err != nil {
			t.Fatal("read restored unavailable result and audit")
		}
		abstention, err := library.ReadStoredAnalysisResult(resultRaw, "no_result", nil, nil)
		if err != nil || abstention.Version != "introdetect-"+version || abstention.Reason != "source_unavailable" ||
			abstention.Episode.ContentIdentity != "" || len(abstention.Episode.Candidates) != 0 || len(abstention.Episode.Reasons) != 0 {
			t.Fatalf("unavailable history acquired detector evidence: %v", err)
		}
		audit, err := library.ReadStoredAnalysisAudit(auditRaw, "no_result")
		if err != nil || audit.Result == nil || !reflect.DeepEqual(*audit.Result, abstention) || audit.Decision != nil {
			t.Fatalf("unavailable audit lost its original abstention: %v", err)
		}
	}
	if after := recoveryEngineJSONState(t, f.ctx, f.source, analysisRecoveryRetainedSQL); after != before {
		t.Fatal("mixed-version recovery changed its source history")
	}
}
