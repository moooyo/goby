//go:build linux

package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/library"
)

func analysisRecoveryV4Fixture(t *testing.T, name string) []byte {
	t.Helper()
	// Pin the complete retired v4 wire, not a serialization of current matcher types.
	pins := map[string]string{
		"intro":       "bab3d978992a00ef4144ed27066bfaa98f212bb9f7ca2f5de084f70b605aca14",
		"preview":     "5757ffc3c2c10a9ecb0bd07a1c95e88937b45f5a812e1fc6d82d1b41ca9c9df7",
		"unavailable": "02faf32c1033367a33e7e2ba9fce44074e9a15c0798597dded5b182e5a4f808b",
		"qualified":   "5d04a45a6983a4958144d9aa2d3e67fd59b957a029b48b198c6995e8f95f5805",
		"visual":      "272b6d7ad7393bdb9b36488601d757bec489494d28d9226a64afa483635263c0",
	}
	filename := "analysis-admission-v4-" + name + ".json"
	if name == "qualified" || name == "visual" {
		filename = "analysis-result-v4-" + name + ".json"
	}
	raw, err := os.ReadFile(filepath.Join("..", "library", "testdata", filename))
	digest := sha256.Sum256(raw)
	if err != nil || pins[name] == "" || hex.EncodeToString(digest[:]) != pins[name] {
		t.Fatal("frozen v4 recovery fixture changed")
	}
	if name == "visual" && (!strings.Contains(string(raw), `"VisualEvidence"`) || strings.Contains(string(raw), `"MeasurementPolicy"`) || strings.Contains(string(raw), `"CalibrationDigest"`)) {
		t.Fatal("frozen v4 visual fixture acquired current measurement claims")
	}
	return raw
}

func analysisRecoveryV4Admission(t *testing.T, name string) (string, string) {
	t.Helper()
	var envelope struct {
		Version         int
		Revision, Epoch int64
		Profile         json.RawMessage
		Execution       json.RawMessage
	}
	if err := json.Unmarshal(analysisRecoveryV4Fixture(t, name), &envelope); err != nil || envelope.Version != 4 ||
		envelope.Revision != 7 || envelope.Epoch != 3 || string(envelope.Profile) != analysisRecoveryV1Profile {
		t.Fatal("frozen v4 recovery envelope differs")
	}
	// Preserve the old wire and canonical order while using the fixture epoch.
	envelope.Revision, envelope.Epoch = 1, 1
	canonical, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal("encode the historical recovery admission")
	}
	digest := sha256.Sum256(canonical)
	fingerprint := hex.EncodeToString(digest[:])
	if library.ValidateStoredAnalysisAdmission(envelope.Profile, envelope.Execution, 1, 1, fingerprint) != nil {
		t.Fatal("frozen v4 recovery admission lost historical validity")
	}
	return string(envelope.Execution), fingerprint
}

