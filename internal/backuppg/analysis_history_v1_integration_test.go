//go:build linux

package backuppg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/library"
)

// These complete wire objects and their field order are frozen from 92577f2.
// Do not marshal a current profile or matcher type to construct v1 history.
const analysisArchiveV1Profile = `{"AutoPublishIntros":true,"PreviewIntervalSeconds":10,"PreviewQuality":80,"MaxSourceBytes":137438953472,"MaxItemRuntimeSeconds":1200,"FeatureCacheMaxBytes":134217728}`
const analysisArchiveV1Unavailable = `{"Version":1,"Available":false,"UnavailableReason":"not_configured","FFmpegSHA256":"","FFprobeSHA256":"","FingerprintSHA256":"","DetectorVersion":"","DetectorOptions":{"MaxEpisodes":0,"MaxAudioSamples":0,"MaxVisualSamples":0,"MaxFeatureBytes":0,"MaxComparisons":0,"MaxOffsetCandidates":0,"MaxCandidatesPerPair":0,"MaxGroups":0,"WindowTicks":0,"MinDurationTicks":0,"MaxDurationTicks":0,"AutoMinDurationTicks":0,"OffsetBinTicks":0,"AudioAlignmentTicks":0,"MaxAudioGapTicks":0,"VisualAlignmentTicks":0,"MaxVisualGapTicks":0,"BoundaryToleranceTicks":0,"MinSupport":0,"MaxAudioHamming":0,"MaxVisualHamming":0,"MinAudioAgreement":0,"MinAudioInformation":0,"MinVisualAgreement":0,"MinAudioSimilarity":0,"MinVisualSimilarity":0,"MinVisualContrast":0,"MinVisualSamples":0,"MinVisualTransitions":0,"MinVisualChangeCoverage":0,"MaxVisualDominance":0},"VisualIntervalTicks":0,"PreviewProfile":"","PreviewWidths":null,"IntroProfile":""}`

// The earlier schema-50 preview contract predates the geometry suffix.
const analysisArchiveV1BarePreview = `{"Version":1,"Available":true,"UnavailableReason":"","FFmpegSHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","FFprobeSHA256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","FingerprintSHA256":"","DetectorVersion":"","DetectorOptions":{"MaxEpisodes":0,"MaxAudioSamples":0,"MaxVisualSamples":0,"MaxFeatureBytes":0,"MaxComparisons":0,"MaxOffsetCandidates":0,"MaxCandidatesPerPair":0,"MaxGroups":0,"WindowTicks":0,"MinDurationTicks":0,"MaxDurationTicks":0,"AutoMinDurationTicks":0,"OffsetBinTicks":0,"AudioAlignmentTicks":0,"MaxAudioGapTicks":0,"VisualAlignmentTicks":0,"MaxVisualGapTicks":0,"BoundaryToleranceTicks":0,"MinSupport":0,"MaxAudioHamming":0,"MaxVisualHamming":0,"MinAudioAgreement":0,"MinAudioInformation":0,"MinVisualAgreement":0,"MinAudioSimilarity":0,"MinVisualSimilarity":0,"MinVisualContrast":0,"MinVisualSamples":0,"MinVisualTransitions":0,"MinVisualChangeCoverage":0,"MaxVisualDominance":0},"VisualIntervalTicks":0,"PreviewProfile":"source-pts-display-preceding-hold-jpeg-v3","PreviewWidths":[240,320,400],"IntroProfile":""}`
const analysisArchiveV1Execution = `{"Version":1,"Available":true,"UnavailableReason":"","FFmpegSHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","FFprobeSHA256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","FingerprintSHA256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","DetectorVersion":"introdetect-v1","DetectorOptions":{"MaxEpisodes":32,"MaxAudioSamples":6000,"MaxVisualSamples":2400,"MaxFeatureBytes":262144,"MaxComparisons":100000000,"MaxOffsetCandidates":6,"MaxCandidatesPerPair":12,"MaxGroups":128,"WindowTicks":6000000000,"MinDurationTicks":150000000,"MaxDurationTicks":1800000000,"AutoMinDurationTicks":300000000,"OffsetBinTicks":5000000,"AudioAlignmentTicks":3333333,"MaxAudioGapTicks":10000000,"VisualAlignmentTicks":10000000,"MaxVisualGapTicks":30000000,"BoundaryToleranceTicks":30000000,"MinSupport":3,"MaxAudioHamming":6,"MaxVisualHamming":10,"MinAudioAgreement":900,"MinAudioInformation":600,"MinVisualAgreement":850,"MinAudioSimilarity":850,"MinVisualSimilarity":850,"MinVisualContrast":40,"MinVisualSamples":8,"MinVisualTransitions":3,"MinVisualChangeCoverage":300,"MaxVisualDominance":600},"VisualIntervalTicks":5000000,"PreviewProfile":"","PreviewWidths":null,"IntroProfile":"archive-intro-v1"}`
const analysisArchiveV1Result = `{"Version":"introdetect-v1","Episode":{"EpisodeKey":"","SourceKey":"retired-source","ContentIdentity":"","Status":"no_result","Reasons":[],"Candidates":[]},"Reason":"source_unavailable"}`

