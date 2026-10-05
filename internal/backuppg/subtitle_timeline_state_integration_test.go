//go:build linux

package backuppg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedSubtitleTimelineArchiveState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type)
		VALUES('subtitle-timeline-library','Subtitle timeline history','movies'),('subtitle-timeline-other-library','Other history','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('subtitle-timeline-root','subtitle-timeline-library','/retired/subtitle-timeline','/retired','subtitle-timeline'),
		('subtitle-timeline-other-root','subtitle-timeline-other-library','/retired/other','/retired','other');
		INSERT INTO items(id,library_id,root_id,relative_path,name,sort_name,type,is_folder)
		VALUES('subtitle-timeline-running','subtitle-timeline-library','subtitle-timeline-root','running.mkv','Running','running','Movie',false),
		('subtitle-timeline-pending','subtitle-timeline-library','subtitle-timeline-root','pending.mkv','Pending','pending','Episode',false),
		('subtitle-timeline-ready','subtitle-timeline-library','subtitle-timeline-root','ready.mkv','Ready','ready','Movie',false);
		INSERT INTO sessions(id,user_id,token_hash,kind,created_at,expires_at,revoked_at)
		VALUES('subtitle-timeline-revoked-session','backup-admin',decode(repeat('c7',32),'hex'),'admin','2019-01-01T00:00:00Z','2020-01-01T00:00:00Z',clock_timestamp());
		INSERT INTO task_definitions(id,key,name)
		VALUES(repeat('a',32),'media.subtitle_timeline_generation','Subtitle timeline history'),
		(repeat('f',32),'library.scan','Unrelated task');
		INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,started_at,total_children)
		VALUES(repeat('b',32),repeat('a',32),'running','manual','backup-admin','subtitle-timeline-revoked-session','admin',
		'media.subtitle_timeline_generation','Subtitle timeline history',clock_timestamp(),1);
		INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,state,finished_at)
		VALUES(repeat('c',32),repeat('b',32),'subtitle-timeline-library','Subtitle timeline history',0,'interrupted',clock_timestamp());
		INSERT INTO subtitle_timeline_queue(item_id,operation_id,requested_revision,completed_revision,claimed_revision,
			force,manual,actor_user_id,actor_session_id,state,run_id,child_id,source_revision,reused,error_code,started_at,finished_at)
		VALUES('subtitle-timeline-running',repeat('1',32),2,0,1,true,true,'retired-user','retired-session','running',repeat('b',32),repeat('c',32),'retired-source',false,'',clock_timestamp(),NULL),
		('subtitle-timeline-pending',repeat('2',32),3,2,2,true,true,'retired-user','retired-session','pending',repeat('d',32),repeat('e',32),'older-source',true,'server_interrupted',clock_timestamp(),clock_timestamp()),
		('subtitle-timeline-ready',repeat('3',32),2,2,2,false,true,'backup-admin','subtitle-timeline-revoked-session','ready',repeat('b',32),repeat('c',32),'retired-source',true,'',clock_timestamp(),clock_timestamp());
		INSERT INTO subtitle_timeline_requests(request_id,fingerprint,queued)
		VALUES('retained-force-receipt',repeat('4',64),3)`); err != nil {
		t.Fatalf("seed retained bitmap subtitle timeline state: %v", err)
	}
}

func TestPostgreSQLSubtitleTimelineArchiveValidatesOwnershipWithoutReauthorizingHistory(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	seedSubtitleTimelineArchiveState(t, ctx, source)
	for _, test := range []struct {
		name, mutation string
		valid          bool
	}{
		{"retained_history", "", true},
		{"removed_source_facts", `UPDATE items SET media=NULL,file_size=0,file_identity='',modified_at=NULL WHERE id='subtitle-timeline-running'`, true},
		{"replacement_without_bitmap_subtitles", `UPDATE items SET media='{"DurationTicks":100000000,"Streams":[{"CodecType":"video"}]}' WHERE id='subtitle-timeline-pending'`, true},
		{"deleted_actor_session", `DELETE FROM sessions WHERE id='subtitle-timeline-revoked-session'`, true},
		{"cancelled_before_claim", `UPDATE subtitle_timeline_queue SET state='cancelled',completed_revision=requested_revision,claimed_revision=0,
			run_id='',child_id='',source_revision='',error_code='request_authority_revoked' WHERE item_id='subtitle-timeline-running'`, true},
		{"nonvideo_owner", `UPDATE items SET type='Audio' WHERE id='subtitle-timeline-pending'`, false},
		{"folder_owner", `UPDATE items SET is_folder=true WHERE id='subtitle-timeline-ready'`, false},
		{"cross_library_root", `UPDATE items SET root_id='subtitle-timeline-other-root' WHERE id='subtitle-timeline-running'`, false},
		{"running_without_source", `UPDATE subtitle_timeline_queue SET source_revision='' WHERE item_id='subtitle-timeline-running'`, false},
		{"missing_running_run", `UPDATE subtitle_timeline_queue SET run_id=repeat('9',32) WHERE item_id='subtitle-timeline-running'`, false},
		{"missing_running_child", `UPDATE subtitle_timeline_queue SET child_id=repeat('9',32) WHERE item_id='subtitle-timeline-running'`, false},
		{"cross_library_child", `UPDATE task_run_children SET library_id='subtitle-timeline-other-library' WHERE id=repeat('c',32)`, false},
		{"wrong_task_kind", `UPDATE task_runs SET task_id=repeat('f',32),task_key='library.scan' WHERE id=repeat('b',32)`, false},
		{"unfinished_ready", `UPDATE subtitle_timeline_queue SET completed_revision=1 WHERE item_id='subtitle-timeline-ready'`, false},
		{"completed_pending", `UPDATE subtitle_timeline_queue SET completed_revision=requested_revision WHERE item_id='subtitle-timeline-pending'`, false},
		{"completed_running_claim", `UPDATE subtitle_timeline_queue SET completed_revision=claimed_revision WHERE item_id='subtitle-timeline-running'`, false},
		{"half_history_reference", `UPDATE subtitle_timeline_queue SET child_id='' WHERE item_id='subtitle-timeline-pending'`, false},
		{"wrong_retained_session_kind", `UPDATE sessions SET kind='emby' WHERE id='subtitle-timeline-revoked-session'`, false},
		{"wrong_retained_session_user", `UPDATE subtitle_timeline_queue SET actor_user_id='retired-user' WHERE item_id='subtitle-timeline-ready'`, false},
		{"control_in_actor", `UPDATE subtitle_timeline_queue SET actor_user_id=E'retired\nuser' WHERE item_id='subtitle-timeline-pending'`, false},
		{"space_in_receipt", `UPDATE subtitle_timeline_requests SET request_id=' retained-force-receipt'`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal("begin isolated subtitle-timeline state mutation")
			}
			defer rollback(tx)
			if err := configureTransaction(ctx, tx, options.Schema); err != nil {
				t.Fatal(err)
			}
			if test.mutation != "" {
				if _, err := tx.Exec(ctx, test.mutation); err != nil {
					t.Fatalf("apply SQL-valid subtitle-timeline state mutation: %v", err)
				}
			}
			err = validateSubtitleTimelineState(ctx, tx, 60)
			if test.valid && err != nil || !test.valid && !errors.Is(err, ErrSchema) {
				t.Fatalf("subtitle-timeline archive validity = %v, result = %v", test.valid, err)
			}
		})
	}
}

func TestPostgreSQLSubtitleTimelineRawRestoreRetainsQueueAndExplicitRegenerationReceipts(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedSubtitleTimelineArchiveState(t, ctx, source)
	// Media sidecars are external to the database archive. Point a retained root
	// at real test-owned files so a restore that touched source storage would be
	// visible, including an overwrite that kept the path in place.
	mediaDirectory := t.TempDir()
	sidecarDirectory := filepath.Join(mediaDirectory, "backdrops", "goby-subtitle-timelines", "retained-source")
	if err := os.MkdirAll(sidecarDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	sidecars := map[string]string{
		"manifest.json":    "retained manifest bytes outside database backup",
		"gen-retained.gst": "retained subtitle-timeline bytes outside database backup",
	}
	for name, content := range sidecars {
		if err := os.WriteFile(filepath.Join(sidecarDirectory, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := source.Exec(ctx, `UPDATE library_roots SET path=$1,allowed_path=$1,relative_path='.' WHERE id='subtitle-timeline-root'`, mediaDirectory); err != nil {
		t.Fatal("bind retained source-side witness", err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	if _, err := Restore(ctx, source, target, archive, facts, options); err != nil {
		t.Fatalf("restore retained bitmap subtitle timeline state: %v", err)
	}
	const witness = `SELECT jsonb_build_object(
		'queue',(SELECT jsonb_agg(to_jsonb(q) ORDER BY item_id) FROM subtitle_timeline_queue q),
		'receipts',(SELECT jsonb_agg(to_jsonb(r) ORDER BY request_id) FROM subtitle_timeline_requests r))::text`
	var before, after string
	if err := source.QueryRow(ctx, witness).Scan(&before); err != nil {
		t.Fatal("read original subtitle-timeline history", err)
	}
	if err := target.QueryRow(ctx, witness).Scan(&after); err != nil {
		t.Fatal("read restored subtitle-timeline history", err)
	}
	if before != after {
		t.Fatal("raw recovery changed explicit generation intent or queue history")
	}
	for name, content := range sidecars {
		got, err := os.ReadFile(filepath.Join(sidecarDirectory, name))
		if err != nil || string(got) != content {
			t.Fatalf("database recovery changed source-side subtitle-timeline file %s: %v", name, err)
		}
	}
}

func TestPostgreSQLSubtitleTimelineTaskCannotEnterHistoricalGenericTasks(t *testing.T) {
	for _, version := range []int64{23, 49, 59} {
		t.Run(fmt.Sprintf("schema_%d", version), func(t *testing.T) {
			ctx, source, _, options := recoveryFixtureAtVersion(t, version)
			if _, err := source.Exec(ctx, `INSERT INTO task_definitions(id,key,name) VALUES(repeat('a',32),'plugin.retained_unknown_task','Historical unknown task');
				INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,finished_at)
				VALUES(repeat('b',32),repeat('a',32),'completed','manual','backup-admin','retired-session','admin','plugin.retained_unknown_task','Historical unknown task',clock_timestamp())`); err != nil {
				t.Fatal(err)
			}
			for _, test := range []struct {
				name, mutation string
				valid          bool
			}{
				{"other_unknown_name", "", true},
				{"future_definition", `UPDATE task_definitions SET key='media.subtitle_timeline_generation' WHERE id=repeat('a',32)`, false},
				{"future_run", `UPDATE task_runs SET task_key='media.subtitle_timeline_generation' WHERE id=repeat('b',32)`, false},
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
					err = validateResourceState(ctx, tx, version)
					if test.valid && err != nil || !test.valid && !errors.Is(err, ErrSchema) {
						t.Fatalf("schema %d future task key boundary: valid=%v error=%v", version, test.valid, err)
					}
				})
			}
		})
	}
}

func TestPostgreSQLSubtitleTimelineMigrationKeepsOlderPolicyAndCreditsHistory(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 59)
	seedAnalysisArchiveCreditsHistory(t, ctx, source)
	if _, err := source.Exec(ctx, `UPDATE libraries SET options=options||'{"EnableAudioWaveformGeneration":true}'::jsonb WHERE id='credits-library';
		UPDATE task_system_events SET sequence=17,occurred_at='2026-10-01T00:00:00Z' WHERE name='CreditsAnalysisRequested';
		UPDATE task_system_events SET sequence=23,occurred_at='2026-10-02T00:00:00Z' WHERE name='AudioWaveformGenerationRequested'`); err != nil {
		t.Fatal("seed original independent generation policies", err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 59 || result.CurrentVersion != currentRecoveryVersion(t) {
		t.Fatalf("restore schema 59 before subtitle timeline migration: %v", err)
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	assertHistoricalRecoveryFacts(t, ctx, source, target, facts, after, sequences, targetSequences)
	var preserved bool
	if err := target.QueryRow(ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM libraries WHERE options ? 'EnableSubtitleTimelineGeneration')
		AND (SELECT count(*)=0 FROM subtitle_timeline_queue)
		AND (SELECT count(*)=0 FROM subtitle_timeline_requests)
		AND EXISTS(SELECT 1 FROM task_system_events WHERE name='SubtitleTimelineGenerationRequested' AND sequence=0 AND lifecycle_key='')
		AND (SELECT count(*)=2 FROM analysis_credits_detections)
		AND (SELECT count(*)=3 FROM analysis_credits_detection_sources)`).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("subtitle timeline migration rewrote policy, work, or retained credits evidence: %v", err)
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}