func seedAnalysisRecoveryV4History(t *testing.T, f *engineRecoveryFixture) string {
	t.Helper()
	execution, fingerprint := analysisRecoveryV4Admission(t, "intro")
	for _, entry := range []struct {
		name, run, scope string
		end              int64
	}{
		{"qualified", strings.Repeat("7", 32), "b", 400000000},
		{"visual", strings.Repeat("8", 32), "c", 180000000},
	} {
		result := analysisRecoveryV4Fixture(t, entry.name)
		start, end := int64(100000000), entry.end
		facts, err := library.ReadStoredAnalysisResult(result, "qualified", &start, &end)
		if err != nil || facts.Version != "introdetect-v4" || len(facts.Episode.Candidates) != 1 || len(facts.Episode.Candidates[0].Support) != 3 {
			t.Fatal("frozen v4 qualification is not a valid historical fact")
		}
		run, scope := entry.run, "analysis:"+strings.Repeat(entry.scope, 64)
		childDigest := sha256.Sum256([]byte(run + ":" + scope))
		child := hex.EncodeToString(childDigest[:16])
		if _, err := f.source.Exec(f.ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
			analysis_input,analysis_config_fingerprint,total_children,terminal_children,completed_children,started_at,finished_at)
			VALUES($1,repeat('7',32),'completed','manual','retired-v4-user','retired-v4-session','admin','media.intro_analysis','Frozen v4 history',
			'{}',$2,1,1,1,'2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, run, fingerprint); err != nil {
			t.Fatal("seed completed v4 recovery task")
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)
			VALUES($1,1,1,$2,$3,$4)`, run, fingerprint, analysisRecoveryV1Profile, execution); err != nil {
			t.Fatal("seed frozen v4 recovery admission")
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key,state,started_at,finished_at)
			VALUES($1,$2,$3,'V4 history',0,$4,'completed','2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, child, run, f.libraryID, scope); err != nil {
			t.Fatal("seed completed v4 recovery child")
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_work(child_id,run_id,task_key,library_id,scope_key,cohort_revision,force)
			VALUES($1,$2,'media.intro_analysis',$3,$4,repeat('c',64),false)`, child, run, f.libraryID, scope); err != nil {
			t.Fatal("seed immutable v4 work")
		}
		for position, support := range facts.Episode.Candidates[0].Support {
			item := "analysis-restore-v4-" + entry.name + "-" + support.SourceKey
			if _, err := f.source.Exec(f.ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
				VALUES($1,$2,$1,$1,'Movie',false)`, item, f.libraryID); err != nil {
				t.Fatal("seed independent v4 history owner")
			}
			if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_work_sources(child_id,item_id,position,target,library_id,root_id,series_id,season_id,episode_key,item_type,
				source_revision,hierarchy_revision,duration_ticks,size,manual_revision,decision_revision,preview_revision)
				VALUES($1,$2,$3,$4,$5,'retired-root','retired-series','retired-season',$6,'Episode',$7,'retired-hierarchy',600000000,1024,0,0,0)`,
				child, item, position, support.SourceKey == facts.Episode.SourceKey, f.libraryID, support.EpisodeKey, support.SourceKey); err != nil {
				t.Fatal("seed immutable v4 source identity")
			}
		}
		item := "analysis-restore-v4-" + entry.name + "-" + facts.Episode.SourceKey
		if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,
			child_id,cohort_revision,status,result,start_ticks,end_ticks,auto_published)
			VALUES($1,1,$2,$3,1,1,$4,repeat('c',64),'qualified',$5,$6,$7,true)`, item, facts.Episode.SourceKey, fingerprint, child, result, start, end); err != nil {
			t.Fatal("seed unchanged v4 qualification metrics")
		}
		for _, support := range facts.Episode.Candidates[0].Support {
			if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_detection_sources(item_id,source_item_id,library_id,root_id,source_revision,hierarchy_revision,episode_key,content_sha256)
				VALUES($1,$2,$3,'retired-root',$4,'retired-hierarchy',$5,$6)`, item, "analysis-restore-v4-"+entry.name+"-"+support.SourceKey,
				f.libraryID, support.SourceKey, support.EpisodeKey, support.ContentIdentity); err != nil {
				t.Fatal("seed complete v4 support references")
			}
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_intro_audit(item_id,revision,source_revision,profile_fingerprint,action,evidence)
			VALUES($1,1,$2,$3,'qualified',$4)`, item, facts.Episode.SourceKey, fingerprint, `{"Result":`+string(result)+`,"Decision":null}`); err != nil {
			t.Fatal("seed untouched v4 qualified audit")
		}
	}
	preview, previewFingerprint := analysisRecoveryV4Admission(t, "preview")
	if _, err := f.source.Exec(f.ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
		analysis_input,analysis_config_fingerprint,started_at,finished_at)
		VALUES(repeat('0',32),repeat('8',32),'completed','manual','retired-v4-user','retired-v4-preview','admin','media.preview_generation','V4 preview history',
		'{}',$1,'2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, previewFingerprint); err != nil {
		t.Fatal("seed historical v4 preview task")
	}
	if _, err := f.source.Exec(f.ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)
		VALUES(repeat('0',32),1,1,$1,$2,$3)`, previewFingerprint, analysisRecoveryV1Profile, preview); err != nil {
		t.Fatal("seed the frozen v4 preview profile")
	}
	return fingerprint
}