func seedAnalysisArchiveV1History(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	canonical := `{"Version":1,"Revision":1,"Epoch":1,"Profile":` + analysisArchiveV1Profile + `,"Execution":` + analysisArchiveV1Execution + `}`
	digest := sha256.Sum256([]byte(canonical))
	fingerprint := hex.EncodeToString(digest[:])
	run, scope := strings.Repeat("e", 32), "analysis:"+strings.Repeat("1", 64)
	childDigest := sha256.Sum256([]byte(run + ":" + scope))
	child := hex.EncodeToString(childDigest[:16])
	if _, err := pool.Exec(ctx, `INSERT INTO task_definitions(id,key,name) VALUES(repeat('f',32),'media.intro_analysis','Historical intro analysis')`); err != nil {
		t.Fatal("seed historical intro definition")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
		analysis_input,analysis_config_fingerprint,total_children,terminal_children,completed_children,started_at,finished_at)
		VALUES($1,repeat('f',32),'completed','manual','backup-admin','retired-v1-session','admin','media.intro_analysis','Historical intro analysis',
		'{}',$2,1,1,1,'2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, run, fingerprint); err != nil {
		t.Fatal("seed historical task without restoring its authority")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)
		VALUES($1,1,1,$2,$3,$4)`, run, fingerprint, analysisArchiveV1Profile, analysisArchiveV1Execution); err != nil {
		t.Fatal("seed the original v1 admission fingerprint")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key,state,started_at,finished_at)
		VALUES($1,$2,'analysis-history-library','Analysis history',0,$3,'completed','2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, child, run, scope); err != nil {
		t.Fatal("seed historical child")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_work(child_id,run_id,task_key,library_id,scope_key,cohort_revision,force)
		VALUES($1,$2,'media.intro_analysis','analysis-history-library',$3,repeat('d',64),false)`, child, run, scope); err != nil {
		t.Fatal("seed historical work")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_work_sources(child_id,item_id,position,target,library_id,root_id,series_id,season_id,episode_key,item_type,
		source_revision,hierarchy_revision,duration_ticks,size,manual_revision,decision_revision,preview_revision)
		VALUES($1,'analysis-history-item',0,true,'analysis-history-library','retired-root','','','','Movie','retired-source','retired-hierarchy',300000000,1024,0,0,3)`, child); err != nil {
		t.Fatal("seed historical source facts")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,
		child_id,cohort_revision,status,result) VALUES('analysis-history-item',1,'retired-source',$1,1,1,$2,repeat('d',64),'no_result',$3)`, fingerprint, child, analysisArchiveV1Result); err != nil {
		t.Fatal("seed a literal v1 abstention without fabricated support")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_intro_audit(item_id,revision,source_revision,profile_fingerprint,action,evidence)
		VALUES('analysis-history-item',1,'retired-source',$1,'no_result',$2)`, fingerprint, `{"Result":`+analysisArchiveV1Result+`,"Decision":null}`); err != nil {
		t.Fatal("seed original v1 audit evidence")
	}
	// Unavailable admissions retain only an abstention from their own envelope.
	unavailableCanonical := `{"Version":1,"Revision":1,"Epoch":1,"Profile":` + analysisArchiveV1Profile + `,"Execution":` + analysisArchiveV1Unavailable + `}`
	unavailableDigest := sha256.Sum256([]byte(unavailableCanonical))
	currentUnavailable, currentFingerprint := analysisArchiveCurrentAdmission(t, library.AnalysisExecutionProfile{Version: library.AnalysisExecutionProfileVersion, UnavailableReason: "not_configured"})
	v2Unavailable, v2Fingerprint := analysisArchiveV2Admission(t, "unavailable")
	for _, entry := range []struct{ version, run, scope, execution, fingerprint string }{
		{"v1", strings.Repeat("d", 32), "analysis:" + strings.Repeat("2", 64), analysisArchiveV1Unavailable, hex.EncodeToString(unavailableDigest[:])},
		{"v2", strings.Repeat("4", 32), "analysis:" + strings.Repeat("5", 64), v2Unavailable, v2Fingerprint},
		{"v3", strings.Repeat("c", 32), "analysis:" + strings.Repeat("3", 64), currentUnavailable, currentFingerprint},
	} {
		item, source := "analysis-history-unavailable-"+entry.version, "unavailable-source-"+entry.version
		childDigest := sha256.Sum256([]byte(entry.run + ":" + entry.scope))
		child := hex.EncodeToString(childDigest[:16])
		result := strings.ReplaceAll(strings.ReplaceAll(analysisArchiveV1Result, "retired-source", source), "introdetect-v1", "introdetect-"+entry.version)
		if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
			VALUES($1,'analysis-history-library',$1,$1,'Movie',false)`, item); err != nil {
			t.Fatal("seed an independent unavailable source owner")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
			analysis_input,analysis_config_fingerprint,total_children,terminal_children,completed_children,started_at,finished_at)
			VALUES($1,repeat('f',32),'completed','manual','backup-admin','retired-session','admin','media.intro_analysis','Unavailable history',
			'{}',$2,1,1,1,'2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, entry.run, entry.fingerprint); err != nil {
			t.Fatal("seed unavailable admission history")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)
			VALUES($1,1,1,$2,$3,$4)`, entry.run, entry.fingerprint, analysisArchiveV1Profile, entry.execution); err != nil {
			t.Fatal("retain the original unavailable canonical profile")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key,state,started_at,finished_at)
			VALUES($1,$2,'analysis-history-library','Analysis history',0,$3,'completed','2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, child, entry.run, entry.scope); err != nil {
			t.Fatal("seed the unavailable history child")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO analysis_work(child_id,run_id,task_key,library_id,scope_key,cohort_revision,force)
			VALUES($1,$2,'media.intro_analysis','analysis-history-library',$3,repeat('d',64),false)`, child, entry.run, entry.scope); err != nil {
			t.Fatal("seed unavailable source work")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO analysis_work_sources(child_id,item_id,position,target,library_id,root_id,series_id,season_id,episode_key,item_type,
			source_revision,hierarchy_revision,duration_ticks,size,manual_revision,decision_revision,preview_revision)
			VALUES($1,$2,0,true,'analysis-history-library','retired-root','','','','Movie',$3,'retired-hierarchy',300000000,1024,0,0,0)`, child, item, source); err != nil {
			t.Fatal("seed unavailable source facts without content evidence")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,
			child_id,cohort_revision,status,result) VALUES($1,1,$2,$3,1,1,$4,repeat('d',64),'no_result',$5)`, item, source, entry.fingerprint, child, result); err != nil {
			t.Fatal("seed a safe unavailable abstention")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO analysis_intro_audit(item_id,revision,source_revision,profile_fingerprint,action,evidence)
			VALUES($1,1,$2,$3,'no_result',$4)`, item, source, entry.fingerprint, `{"Result":`+result+`,"Decision":null}`); err != nil {
			t.Fatal("seed safe unavailable audit evidence")
		}
	}
	bareFingerprint := analysisArchiveV1AdmissionFingerprint(analysisArchiveV1BarePreview)
	if _, err := pool.Exec(ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
		analysis_input,analysis_config_fingerprint,started_at,finished_at)
		VALUES(repeat('1',32),repeat('a',32),'completed','manual','backup-admin','retired-preview-session','admin','media.preview_generation','Early preview history',
		'{}',$1,'2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, bareFingerprint); err != nil {
		t.Fatal("seed the early preview admission without derivative files")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)
		VALUES(repeat('1',32),1,1,$1,$2,$3)`, bareFingerprint, analysisArchiveV1Profile, analysisArchiveV1BarePreview); err != nil {
		t.Fatal("retain the original bare preview profile and fingerprint")
	}
	return fingerprint
}

