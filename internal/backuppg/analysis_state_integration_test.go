//go:build linux

package backuppg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/library"
)

func seedAnalysisArchiveState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	profile := library.DefaultAnalysisProfile()
	execution := library.AnalysisExecutionProfile{Version: library.AnalysisExecutionProfileVersion, UnavailableReason: "not_configured"}
	profileRaw, _ := json.Marshal(profile)
	executionRaw, _ := json.Marshal(execution)
	bound, _ := json.Marshal(struct {
		Version         int
		Revision, Epoch int64
		Profile         library.AnalysisProfile
		Execution       library.AnalysisExecutionProfile
	}{library.AnalysisExecutionProfileVersion, 1, 1, profile, execution})
	digest := sha256.Sum256(bound)
	fingerprint := hex.EncodeToString(digest[:])
	run := strings.Repeat("b", 32)
	scope := "analysis:" + strings.Repeat("c", 64)
	childHash := sha256.Sum256([]byte(run + ":" + scope))
	child := hex.EncodeToString(childHash[:16])
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('analysis-history-library','Analysis history','movies');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES('analysis-history-item','analysis-history-library','Retained item','retained item','Movie',false);
		INSERT INTO task_definitions(id,key,name) VALUES(repeat('a',32),'media.preview_generation','Preview history')`); err != nil {
		t.Fatal("seed analysis history owners")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
		analysis_input,analysis_config_fingerprint,actor_peer_ip,total_children)
		VALUES($1,repeat('a',32),'pending','manual','backup-admin','retired-native-session','admin','media.preview_generation','Preview history','{}',$2,'127.0.0.1',1)`, run, fingerprint); err != nil {
		t.Fatal("seed captured analysis task")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)VALUES($1,1,1,$2,$3,$4)`, run, fingerprint, profileRaw, executionRaw); err != nil {
		t.Fatal("seed immutable analysis profile")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key) VALUES($1,$2,'analysis-history-library','Analysis history',0,$3)`, child, run, scope); err != nil {
		t.Fatal("seed scoped analysis child")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_work(child_id,run_id,task_key,library_id,scope_key,cohort_revision,force)VALUES($1,$2,'media.preview_generation','analysis-history-library',$3,repeat('d',64),false)`, child, run, scope); err != nil {
		t.Fatal("seed immutable analysis work")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_work_sources(child_id,item_id,position,target,library_id,root_id,series_id,season_id,episode_key,item_type,
		source_revision,hierarchy_revision,duration_ticks,size,manual_revision,decision_revision,preview_revision)
		VALUES($1,'analysis-history-item',0,true,'analysis-history-library','retired-root','','','','Movie','retired-source','retired-hierarchy',300000000,1024,0,0,3)`, child); err != nil {
		t.Fatal("seed historical source facts without requiring current filesystem roots")
	}
	content := strings.Repeat("e", 64)
	feature := library.AnalysisFeatures{ContentSHA256: content, AlgorithmProfile: "archive-features-v1"}
	payload, err := library.EncodeAnalysisFeatures(feature, 300000000)
	if err != nil {
		t.Fatal("encode bounded retained feature evidence")
	}
	key := analysisStateFeatureKey("analysis-history-item", "retired-source", fingerprint)
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_feature_cache(cache_key,item_id,source_revision,profile_fingerprint,content_sha256,algorithm_profile,duration_ticks,payload,bytes)
		VALUES($1,'analysis-history-item','retired-source',$2,$3,$4,300000000,$5,$6)`, key, fingerprint, content, feature.AlgorithmProfile, payload, len(payload)); err != nil {
		t.Fatal("seed source-bound feature cache")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_preview_state(item_id,revision)VALUES('analysis-history-item',3);
		INSERT INTO analysis_intro_decisions(item_id,revision,source_revision,rejected)VALUES('analysis-history-item',2,'retired-source',true)`); err != nil {
		t.Fatal("seed retained explicit analysis decisions")
	}
}