func TestEngineV4AnalysisHistoryPreservesRawEvidenceBeforeRevokingAuthority(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newEngineRecoveryFixture(t)
	seedAnalysisRecoveryFixture(t, f)
	seedAnalysisRecoveryV1History(t, f)
	seedAnalysisRecoveryV2History(t, f)
	seedAnalysisRecoveryV3History(t, f)
	fingerprint := seedAnalysisRecoveryV4History(t, f)
	before := recoveryEngineJSONState(t, f.ctx, f.source, analysisRecoveryRetainedSQL)
	assertUnavailableProfilesRejectQualifiedHistory(t, f, true, true, true)
	if after := recoveryEngineJSONState(t, f.ctx, f.source, analysisRecoveryRetainedSQL); after != before {
		t.Fatal("rolled-back unavailable profile witnesses changed the source history")
	}
	manifest, metadata := f.create(t)
	reader, err := f.objects.Snapshot(f.ctx, metadata.ID)
	if err != nil {
		t.Fatal("open the mixed v1/v2/v3/v4/v5 encrypted archive")
	}
	defer reader.Close()
	passphrase := []byte("recovery-integration-passphrase")
	defer clear(passphrase)
	archive, err := f.engine.OpenArchive(f.ctx, reader, passphrase)
	if err != nil {
		t.Fatalf("open archive with frozen v4 semantics: %v", err)
	}
	defer archive.Close()
	lease, err := database.AcquireLease(f.ctx, f.target)
	if err != nil {
		t.Fatal("acquire target recovery lease")
	}
	defer lease.Close()
	execution, _ := analysisRecoveryV4Admission(t, "intro")
	preview, previewFingerprint := analysisRecoveryV4Admission(t, "preview")
	refused := errors.New("frozen v4 raw history witnessed before normalization")
	called := false
	_, err = backuppg.RestoreFinalized(f.ctx, f.source, f.target, archive.Database(), manifest.Source, f.engine.options, func(ctx context.Context, tx pgx.Tx, raw backuppg.RestoreResult) error {
		called = true
		if !reflect.DeepEqual(raw.Tables, manifest.Source.Tables) {
			t.Error("raw v4 table fingerprints changed before normalization")
		}
		var exact bool
		if err := tx.QueryRow(ctx, `SELECT (SELECT publication_epoch=1 FROM analysis_settings WHERE id=1)
			AND (SELECT execution=$1::jsonb AND fingerprint=$2 FROM analysis_run_profiles WHERE run_id=repeat('7',32))
			AND (SELECT analysis_config_fingerprint=$2 FROM task_runs WHERE id=repeat('7',32))
			AND (SELECT result=$3::jsonb AND auto_published FROM analysis_detections WHERE item_id='analysis-restore-v4-qualified-source-1')
			AND (SELECT evidence->'Result'=$3::jsonb FROM analysis_intro_audit WHERE item_id='analysis-restore-v4-qualified-source-1')
			AND (SELECT execution=$4::jsonb AND fingerprint=$5 FROM analysis_run_profiles WHERE run_id=repeat('0',32))
			AND (SELECT execution=$1::jsonb AND fingerprint=$2 FROM analysis_run_profiles WHERE run_id=repeat('8',32))
			AND (SELECT analysis_config_fingerprint=$2 FROM task_runs WHERE id=repeat('8',32))
			AND (SELECT result=$6::jsonb AND auto_published FROM analysis_detections WHERE item_id='analysis-restore-v4-visual-source-1')
			AND (SELECT evidence->'Result'=$6::jsonb FROM analysis_intro_audit WHERE item_id='analysis-restore-v4-visual-source-1')
			AND (SELECT count(*) FROM analysis_run_profiles WHERE execution->>'Version'='4')=4
			AND (SELECT count(*) FROM analysis_detections WHERE auto_published)=6
			AND (SELECT count(*) FROM analysis_previews)=3 AND (SELECT count(*) FROM analysis_feature_cache)=1`,
			execution, fingerprint, analysisRecoveryV4Fixture(t, "qualified"), preview, previewFingerprint, analysisRecoveryV4Fixture(t, "visual")).Scan(&exact); err != nil || !exact {
			t.Error("v4 evidence was rewritten, rehashed, or invalidated before raw verification")
		}
		return refused
	})
	if !called || !errors.Is(err, refused) {
		t.Fatal("raw v4 verification did not reach and roll back the finalizer witness")
	}
	if _, err := archive.Database().Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the same authenticated mixed archive")
	}
	result, err := archive.RestoreInto(f.ctx, f.target, lease, f.targetConfig)
	if err != nil || result.AnalysisPublicationEpoch != 2 || result.DisabledAnalysisDetections != 6 || result.RemovedAnalysisPreviews != 3 || result.RemovedAnalysisFeatures != 1 {
		t.Fatalf("v4 recovery failed to revoke every execution proof: %+v, %v", result, err)
	}
	if after := recoveryEngineJSONState(t, f.ctx, f.target, analysisRecoveryRetainedSQL); after != before {
		t.Fatal("normalization reinterpreted v4 wire, metrics, admission fingerprints, or explicit decisions")
	}
	var safe bool
	if err := f.target.QueryRow(f.ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM analysis_previews) AND NOT EXISTS(SELECT 1 FROM analysis_feature_cache)
		AND NOT EXISTS(SELECT 1 FROM analysis_detections WHERE auto_published)
		AND (SELECT publication_epoch=2 FROM analysis_settings WHERE id=1)
		AND (SELECT count(*) FROM task_runs WHERE id IN (repeat('7',32),repeat('8',32)) AND state='completed' AND analysis_config_fingerprint=$1)=2
		AND (SELECT state='completed' FROM task_runs WHERE id=repeat('0',32))
		AND (SELECT count(*) FROM task_run_children WHERE run_id IN (repeat('7',32),repeat('8',32)) AND state='completed' AND executor_token IS NULL)=2`, fingerprint).Scan(&safe); err != nil || !safe {
		t.Fatal("restored v4 history retained publication authority or resumed historical work")
	}
	for _, name := range []string{"intro", "preview", "unavailable"} {
		execution, _ := analysisRecoveryV4Admission(t, name)
		var live library.AnalysisExecutionProfile
		if json.Unmarshal([]byte(execution), &live) != nil || !errors.Is(library.ValidateAnalysisExecutionProfile(live), library.ErrInvalidInput) {
			t.Fatalf("restored v4 %s admission was promoted into current worker authority", name)
		}
	}
	if after := recoveryEngineJSONState(t, f.ctx, f.source, analysisRecoveryRetainedSQL); after != before {
		t.Fatal("v4 recovery changed the original source history")
	}
}