func analysisArchiveV1AdmissionFingerprint(execution string) string {
	canonical := `{"Version":1,"Revision":1,"Epoch":1,"Profile":` + analysisArchiveV1Profile + `,"Execution":` + execution + `}`
	digest := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(digest[:])
}

func analysisArchiveCurrentAdmission(t *testing.T, execution library.AnalysisExecutionProfile) (string, string) {
	t.Helper()
	raw, err := json.Marshal(execution)
	if err != nil {
		t.Fatal("encode the current execution fixture")
	}
	canonical, err := json.Marshal(struct {
		Version         int
		Revision, Epoch int64
		Profile         library.AnalysisProfile
		Execution       library.AnalysisExecutionProfile
	}{library.AnalysisExecutionProfileVersion, 1, 1, library.DefaultAnalysisProfile(), execution})
	if err != nil {
		t.Fatal("encode the current canonical admission")
	}
	digest := sha256.Sum256(canonical)
	return string(raw), hex.EncodeToString(digest[:])
}

func TestPostgreSQLAnalysisMixedHistoryPreservesV1WireThroughRawRestoreAndRecoveryInspection(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedAnalysisArchiveState(t, ctx, source)
	fingerprint := seedAnalysisArchiveV1History(t, ctx, source)
	const witness = `SELECT jsonb_build_object(
		'profiles',(SELECT jsonb_agg(to_jsonb(p) ORDER BY run_id) FROM analysis_run_profiles p),
		'runs',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM task_runs r WHERE analysis_input IS NOT NULL),
		'children',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM task_run_children c WHERE analysis_scope_key<>''),
		'work',(SELECT jsonb_agg(to_jsonb(w) ORDER BY child_id) FROM analysis_work w),
		'sources',(SELECT jsonb_agg(to_jsonb(s) ORDER BY child_id,position) FROM analysis_work_sources s),
		'detections',(SELECT jsonb_agg(to_jsonb(d) ORDER BY item_id) FROM analysis_detections d),
		'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM analysis_intro_audit a))::text`
	var before string
	if err := source.QueryRow(ctx, witness).Scan(&before); err != nil {
		t.Fatal("capture mixed-version raw history")
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	// Recoverydb consumes this reader before trusting a retained database slot.
	tx, err := source.Begin(ctx)
	if err != nil {
		t.Fatal("begin mixed-history recovery inspection")
	}
	defer rollback(tx)
	// The generic backup fixture owns a random schema and leaves default public
	// in place. Whole-database recovery inspection requires no outside schema.
	// Remove only empty, owned public inside this never-committed transaction.
	const publicNamespace = `SELECT to_jsonb(n)::text,
		pg_catalog.pg_has_role(n.nspowner,'USAGE') AND EXISTS(SELECT 1 FROM pg_catalog.pg_database
			WHERE datname=current_database() AND pg_catalog.pg_has_role(datdba,'USAGE')),
		NOT EXISTS(SELECT 1 FROM pg_catalog.pg_depend WHERE refclassid='pg_catalog.pg_namespace'::regclass AND refobjid=n.oid)
		AND NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class WHERE relnamespace=n.oid)
		AND NOT EXISTS(SELECT 1 FROM pg_catalog.pg_proc WHERE pronamespace=n.oid)
		AND NOT EXISTS(SELECT 1 FROM pg_catalog.pg_type WHERE typnamespace=n.oid)
		FROM pg_catalog.pg_namespace n WHERE n.nspname='public'`
	var publicBefore string
	var publicOwned, publicEmpty bool
	if err := tx.QueryRow(ctx, publicNamespace).Scan(&publicBefore, &publicOwned, &publicEmpty); err != nil || !publicOwned || !publicEmpty || options.Schema == "public" {
		t.Fatal("inspection requires the fixture database's empty, owned default public schema")
	}
	if _, err := tx.Exec(ctx, `DROP SCHEMA public RESTRICT`); err != nil {
		t.Fatalf("isolate recovery inspection without removing any schema contents: %v", err)
	}
	inspection, err := InspectRecoveryTransaction(ctx, tx, options.Schema)
	if err != nil {
		t.Fatalf("recovery reader rejected literal v1 history: %v", err)
	}
	if err := inspection.LockTables(ctx); err != nil {
		t.Fatalf("lock exact mixed-version recovery facts: %v", err)
	}
	inspected, err := inspection.Facts(ctx, facts.ProbeVersion)
	if err != nil || !reflect.DeepEqual(inspected.Tables, facts.Tables) {
		t.Fatalf("recovery reader changed mixed-version table facts: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal("roll back the isolated recovery inspection")
	}
	var publicAfter string
	if err := source.QueryRow(ctx, publicNamespace).Scan(&publicAfter, &publicOwned, &publicEmpty); err != nil || publicAfter != publicBefore || !publicOwned || !publicEmpty {
		t.Fatal("inspection rollback did not restore the exact public namespace OID, owner and ACL")
	}
	if _, err := Restore(ctx, source, target, archive, facts, options); err != nil {
		t.Fatalf("restore verified mixed-version analysis history: %v", err)
	}
	var after string
	if err := target.QueryRow(ctx, witness).Scan(&after); err != nil || before != after {
		t.Fatal("raw restore rewrote a historical fingerprint, result, audit, or task")
	}
	var exact bool
	if err := target.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM analysis_run_profiles WHERE execution->>'Version'='1' AND fingerprint=$1 AND profile=$2::jsonb AND execution=$3::jsonb)=1
		AND (SELECT count(*) FROM analysis_run_profiles WHERE execution->>'Version'='1')=3
		AND (SELECT count(*) FROM analysis_run_profiles WHERE execution->>'Version'='1' AND execution->'Available'='false'::jsonb)=1
		AND (SELECT count(*) FROM analysis_run_profiles WHERE execution->>'Version'='2')=1
		AND (SELECT count(*) FROM analysis_run_profiles WHERE execution->>'Version'='3')=2
		AND (SELECT analysis_config_fingerprint=$1 FROM task_runs WHERE id=repeat('e',32))
		AND (SELECT result=$4::jsonb AND NOT auto_published FROM analysis_detections WHERE item_id='analysis-history-item')
		AND (SELECT evidence->'Result'=$4::jsonb FROM analysis_intro_audit WHERE item_id='analysis-history-item')`,
		fingerprint, analysisArchiveV1Profile, analysisArchiveV1Execution, analysisArchiveV1Result).Scan(&exact); err != nil || !exact {
		t.Fatal("mixed-version restore upgraded or lost original v1 evidence")
	}
	if err := target.QueryRow(ctx, `SELECT p.execution=$1::jsonb AND p.fingerprint=$2 AND r.analysis_config_fingerprint=$2
		FROM analysis_run_profiles p JOIN task_runs r ON r.id=p.run_id WHERE p.run_id=repeat('1',32)`,
		analysisArchiveV1BarePreview, analysisArchiveV1AdmissionFingerprint(analysisArchiveV1BarePreview)).Scan(&exact); err != nil || !exact {
		t.Fatal("raw restore appended geometry claims to an early preview admission")
	}
	if err := target.QueryRow(ctx, `SELECT count(*)=3 FROM analysis_detections d
		JOIN analysis_work w ON w.child_id=d.child_id JOIN analysis_run_profiles p ON p.run_id=w.run_id
		WHERE d.item_id IN ('analysis-history-unavailable-v1','analysis-history-unavailable-v2','analysis-history-unavailable-v3')
		AND p.execution->'Available'='false'::jsonb AND p.execution->>'DetectorVersion'=''
		AND d.result->>'Version'='introdetect-v'||(p.execution->>'Version')
		AND d.status='no_result' AND d.result->>'Reason'='source_unavailable' AND NOT d.auto_published
		AND d.result->'Episode'->>'ContentIdentity'='' AND d.result->'Episode'->'Candidates'='[]'::jsonb
		AND d.result->'Episode'->'Reasons'='[]'::jsonb
		AND NOT EXISTS(SELECT 1 FROM analysis_detection_sources s WHERE s.item_id=d.item_id)`).Scan(&exact); err != nil || !exact {
		t.Fatal("raw restore lost safe unavailable abstentions or fabricated detector evidence")
	}
}

func TestPostgreSQLAnalysisHistoryRequiresMatchingAdmissionAndResultVersions(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	seedAnalysisArchiveState(t, ctx, source)
	v1Fingerprint := seedAnalysisArchiveV1History(t, ctx, source)
	if introdetect.Version != "introdetect-v3" || library.AnalysisExecutionProfileVersion != 3 {
		t.Fatal("mixed-version witness requires the current v3 execution contract")
	}
	execution := library.AnalysisExecutionProfile{Version: library.AnalysisExecutionProfileVersion, Available: true,
		FFmpegSHA256: strings.Repeat("a", 64), FFprobeSHA256: strings.Repeat("b", 64), FingerprintSHA256: strings.Repeat("c", 64),
		DetectorVersion: introdetect.Version, DetectorOptions: introdetect.DefaultOptions(), VisualIntervalTicks: 5000000, IntroProfile: "archive-intro-v3"}
	currentRaw, currentFingerprint := analysisArchiveCurrentAdmission(t, execution)
	v2Raw, v2Fingerprint := analysisArchiveV2Admission(t, "intro")
	for _, version := range []string{"v1", "v2", "v3"} {
		raw := strings.Replace(analysisArchiveV1Result, "introdetect-v1", "introdetect-"+version, 1)
		if err := library.ValidateStoredAnalysisResult([]byte(raw), "no_result", nil, nil); err != nil {
			t.Fatalf("cross-version witness must use independently valid result shapes: %v", err)
		}
	}
	if err := library.ValidateStoredAnalysisAdmission([]byte(analysisArchiveV1Profile), []byte(currentRaw), 1, 1, currentFingerprint); err != nil {
		t.Fatalf("cross-version witness must use a valid current fingerprint: %v", err)
	}
	v2Unavailable, v2UnavailableFingerprint := analysisArchiveV2Admission(t, "unavailable")
	currentUnavailable, currentUnavailableFingerprint := analysisArchiveCurrentAdmission(t, library.AnalysisExecutionProfile{Version: library.AnalysisExecutionProfileVersion, UnavailableReason: "not_configured"})
	var tests []struct {
		name, execution, fingerprint, result string
		valid                                bool
	}
	for _, admission := range []struct{ name, version, execution, fingerprint string }{
		{"v1_admission", "v1", analysisArchiveV1Execution, v1Fingerprint},
		{"v2_admission", "v2", v2Raw, v2Fingerprint},
		{"v3_admission", "v3", currentRaw, currentFingerprint},
		{"unavailable_v1_admission", "v1", analysisArchiveV1Unavailable, analysisArchiveV1AdmissionFingerprint(analysisArchiveV1Unavailable)},
		{"unavailable_v2_admission", "v2", v2Unavailable, v2UnavailableFingerprint},
		{"unavailable_v3_admission", "v3", currentUnavailable, currentUnavailableFingerprint},
	} {
		for _, version := range []string{"v1", "v2", "v3"} {
			tests = append(tests, struct {
				name, execution, fingerprint, result string
				valid                                bool
			}{admission.name + "_" + version + "_result", admission.execution, admission.fingerprint,
				strings.Replace(analysisArchiveV1Result, "introdetect-v1", "introdetect-"+version, 1), admission.version == version})
		}
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal("begin isolated version binding witness")
			}
			defer rollback(tx)
			if err := configureTransaction(ctx, tx, options.Schema); err != nil {
				t.Fatal("configure version binding witness")
			}
			if _, err := tx.Exec(ctx, `ALTER TABLE analysis_run_profiles DISABLE TRIGGER analysis_profile_immutable`); err != nil {
				t.Fatal("allow the isolated profile binding fixture")
			}
			if _, err := tx.Exec(ctx, `UPDATE analysis_run_profiles SET execution=$1,fingerprint=$2 WHERE run_id=repeat('e',32)`, test.execution, test.fingerprint); err != nil {
				t.Fatal("select the independently valid execution profile")
			}
			for _, statement := range []string{
				`UPDATE task_runs SET analysis_config_fingerprint=$1 WHERE id=repeat('e',32)`,
				`UPDATE analysis_detections SET profile_fingerprint=$1 WHERE item_id='analysis-history-item'`,
				`UPDATE analysis_intro_audit SET profile_fingerprint=$1 WHERE item_id='analysis-history-item'`,
			} {
				if _, err := tx.Exec(ctx, statement, test.fingerprint); err != nil {
					t.Fatalf("bind all references to one canonical admission: %v", err)
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE analysis_detections SET result=$1 WHERE item_id='analysis-history-item'`, test.result); err != nil {
				t.Fatal("select the independently valid result version")
			}
			if _, err := tx.Exec(ctx, `UPDATE analysis_intro_audit SET evidence=$1 WHERE item_id='analysis-history-item'`, `{"Result":`+test.result+`,"Decision":null}`); err != nil {
				t.Fatal("retain the matching historical audit result")
			}
			err = validateAnalysisState(ctx, tx, 50)
			if test.valid && err != nil || !test.valid && !errors.Is(err, ErrSchema) {
				t.Fatalf("version binding validity=%v result=%v", test.valid, err)
			}
		})
	}
}