func TestPostgreSQLAnalysisArchiveRejectsSemanticCorruptionBeforeNormalization(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	seedAnalysisArchiveState(t, ctx, source)
	for _, test := range []struct {
		name, mutation string
		valid          bool
	}{
		{"retired_authority_and_source", "", true},
		{"unsorted_selection", `UPDATE task_runs SET analysis_input='{"LibraryIds":["z","a"]}' WHERE id=repeat('b',32)`, false},
		{"nonstring_selection", `UPDATE task_runs SET analysis_input='{"ItemIds":[42]}' WHERE id=repeat('b',32)`, false},
		{"mapped_peer", `UPDATE task_runs SET actor_peer_ip='::ffff:127.0.0.1' WHERE id=repeat('b',32)`, false},
		{"profile_binding", `UPDATE task_runs SET analysis_config_fingerprint=repeat('f',64) WHERE id=repeat('b',32)`, false},
		{"child_scope", `UPDATE task_run_children SET analysis_scope_key='analysis:'||repeat('f',64) WHERE run_id=repeat('b',32)`, false},
		{"feature_bytes", `UPDATE analysis_feature_cache SET payload=set_byte(payload,0,0)`, false},
		{"feature_content_binding", `UPDATE analysis_feature_cache SET content_sha256=repeat('f',64)`, false},
		{"feature_algorithm_binding", `UPDATE analysis_feature_cache SET algorithm_profile='other-profile'`, false},
		{"feature_key_binding", `UPDATE analysis_feature_cache SET cache_key=repeat('f',64)`, false},
		{"noncontiguous_sources", `ALTER TABLE analysis_work_sources DISABLE TRIGGER analysis_sources_immutable;UPDATE analysis_work_sources SET position=1`, false},
		{"unknown_profile_field", `ALTER TABLE analysis_run_profiles DISABLE TRIGGER analysis_profile_immutable;UPDATE analysis_run_profiles SET profile=profile||'{"Uncontrolled":true}'`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal("begin isolated analysis archive mutation")
			}
			defer rollback(tx)
			if err := configureTransaction(ctx, tx, options.Schema); err != nil {
				t.Fatal("configure analysis archive inspection")
			}
			if test.mutation != "" {
				if _, err := tx.Exec(ctx, test.mutation); err != nil {
					t.Fatalf("apply the bounded semantic corruption fixture: %v", err)
				}
			}
			err = validateAnalysisState(ctx, tx, 50)
			if test.valid && err != nil || !test.valid && !errors.Is(err, ErrSchema) {
				t.Fatalf("analysis semantic validity=%v, result=%v", test.valid, err)
			}
		})
	}
}

func TestPostgreSQLAnalysisRawRestoreRetainsExactHistoryAndCacheFacts(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedAnalysisArchiveState(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	if _, err := Restore(ctx, source, target, archive, facts, options); err != nil {
		t.Fatalf("restore verified raw analysis state: %v", err)
	}
	const witness = `SELECT jsonb_build_object(
		'settings',(SELECT to_jsonb(s) FROM analysis_settings s),
		'profiles',(SELECT jsonb_agg(to_jsonb(p) ORDER BY run_id) FROM analysis_run_profiles p),
		'work',(SELECT jsonb_agg(to_jsonb(w) ORDER BY child_id) FROM analysis_work w),
		'sources',(SELECT jsonb_agg(to_jsonb(s) ORDER BY child_id,position) FROM analysis_work_sources s),
		'features',(SELECT jsonb_agg(to_jsonb(f) ORDER BY cache_key) FROM analysis_feature_cache f),
		'decisions',(SELECT jsonb_agg(to_jsonb(d) ORDER BY item_id) FROM analysis_intro_decisions d),
		'preview_state',(SELECT jsonb_agg(to_jsonb(p) ORDER BY item_id) FROM analysis_preview_state p),
		'runs',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM task_runs r WHERE task_key='media.preview_generation'),
		'children',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM task_run_children c WHERE analysis_scope_key<>''))::text`
	var before, after string
	if source.QueryRow(ctx, witness).Scan(&before) != nil || target.QueryRow(ctx, witness).Scan(&after) != nil || before != after {
		t.Fatal("raw restore normalized or changed original analysis authority/evidence before its consumer boundary")
	}
}
