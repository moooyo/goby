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

// These hashes pin complete wire fixtures frozen from 00047ac, including the
// original Options field order and the old qualification metric semantics.
func analysisArchiveV2Fixture(t *testing.T, name string) []byte {
	t.Helper()
	pins := map[string]string{
		"intro":       "2c255299bb4919df394fb980855fef0698d89c97f9418daac694aeb4cc08a4a4",
		"preview":     "d98d3cffc35bdce942491b1c10f322c86b1bfe0689d5bcd566df2beecd5c0eda",
		"unavailable": "7a17d7533f15bb16083eee6c1eb61f67a68ba039502a9ba694d7bf8592482f95",
		"qualified":   "6d0ae6ab835adc32e6eaa7f8fa8765820e393a57458a5ee4191f5c6dc1af4faf",
	}
	filename := "analysis-admission-v2-" + name + ".json"
	if name == "qualified" {
		filename = "analysis-result-v2-qualified.json"
	}
	raw, err := os.ReadFile(filepath.Join("..", "library", "testdata", filename))
	digest := sha256.Sum256(raw)
	if err != nil || pins[name] == "" || hex.EncodeToString(digest[:]) != pins[name] {
		t.Fatal("frozen v2 archive fixture changed")
	}
	return raw
}

func analysisArchiveV2Admission(t *testing.T, name string) (string, string) {
	t.Helper()
	var envelope struct {
		Version         int
		Revision, Epoch int64
		Profile         json.RawMessage
		Execution       json.RawMessage
	}
	if err := json.Unmarshal(analysisArchiveV2Fixture(t, name), &envelope); err != nil || envelope.Version != 2 ||
		envelope.Revision != 7 || envelope.Epoch != 3 || string(envelope.Profile) != analysisArchiveV1Profile {
		t.Fatal("frozen v2 admission envelope differs")
	}
	// Only fixture-local revision and epoch change; no current typed profile or
	// matcher default supplies historical wire fields or canonical field order.
	envelope.Revision, envelope.Epoch = 1, 1
	canonical, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal("encode the frozen v2 admission at the fixture epoch")
	}
	digest := sha256.Sum256(canonical)
	fingerprint := hex.EncodeToString(digest[:])
	if library.ValidateStoredAnalysisAdmission(envelope.Profile, envelope.Execution, 1, 1, fingerprint) != nil {
		t.Fatal("frozen v2 admission lost historical validity")
	}
	return string(envelope.Execution), fingerprint
}

func seedAnalysisArchiveV2History(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	execution, fingerprint := analysisArchiveV2Admission(t, "intro")
	result := analysisArchiveV2Fixture(t, "qualified")
	start, end := int64(100000000), int64(400000000)
	facts, err := library.ReadStoredAnalysisResult(result, "qualified", &start, &end)
	if err != nil || facts.Version != "introdetect-v2" || len(facts.Episode.Candidates) != 1 || len(facts.Episode.Candidates[0].Support) != 3 {
		t.Fatal("frozen v2 qualification is not an independent historical fixture")
	}
	run, scope := strings.Repeat("2", 32), "analysis:"+strings.Repeat("4", 64)
	childDigest := sha256.Sum256([]byte(run + ":" + scope))
	child := hex.EncodeToString(childDigest[:16])
	if _, err := pool.Exec(ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
		analysis_input,analysis_config_fingerprint,total_children,terminal_children,completed_children,started_at,finished_at)
		VALUES($1,repeat('f',32),'completed','manual','backup-admin','retired-v2-session','admin','media.intro_analysis','Frozen v2 history',
		'{}',$2,1,1,1,'2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, run, fingerprint); err != nil {
		t.Fatal("seed completed v2 task")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)
		VALUES($1,1,1,$2,$3,$4)`, run, fingerprint, analysisArchiveV1Profile, execution); err != nil {
		t.Fatal("seed frozen v2 admission")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key,state,started_at,finished_at)
		VALUES($1,$2,'analysis-history-library','V2 history',0,$3,'completed','2025-01-01T00:00:00Z','2025-01-01T00:00:01Z');`, child, run, scope); err != nil {
		t.Fatal("seed v2 child")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_work(child_id,run_id,task_key,library_id,scope_key,cohort_revision,force)
		VALUES($1,$2,'media.intro_analysis','analysis-history-library',$3,repeat('d',64),false)`, child, run, scope); err != nil {
		t.Fatal("seed v2 work")
	}
	for position, support := range facts.Episode.Candidates[0].Support {
		item := "analysis-history-v2-" + support.SourceKey
		if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
			VALUES($1,'analysis-history-library',$1,$1,'Movie',false)`, item); err != nil {
			t.Fatal("seed v2 historical owner")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO analysis_work_sources(child_id,item_id,position,target,library_id,root_id,series_id,season_id,episode_key,item_type,
			source_revision,hierarchy_revision,duration_ticks,size,manual_revision,decision_revision,preview_revision)
			VALUES($1,$2,$3,$4,'analysis-history-library','retired-root','retired-series','retired-season',$5,'Episode',$6,'retired-hierarchy',600000000,1024,0,0,0)`,
			child, item, position, support.SourceKey == facts.Episode.SourceKey, support.EpisodeKey, support.SourceKey); err != nil {
			t.Fatal("seed exact v2 source facts")
		}
	}
	item := "analysis-history-v2-" + facts.Episode.SourceKey
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,
		child_id,cohort_revision,status,result,start_ticks,end_ticks,auto_published)
		VALUES($1,1,$2,$3,1,1,$4,repeat('d',64),'qualified',$5,$6,$7,true)`, item, facts.Episode.SourceKey, fingerprint, child, result, start, end); err != nil {
		t.Fatal("seed complete frozen v2 qualification")
	}
	for _, support := range facts.Episode.Candidates[0].Support {
		if _, err := pool.Exec(ctx, `INSERT INTO analysis_detection_sources(item_id,source_item_id,library_id,root_id,source_revision,hierarchy_revision,episode_key,content_sha256)
			VALUES($1,$2,'analysis-history-library','retired-root',$3,'retired-hierarchy',$4,$5)`, item, "analysis-history-v2-"+support.SourceKey,
			support.SourceKey, support.EpisodeKey, support.ContentIdentity); err != nil {
			t.Fatal("seed v2 support without inventing current metrics")
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_intro_audit(item_id,revision,source_revision,profile_fingerprint,action,evidence)
		VALUES($1,1,$2,$3,'qualified',$4)`, item, facts.Episode.SourceKey, fingerprint, `{"Result":`+string(result)+`,"Decision":null}`); err != nil {
		t.Fatal("seed frozen v2 audit")
	}
	preview, previewFingerprint := analysisArchiveV2Admission(t, "preview")
	if _, err := pool.Exec(ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
		analysis_input,analysis_config_fingerprint,started_at,finished_at)
		VALUES(repeat('3',32),repeat('a',32),'completed','manual','backup-admin','retired-v2-preview','admin','media.preview_generation','V2 preview history',
		'{}',$1,'2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, previewFingerprint); err != nil {
		t.Fatal("seed historical v2 preview task")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)
		VALUES(repeat('3',32),1,1,$1,$2,$3)`, previewFingerprint, analysisArchiveV1Profile, preview); err != nil {
		t.Fatal("seed frozen v2 preview admission")
	}
	return fingerprint
}

