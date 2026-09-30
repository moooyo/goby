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
	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/library"
)

func seedAnalysisArchiveIntroSkipperHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []byte {
	t.Helper()
	profile := library.DefaultAnalysisProfile()
	execution := library.AnalysisExecutionProfile{Version: library.AnalysisExecutionProfileVersion, Available: true,
		FFmpegSHA256: strings.Repeat("a", 64), FingerprintSHA256: strings.Repeat("a", 64),
		DetectorVersion: introskipper.Version, IntroProfile: "archive-intro-skipper-v1", IntroSkipperOptions: profile.IntroSkipper}
	profileRaw, _ := json.Marshal(profile)
	executionRaw, _ := json.Marshal(execution)
	canonical, _ := json.Marshal(struct {
		Version         int
		Revision, Epoch int64
		Profile         library.AnalysisProfile
		Execution       library.AnalysisExecutionProfile
	}{library.AnalysisExecutionProfileVersion, 1, 1, profile, execution})
	digest := sha256.Sum256(canonical)
	fingerprint := hex.EncodeToString(digest[:])
	if err := library.ValidateStoredAnalysisAdmission(profileRaw, executionRaw, 1, 1, fingerprint); err != nil {
		t.Fatalf("native archive seed has an invalid admission: %v", err)
	}
	run, scope := strings.Repeat("9", 32), "analysis:"+strings.Repeat("9", 64)
	childDigest := sha256.Sum256([]byte(run + ":" + scope))
	child := hex.EncodeToString(childDigest[:16])
	if _, err := pool.Exec(ctx, `INSERT INTO task_definitions(id,key,name) VALUES(repeat('f',32),'media.intro_analysis','Native intro analysis')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,
		analysis_input,analysis_config_fingerprint,total_children,terminal_children,completed_children,started_at,finished_at)
		VALUES($1,repeat('f',32),'completed','manual','backup-admin','retired-native-session','admin','media.intro_analysis','Native intro analysis',
		'{}',$2,1,1,1,'2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, run, fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution)
		VALUES($1,1,1,$2,$3,$4)`, run, fingerprint, profileRaw, executionRaw); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key,state,started_at,finished_at)
		VALUES($1,$2,'analysis-history-library','Analysis history',0,$3,'completed','2025-01-01T00:00:00Z','2025-01-01T00:00:01Z')`, child, run, scope); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_work(child_id,run_id,task_key,library_id,scope_key,cohort_revision,force)
		VALUES($1,$2,'media.intro_analysis','analysis-history-library',$3,repeat('d',64),false)`, child, run, scope); err != nil {
		t.Fatal(err)
	}
	const duration = 60 * introskipper.TicksPerSecond
	interval := introskipper.Interval{StartTicks: 0, EndTicks: 20 * introskipper.TicksPerSecond}
	support := []introskipper.Support{
		{EpisodeKey: "native-episode-1", SourceKey: "native-source-1", ContentIdentity: strings.Repeat("a", 64), AlgorithmProfile: execution.IntroProfile, Interval: interval},
		{EpisodeKey: "native-episode-2", SourceKey: "native-source-2", ContentIdentity: strings.Repeat("b", 64), AlgorithmProfile: execution.IntroProfile, Interval: interval},
	}
	for position, value := range support {
		item := "native-item-" + string(rune('1'+position))
		if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
			VALUES($1,'analysis-history-library',$1,$1,'Episode',false)`, item); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO analysis_work_sources(child_id,item_id,position,target,library_id,root_id,series_id,season_id,episode_key,item_type,
			source_revision,hierarchy_revision,duration_ticks,size,manual_revision,decision_revision,preview_revision)
			VALUES($1,$2,$3,true,'analysis-history-library','retired-root','retired-series','retired-season',$4,'Episode',$5,'retired-hierarchy',$6,1024,0,0,0)`,
			child, item, position, value.EpisodeKey, value.SourceKey, duration); err != nil {
			t.Fatal(err)
		}
	}
	result := library.AnalysisStoredIntroSkipperResult{Version: introskipper.Version, Options: profile.IntroSkipper,
		Episode: introskipper.EpisodeResult{EpisodeKey: support[0].EpisodeKey, SourceKey: support[0].SourceKey, ContentIdentity: support[0].ContentIdentity,
			AlgorithmProfile: execution.IntroProfile, DurationTicks: duration, Status: introskipper.Qualified, Reasons: []string{},
			Candidate: &introskipper.Candidate{Interval: interval, UpstreamCommit: introskipper.UpstreamCommit, Support: support}}}
	resultRaw, _ := json.Marshal(result)
	if _, err := library.ReadStoredAnalysisResult(resultRaw, introskipper.Qualified, &interval.StartTicks, &interval.EndTicks); err != nil {
		t.Fatalf("native archive seed has an invalid result: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,
		child_id,cohort_revision,status,result,start_ticks,end_ticks,auto_published)
		VALUES('native-item-1',1,'native-source-1',$1,1,1,$2,repeat('d',64),'qualified',$3,$4,$5,true)`, fingerprint, child, resultRaw, interval.StartTicks, interval.EndTicks); err != nil {
		t.Fatal(err)
	}
	for position, value := range support {
		if _, err := pool.Exec(ctx, `INSERT INTO analysis_detection_sources(item_id,source_item_id,library_id,root_id,source_revision,hierarchy_revision,episode_key,content_sha256)
			VALUES('native-item-1',$1,'analysis-history-library','retired-root',$2,'retired-hierarchy',$3,$4)`,
			"native-item-"+string(rune('1'+position)), value.SourceKey, value.EpisodeKey, value.ContentIdentity); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_intro_audit(item_id,revision,source_revision,profile_fingerprint,action,evidence)
		VALUES('native-item-1',1,'native-source-1',$1,'qualified',$2)`, fingerprint, `{"Result":`+string(resultRaw)+`,"Decision":null}`); err != nil {
		t.Fatal(err)
	}
	features := library.AnalysisFeatures{ContentSHA256: support[0].ContentIdentity, AlgorithmProfile: execution.IntroProfile,
		RawFingerprint: []uint32{1, 2, 3, 4}, FingerprintEndSeconds: 60}
	payload, err := library.EncodeAnalysisFeatures(features, duration)
	if err != nil {
		t.Fatal(err)
	}
	key := analysisStateFeatureKey("native-item-1", "native-source-1", fingerprint)
	if _, err := pool.Exec(ctx, `INSERT INTO analysis_feature_cache(cache_key,item_id,source_revision,profile_fingerprint,content_sha256,algorithm_profile,
		duration_ticks,payload,bytes) VALUES($1,'native-item-1','native-source-1',$2,$3,$4,$5,$6,$7)`,
		key, fingerprint, features.ContentSHA256, features.AlgorithmProfile, duration, payload, len(payload)); err != nil {
		t.Fatal(err)
	}
	return resultRaw
}