func TestPostgreSQLAnalysisV1HistoryStillRejectsUnknownWireAndReboundFingerprints(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	seedAnalysisArchiveState(t, ctx, source)
	seedAnalysisArchiveV1History(t, ctx, source)
	for _, test := range []struct{ name, mutation string }{
		{"unknown_v1_option", `ALTER TABLE analysis_run_profiles DISABLE TRIGGER analysis_profile_immutable; UPDATE analysis_run_profiles SET execution=jsonb_set(execution,'{DetectorOptions,UnknownV2Field}','1') WHERE run_id=repeat('e',32)`},
		{"changed_canonical_fingerprint", `ALTER TABLE analysis_run_profiles DISABLE TRIGGER analysis_profile_immutable; UPDATE analysis_run_profiles SET fingerprint=repeat('9',64) WHERE run_id=repeat('e',32); UPDATE task_runs SET analysis_config_fingerprint=repeat('9',64) WHERE id=repeat('e',32); UPDATE analysis_detections SET profile_fingerprint=repeat('9',64) WHERE item_id='analysis-history-item'; UPDATE analysis_intro_audit SET profile_fingerprint=repeat('9',64) WHERE item_id='analysis-history-item'`},
		{"unknown_result_version", `UPDATE analysis_detections SET result=jsonb_set(result,'{Version}','"introdetect-v999"') WHERE item_id='analysis-history-item'`},
		{"unknown_audit_version", `UPDATE analysis_intro_audit SET evidence=jsonb_set(evidence,'{Result,Version}','"introdetect-v999"') WHERE item_id='analysis-history-item'`},
		{"fabricated_abstention_support", `INSERT INTO analysis_detection_sources(item_id,source_item_id,library_id,root_id,source_revision,hierarchy_revision,episode_key,content_sha256) VALUES('analysis-history-item','analysis-history-item','analysis-history-library','retired-root','retired-source','retired-hierarchy','',repeat('a',64))`},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal("begin isolated historical evidence corruption")
			}
			defer rollback(tx)
			if err := configureTransaction(ctx, tx, options.Schema); err != nil {
				t.Fatal("configure historical evidence validation")
			}
			if _, err := tx.Exec(ctx, test.mutation); err != nil {
				t.Fatalf("apply bounded historical corruption: %v", err)
			}
			if err := validateAnalysisState(ctx, tx, 50); !errors.Is(err, ErrSchema) {
				t.Fatalf("historical compatibility bypassed semantic validation: %v", err)
			}
		})
	}
	t.Run("unknown_historical_preview_profile", func(t *testing.T) {
		tx, err := source.Begin(ctx)
		if err != nil {
			t.Fatal("begin isolated historical preview mutation")
		}
		defer rollback(tx)
		if err := configureTransaction(ctx, tx, options.Schema); err != nil {
			t.Fatal("configure historical preview validation")
		}
		unknown := strings.Replace(analysisArchiveV1BarePreview, "source-pts-display-preceding-hold-jpeg-v3", "source-pts-display-preceding-hold-jpeg-v3;unknown-profile", 1)
		fingerprint := analysisArchiveV1AdmissionFingerprint(unknown)
		if _, err := tx.Exec(ctx, `ALTER TABLE analysis_run_profiles DISABLE TRIGGER analysis_profile_immutable`); err != nil {
			t.Fatal("allow the isolated preview wire fixture")
		}
		if _, err := tx.Exec(ctx, `UPDATE analysis_run_profiles SET execution=$1,fingerprint=$2 WHERE run_id=repeat('1',32)`, unknown, fingerprint); err != nil {
			t.Fatal("bind the unknown preview literal to its own canonical fingerprint")
		}
		if _, err := tx.Exec(ctx, `UPDATE task_runs SET analysis_config_fingerprint=$1 WHERE id=repeat('1',32)`, fingerprint); err != nil {
			t.Fatal("keep the preview admission fingerprint references consistent")
		}
		if err := validateAnalysisState(ctx, tx, 50); !errors.Is(err, ErrSchema) {
			t.Fatalf("historical preview compatibility accepted an unknown literal: %v", err)
		}
	})
}