func TestPostgreSQLAnalysisV2HistoryPreservesQualifiedWireThroughRawRestore(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedAnalysisArchiveState(t, ctx, source)
	seedAnalysisArchiveV1History(t, ctx, source)
	fingerprint := seedAnalysisArchiveV2History(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	if _, err := Restore(ctx, source, target, archive, facts, options); err != nil {
		t.Fatalf("raw restore rejected independently frozen v2 evidence: %v", err)
	}
	execution, _ := analysisArchiveV2Admission(t, "intro")
	preview, previewFingerprint := analysisArchiveV2Admission(t, "preview")
	var exact bool
	if err := target.QueryRow(ctx, `SELECT
		(SELECT execution=$1::jsonb AND fingerprint=$2 FROM analysis_run_profiles WHERE run_id=repeat('2',32))
		AND (SELECT analysis_config_fingerprint=$2 FROM task_runs WHERE id=repeat('2',32))
		AND (SELECT result=$3::jsonb AND auto_published FROM analysis_detections WHERE item_id='analysis-history-v2-source-1')
		AND (SELECT evidence->'Result'=$3::jsonb FROM analysis_intro_audit WHERE item_id='analysis-history-v2-source-1')
		AND (SELECT execution=$4::jsonb AND fingerprint=$5 FROM analysis_run_profiles WHERE run_id=repeat('3',32))`,
		execution, fingerprint, analysisArchiveV2Fixture(t, "qualified"), preview, previewFingerprint).Scan(&exact); err != nil || !exact {
		t.Fatal("raw restore reinterpreted v2 metrics, rewrote admission, or normalized before proof")
	}
	for _, mutation := range []string{
		`UPDATE analysis_detections SET result=jsonb_set(result,'{Version}','"introdetect-v3"') WHERE item_id='analysis-history-v2-source-1'`,
		`UPDATE analysis_intro_audit SET evidence=evidence #- '{Result,Episode,Candidates,0,Metrics,VisualMatchedTimePermille}' WHERE item_id='analysis-history-v2-source-1'`,
	} {
		tx, err := target.Begin(ctx)
		if err != nil {
			t.Fatal("begin isolated v2 history mutation")
		}
		if err := configureTransaction(ctx, tx, options.Schema); err != nil {
			rollback(tx)
			t.Fatal("configure v2 history mutation")
		}
		if _, err := tx.Exec(ctx, mutation); err != nil {
			rollback(tx)
			t.Fatal("apply v2 history mutation")
		}
		err = validateAnalysisState(ctx, tx, 50)
		rollback(tx)
		if !errors.Is(err, ErrSchema) {
			t.Fatalf("v2 history accepted current retagging or missing historical metrics: %v", err)
		}
	}
}
