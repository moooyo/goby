//go:build linux

package backuppg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/library"
)

// These hashes pin complete retired v4 wire fixtures, including the original
// Options field order and visual evidence without v5 measurement policy claims.
func analysisArchiveV4Fixture(t *testing.T, name string) []byte {
	t.Helper()
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
		t.Fatal("frozen v4 archive fixture changed")
	}
	if name == "visual" && (!strings.Contains(string(raw), `"VisualEvidence"`) || strings.Contains(string(raw), `"MeasurementPolicy"`) || strings.Contains(string(raw), `"CalibrationDigest"`)) {
		t.Fatal("frozen v4 visual fixture acquired current measurement claims")
	}
	return raw
}

func analysisArchiveV4Admission(t *testing.T, name string) (string, string) {
	t.Helper()
	var envelope struct {
		Version         int
		Revision, Epoch int64
		Profile         json.RawMessage
		Execution       json.RawMessage
	}
	if err := json.Unmarshal(analysisArchiveV4Fixture(t, name), &envelope); err != nil || envelope.Version != 4 ||
		envelope.Revision != 7 || envelope.Epoch != 3 || string(envelope.Profile) != analysisArchiveV1Profile {
		t.Fatal("frozen v4 admission envelope differs")
	}
	// Only fixture-local revision and epoch change; no current typed profile or
	// matcher default supplies historical wire fields or canonical field order.
	envelope.Revision, envelope.Epoch = 1, 1
	canonical, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal("encode the frozen v4 admission at the fixture epoch")
	}
	digest := sha256.Sum256(canonical)
	fingerprint := hex.EncodeToString(digest[:])
	if library.ValidateStoredAnalysisAdmission(envelope.Profile, envelope.Execution, 1, 1, fingerprint) != nil {
		t.Fatal("frozen v4 admission lost historical validity")
	}
	return string(envelope.Execution), fingerprint
}