func TestPostgreSQLUnavailableAnalysisHistoryCannotAcquireMatcherEvidence(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	seedAnalysisArchiveState(t, ctx, source)
	seedAnalysisArchiveV1History(t, ctx, source)
	for _, version := range []string{"v1", "v2", "v3"} {
		for _, test := range []struct{ name, mutation string }{
			{"qualified", `UPDATE analysis_detections SET status='qualified',start_ticks=0,end_ticks=300000000,result=jsonb_set(result,'{Episode,Status}','"qualified"') WHERE item_id=$1`},
			{"content_identity", `UPDATE analysis_detections SET result=jsonb_set(result,'{Episode,ContentIdentity}',to_jsonb(repeat('a',64))) WHERE item_id=$1`},
			{"candidate", `UPDATE analysis_detections SET result=jsonb_set(result,'{Episode,Candidates}','[{}]') WHERE item_id=$1`},
			{"matcher_reason", `UPDATE analysis_detections SET result=jsonb_set(result,'{Episode,Reasons}','["missing_audio"]') WHERE item_id=$1`},
			{"missing_abstention_reason", `UPDATE analysis_detections SET result=jsonb_set(result,'{Reason}','""') WHERE item_id=$1`},
		} {
			t.Run(version+"/"+test.name, func(t *testing.T) {
				tx, err := source.Begin(ctx)
				if err != nil {
					t.Fatal("begin isolated unavailable evidence mutation")
				}
				defer rollback(tx)
				if err := configureTransaction(ctx, tx, options.Schema); err != nil {
					t.Fatal("configure unavailable evidence validation")
				}
				if err := validateAnalysisState(ctx, tx, 50); err != nil {
					t.Fatalf("safe unavailable history must be accepted before mutation: %v", err)
				}
				if _, err := tx.Exec(ctx, test.mutation, "analysis-history-unavailable-"+version); err != nil {
					t.Fatalf("apply the isolated fabricated evidence field: %v", err)
				}
				if err := validateAnalysisState(ctx, tx, 50); !errors.Is(err, ErrSchema) {
					t.Fatalf("unavailable admission acquired matcher evidence: %v", err)
				}
			})
		}
	}
}