func TestPostgreSQLAnalysisIntroSkipperArchiveBindsSettingsAndSourceFacts(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	seedAnalysisArchiveState(t, ctx, source)
	seedAnalysisArchiveIntroSkipperHistory(t, ctx, source)
	for _, test := range []struct {
		name, mutation string
		version        int64
		valid          bool
	}{
		{"complete", "", 54, true},
		{"future_payload_in_schema_53", "", 53, false},
		{"changed_result_options", `UPDATE analysis_detections SET result=jsonb_set(result,'{Options,MaximumTimeSkip}','4') WHERE item_id='native-item-1'`, 54, false},
		{"changed_source_duration", `UPDATE analysis_detections SET result=jsonb_set(result,'{Episode,DurationTicks}','600000001') WHERE item_id='native-item-1'`, 54, false},
		{"changed_algorithm", `UPDATE analysis_detections SET result=replace(result::text,'archive-intro-skipper-v1','other-profile')::jsonb WHERE item_id='native-item-1'`, 54, false},
		{"missing_support", `DELETE FROM analysis_detection_sources WHERE item_id='native-item-1' AND source_item_id='native-item-2'`, 54, false},
		{"unknown_settings_option", `ALTER TABLE analysis_settings DROP CONSTRAINT analysis_settings_intro_skipper_options_check; UPDATE analysis_settings SET intro_skipper_options=intro_skipper_options||'{"Unknown":1}'::jsonb`, 54, false},
		{"missing_settings_option", `ALTER TABLE analysis_settings DROP CONSTRAINT analysis_settings_intro_skipper_options_check; UPDATE analysis_settings SET intro_skipper_options=intro_skipper_options-'MaximumTimeSkip'`, 54, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			if err := configureTransaction(ctx, tx, options.Schema); err != nil {
				t.Fatal(err)
			}
			if test.mutation != "" {
				if _, err := tx.Exec(ctx, test.mutation); err != nil {
					t.Fatal(err)
				}
			}
			err = validateAnalysisState(ctx, tx, test.version)
			if test.valid && err != nil || !test.valid && !errors.Is(err, ErrSchema) {
				t.Fatalf("native archive validity=%v, result=%v", test.valid, err)
			}
		})
	}
	for _, version := range []int64{50, 51, 52, 53, 54} {
		tx, err := source.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := configureTransaction(ctx, tx, options.Schema); err != nil {
			rollback(tx)
			t.Fatal(err)
		}
		for _, validate := range []func(context.Context) error{
			func(ctx context.Context) error { return validateAnalysisAdmissionState(ctx, tx, version) },
			func(ctx context.Context) error { return validateAnalysisFeatureState(ctx, tx, version) },
			func(ctx context.Context) error { return validateAnalysisDetectionState(ctx, tx, version) },
		} {
			err = validate(ctx)
			if version < 54 && !errors.Is(err, ErrSchema) || version == 54 && err != nil {
				rollback(tx)
				t.Fatalf("schema %d changed the native archive payload boundary: %v", version, err)
			}
		}
		rollback(tx)
	}
}

func TestPostgreSQLAnalysisIntroSkipperHistorySurvivesRawRestore(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedAnalysisArchiveState(t, ctx, source)
	result := seedAnalysisArchiveIntroSkipperHistory(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	if _, err := Restore(ctx, source, target, archive, facts, options); err != nil {
		t.Fatalf("restore native pair evidence with retained v5 history: %v", err)
	}
	var exact bool
	if err := target.QueryRow(ctx, `SELECT
		(SELECT result=$1::jsonb AND auto_published FROM analysis_detections WHERE item_id='native-item-1')
		AND (SELECT evidence->'Result'=$1::jsonb FROM analysis_intro_audit WHERE item_id='native-item-1')
		AND (SELECT count(*)=2 FROM analysis_detection_sources WHERE item_id='native-item-1')
		AND (SELECT get_byte(payload,4)=4 FROM analysis_feature_cache WHERE item_id='native-item-1')
		AND (SELECT execution->>'Version'='5' AND NOT (profile ? 'IntroSkipper') FROM analysis_run_profiles WHERE run_id=repeat('b',32))`, result).Scan(&exact); err != nil || !exact {
		t.Fatalf("restore changed native pair evidence or reinterpreted v5 history: %v", err)
	}
}