func seedAnalysisArchiveV4History(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	execution, fingerprint := analysisArchiveV4Admission(t, "intro")
	for _, entry := range []struct {
		name, run, scope string
		end              int64
	}{
		{"qualified", strings.Repeat("9", 32), "9", 400000000},
		{"visual", strings.Repeat("a", 32), "a", 180000000},
	} {
		result := analysisArchiveV4Fixture(t, entry.name)
		start, end := int64(100000000), entry.end
		facts, err := library.ReadStoredAnalysisResult(result, "qualified", &start, &end)
		if err != nil || facts.Version != "introdetect-v4" || len(facts.Episode.Candidates) != 1 || len(facts.Episode.Candidates[0].Support) != 3 {
			t.Fatal("frozen v4 qualification is not an independent historical fixture")
		}
		run, scope := entry.run, "analysis:"+strings.Repeat(entry.scope, 64)
		childDigest := sha256.Sum256([]byte(run + ":" + scope))
		child := hex.EncodeToString(childDigest[:16])
		if _, err := pool.Exec(ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
			analysis_input,analysis_config_fingerprint,total_children,terminal_children,completed_children,started_at,finished_at)
			VALUES($1,repeat('f',32),'completed','manual','backup-admin','retired-v4-session','admin','media.intro_analysis','Frozen v4 history',
			'{}',$2,1,1,1,'2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, run, fingerprint); err != nil {
			t.Fatal("seed completed v4 task")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)
			VALUES($1,1,1,$2,$3,$4)`, run, fingerprint, analysisArchiveV1Profile, execution); err != nil {
			t.Fatal("seed frozen v4 admission")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key,state,started_at,finished_at)
			VALUES($1,$2,'analysis-history-library','V4 history',0,$3,'completed','2025-01-01T00:00:00Z','2025-01-01T00:00:01Z');`, child, run, scope); err != nil {
			t.Fatal("seed v4 child")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO analysis_work(child_id,run_id,task_key,library_id,scope_key,cohort_revision,force)
			VALUES($1,$2,'media.intro_analysis','analysis-history-library',$3,repeat('d',64),false)`, child, run, scope); err != nil {
			t.Fatal("seed v4 work")
		}
		for position, support := range facts.Episode.Candidates[0].Support {
			item := "analysis-history-v4-" + entry.name + "-" + support.SourceKey
			if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
				VALUES($1,'analysis-history-library',$1,$1,'Movie',false)`, item); err != nil {
				t.Fatal("seed v4 historical owner")
			}
			if _, err := pool.Exec(ctx, `INSERT INTO analysis_work_sources(child_id,item_id,position,target,library_id,root_id,series_id,season_id,episode_key,item_type,
				source_revision,hierarchy_revision,duration_ticks,size,manual_revision,decision_revision,preview_revision)
				VALUES($1,$2,$3,$4,'analysis-history-library','retired-root','retired-series','retired-season',$5,'Episode',$6,'retired-hierarchy',600000000,1024,0,0,0)`,
				child, item, position, support.SourceKey == facts.Episode.SourceKey, support.EpisodeKey, support.SourceKey); err != nil {
				t.Fatal("seed exact v4 source facts")
			}
		}
		item := "analysis-history-v4-" + entry.name + "-" + facts.Episode.SourceKey
		if _, err := pool.Exec(ctx, `INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,
			child_id,cohort_revision,status,result,start_ticks,end_ticks,auto_published)
			VALUES($1,1,$2,$3,1,1,$4,repeat('d',64),'qualified',$5,$6,$7,true)`, item, facts.Episode.SourceKey, fingerprint, child, result, start, end); err != nil {
			t.Fatal("seed complete frozen v4 qualification")
		}
		for _, support := range facts.Episode.Candidates[0].Support {
			if _, err := pool.Exec(ctx, `INSERT INTO analysis_detection_sources(item_id,source_item_id,library_id,root_id,source_revision,hierarchy_revision,episode_key,content_sha256)
				VALUES($1,$2,'analysis-history-library','retired-root',$3,'retired-hierarchy',$4,$5)`, item, "analysis-history-v4-"+entry.name+"-"+support.SourceKey,
				support.SourceKey, support.EpisodeKey, support.ContentIdentity); err != nil {
				t.Fatal("seed v4 support without inventing current metrics")
			}
		}
		if _, err := pool.Exec(ctx, `INSERT INTO analysis_intro_audit(item_id,revision,source_revision,profile_fingerprint,action,evidence)
			VALUES($1,1,$2,$3,'qualified',$4)`, item, facts.Episode.SourceKey, fingerprint, `{"Result":`+string(result)+`,"Decision":null}`); err != nil {
			t.Fatal("seed frozen v4 audit")
		}
	}
	preview, previewFingerprint := analysisArchiveV4Admission(t, "preview")
	if _, err := pool.Exec(ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
		analysis_input,analysis_config_fingerprint,started_at,finished_at)
		VALUES(repeat('f',32),repeat('a',32),'completed','manual','backup-admin','retired-v4-preview','admin','media.preview_generation','V4 preview history',
		'{}',$1,'2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, previewFingerprint); err != nil {
		t.Fatal("seed historical v4 preview task")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)
		VALUES(repeat('f',32),1,1,$1,$2,$3)`, previewFingerprint, analysisArchiveV1Profile, preview); err != nil {
		t.Fatal("seed frozen v4 preview admission")
	}
	return fingerprint
}

func TestPostgreSQLAnalysisV4HistoryPreservesJointAndVisualWireThroughRawRestore(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedAnalysisArchiveState(t, ctx, source)
	seedAnalysisArchiveV1History(t, ctx, source)
	seedAnalysisArchiveV2History(t, ctx, source)
	seedAnalysisArchiveV3History(t, ctx, source)
	fingerprint := seedAnalysisArchiveV4History(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	if _, err := Restore(ctx, source, target, archive, facts, options); err != nil {
		t.Fatalf("raw restore rejected independently frozen v4 evidence: %v", err)
	}
	execution, _ := analysisArchiveV4Admission(t, "intro")
	preview, previewFingerprint := analysisArchiveV4Admission(t, "preview")
	var exact bool
	if err := target.QueryRow(ctx, `SELECT
		(SELECT execution=$1::jsonb AND fingerprint=$2 FROM analysis_run_profiles WHERE run_id=repeat('9',32))
		AND (SELECT analysis_config_fingerprint=$2 FROM task_runs WHERE id=repeat('9',32))
		AND (SELECT result=$3::jsonb AND auto_published FROM analysis_detections WHERE item_id='analysis-history-v4-qualified-source-1')
		AND (SELECT evidence->'Result'=$3::jsonb FROM analysis_intro_audit WHERE item_id='analysis-history-v4-qualified-source-1')
		AND (SELECT execution=$4::jsonb AND fingerprint=$5 FROM analysis_run_profiles WHERE run_id=repeat('f',32))
		AND (SELECT execution=$1::jsonb AND fingerprint=$2 FROM analysis_run_profiles WHERE run_id=repeat('a',32))
		AND (SELECT analysis_config_fingerprint=$2 FROM task_runs WHERE id=repeat('a',32))
		AND (SELECT result=$6::jsonb AND auto_published FROM analysis_detections WHERE item_id='analysis-history-v4-visual-source-1')
		AND (SELECT evidence->'Result'=$6::jsonb FROM analysis_intro_audit WHERE item_id='analysis-history-v4-visual-source-1')`,
		execution, fingerprint, analysisArchiveV4Fixture(t, "qualified"), preview, previewFingerprint, analysisArchiveV4Fixture(t, "visual")).Scan(&exact); err != nil || !exact {
		t.Fatal("raw restore reinterpreted v4 metrics, rewrote admission, or normalized before proof")
	}
	for _, mutation := range []string{
		`UPDATE analysis_detections SET result=jsonb_set(result,'{Version}','"introdetect-v5"') WHERE item_id='analysis-history-v4-visual-source-1'`,
		`UPDATE analysis_detections SET result=jsonb_set(result,'{Episode,Candidates,0,VisualEvidence,MeasurementPolicy}','"coarse-v1"') WHERE item_id='analysis-history-v4-visual-source-1'`,
		`UPDATE analysis_detections SET result=jsonb_set(result,'{Episode,Candidates,0,VisualEvidence,CalibrationDigest}','""') WHERE item_id='analysis-history-v4-visual-source-1'`,
		`UPDATE analysis_detections SET result=result #- '{Episode,Candidates,0,VisualEvidence,Samples}' WHERE item_id='analysis-history-v4-visual-source-1'`,
		`UPDATE analysis_intro_audit SET evidence=evidence #- '{Result,Episode,Candidates,0,Metrics,VisualMatchedTimePermille}' WHERE item_id='analysis-history-v4-qualified-source-1'`,
		`UPDATE analysis_intro_audit SET evidence=jsonb_set(evidence,'{Result,Version}','"introdetect-v999"') WHERE item_id='analysis-history-v4-visual-source-1'`,
	} {
		tx, err := target.Begin(ctx)
		if err != nil {
			t.Fatal("begin isolated v4 history mutation")
		}
		if err := configureTransaction(ctx, tx, options.Schema); err != nil {
			rollback(tx)
			t.Fatal("configure v4 history mutation")
		}
		if _, err := tx.Exec(ctx, mutation); err != nil {
			rollback(tx)
			t.Fatal("apply v4 history mutation")
		}
		err = validateAnalysisState(ctx, tx, 50)
		rollback(tx)
		if !errors.Is(err, ErrSchema) {
			t.Fatalf("v4 history accepted retagging, fabricated evidence, or missing historical metrics: %v", err)
		}
	}
}
