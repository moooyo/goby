//go:build linux

package backuppg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedAudioWaveformArchiveState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type)
		VALUES('waveform-library','Waveform history','movies'),('waveform-other-library','Other history','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('waveform-root','waveform-library','/retired/waveform','/retired','waveform'),
		('waveform-other-root','waveform-other-library','/retired/other','/retired','other');
		INSERT INTO items(id,library_id,root_id,relative_path,name,sort_name,type,is_folder)
		VALUES('waveform-running','waveform-library','waveform-root','running.mkv','Running','running','Movie',false),
		('waveform-pending','waveform-library','waveform-root','pending.mkv','Pending','pending','Episode',false),
		('waveform-ready','waveform-library','waveform-root','ready.mkv','Ready','ready','Movie',false);
		INSERT INTO sessions(id,user_id,token_hash,kind,created_at,expires_at,revoked_at)
		VALUES('waveform-revoked-session','backup-admin',decode(repeat('c7',32),'hex'),'admin','2019-01-01T00:00:00Z','2020-01-01T00:00:00Z',clock_timestamp());
		INSERT INTO task_definitions(id,key,name)
		VALUES(repeat('a',32),'media.audio_waveform_generation','Waveform history'),
		(repeat('f',32),'library.scan','Unrelated task');
		INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,started_at,total_children)
		VALUES(repeat('b',32),repeat('a',32),'running','manual','backup-admin','waveform-revoked-session','admin',
		'media.audio_waveform_generation','Waveform history',clock_timestamp(),1);
		INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,state,finished_at)
		VALUES(repeat('c',32),repeat('b',32),'waveform-library','Waveform history',0,'interrupted',clock_timestamp());
		INSERT INTO audio_waveform_queue(item_id,operation_id,requested_revision,completed_revision,claimed_revision,
			force,manual,actor_user_id,actor_session_id,state,run_id,child_id,source_revision,reused,error_code,started_at,finished_at)
		VALUES('waveform-running',repeat('1',32),2,0,1,true,true,'retired-user','retired-session','running',repeat('b',32),repeat('c',32),'retired-source',false,'',clock_timestamp(),NULL),
		('waveform-pending',repeat('2',32),3,2,2,true,true,'retired-user','retired-session','pending',repeat('d',32),repeat('e',32),'older-source',true,'server_interrupted',clock_timestamp(),clock_timestamp()),
		('waveform-ready',repeat('3',32),2,2,2,false,true,'backup-admin','waveform-revoked-session','ready',repeat('b',32),repeat('c',32),'retired-source',true,'',clock_timestamp(),clock_timestamp());
		INSERT INTO audio_waveform_requests(request_id,fingerprint,queued)
		VALUES('retained-force-receipt',repeat('4',64),3)`); err != nil {
		t.Fatalf("seed retained audio waveform state: %v", err)
	}
}

func TestPostgreSQLAudioWaveformArchiveValidatesOwnershipWithoutReauthorizingHistory(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	seedAudioWaveformArchiveState(t, ctx, source)
	for _, test := range []struct {
		name, mutation string
		valid          bool
	}{
		{"retained_history", "", true},
		{"removed_source_facts", `UPDATE items SET media=NULL,file_size=0,file_identity='',modified_at=NULL WHERE id='waveform-running'`, true},
		{"replacement_without_audio", `UPDATE items SET media='{"DurationTicks":100000000,"Streams":[{"CodecType":"video"}]}' WHERE id='waveform-pending'`, true},
		{"deleted_actor_session", `DELETE FROM sessions WHERE id='waveform-revoked-session'`, true},
		{"cancelled_before_claim", `UPDATE audio_waveform_queue SET state='cancelled',completed_revision=requested_revision,claimed_revision=0,
			run_id='',child_id='',source_revision='',error_code='request_authority_revoked' WHERE item_id='waveform-running'`, true},
		{"nonvideo_owner", `UPDATE items SET type='Audio' WHERE id='waveform-pending'`, false},
		{"folder_owner", `UPDATE items SET is_folder=true WHERE id='waveform-ready'`, false},
		{"cross_library_root", `UPDATE items SET root_id='waveform-other-root' WHERE id='waveform-running'`, false},
		{"running_without_source", `UPDATE audio_waveform_queue SET source_revision='' WHERE item_id='waveform-running'`, false},
		{"missing_running_run", `UPDATE audio_waveform_queue SET run_id=repeat('9',32) WHERE item_id='waveform-running'`, false},
		{"missing_running_child", `UPDATE audio_waveform_queue SET child_id=repeat('9',32) WHERE item_id='waveform-running'`, false},
		{"cross_library_child", `UPDATE task_run_children SET library_id='waveform-other-library' WHERE id=repeat('c',32)`, false},
		{"wrong_task_kind", `UPDATE task_runs SET task_id=repeat('f',32),task_key='library.scan' WHERE id=repeat('b',32)`, false},
		{"unfinished_ready", `UPDATE audio_waveform_queue SET completed_revision=1 WHERE item_id='waveform-ready'`, false},
		{"completed_pending", `UPDATE audio_waveform_queue SET completed_revision=requested_revision WHERE item_id='waveform-pending'`, false},
		{"completed_running_claim", `UPDATE audio_waveform_queue SET completed_revision=claimed_revision WHERE item_id='waveform-running'`, false},
		{"half_history_reference", `UPDATE audio_waveform_queue SET child_id='' WHERE item_id='waveform-pending'`, false},
		{"wrong_retained_session_kind", `UPDATE sessions SET kind='emby' WHERE id='waveform-revoked-session'`, false},
		{"wrong_retained_session_user", `UPDATE audio_waveform_queue SET actor_user_id='retired-user' WHERE item_id='waveform-ready'`, false},
		{"control_in_actor", `UPDATE audio_waveform_queue SET actor_user_id=E'retired\nuser' WHERE item_id='waveform-pending'`, false},
		{"space_in_receipt", `UPDATE audio_waveform_requests SET request_id=' retained-force-receipt'`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal("begin isolated waveform state mutation")
			}
			defer rollback(tx)
			if err := configureTransaction(ctx, tx, options.Schema); err != nil {
				t.Fatal(err)
			}
			if test.mutation != "" {
				if _, err := tx.Exec(ctx, test.mutation); err != nil {
					t.Fatalf("apply SQL-valid waveform state mutation: %v", err)
				}
			}
			err = validateAudioWaveformState(ctx, tx, 58)
			if test.valid && err != nil || !test.valid && !errors.Is(err, ErrSchema) {
				t.Fatalf("waveform archive validity = %v, result = %v", test.valid, err)
			}
		})
	}
}

func TestPostgreSQLAudioWaveformRawRestoreRetainsQueueAndExplicitRegenerationReceipts(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedAudioWaveformArchiveState(t, ctx, source)
	// Media sidecars are external to the database archive. Point a retained root
	// at real test-owned files so a restore that touched source storage would be
	// visible, including an overwrite that kept the path in place.
	mediaDirectory := t.TempDir()
	sidecarDirectory := filepath.Join(mediaDirectory, "backdrops", "goby-waveforms", "retained-source")
	if err := os.MkdirAll(sidecarDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	sidecars := map[string]string{
		"manifest.json":    "retained manifest bytes outside database backup",
		"gen-retained.gwv": "retained waveform bytes outside database backup",
	}
	for name, content := range sidecars {
		if err := os.WriteFile(filepath.Join(sidecarDirectory, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := source.Exec(ctx, `UPDATE library_roots SET path=$1,allowed_path=$1,relative_path='.' WHERE id='waveform-root'`, mediaDirectory); err != nil {
		t.Fatal("bind retained source-side witness", err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	if _, err := Restore(ctx, source, target, archive, facts, options); err != nil {
		t.Fatalf("restore retained audio waveform state: %v", err)
	}
	const witness = `SELECT jsonb_build_object(
		'queue',(SELECT jsonb_agg(to_jsonb(q) ORDER BY item_id) FROM audio_waveform_queue q),
		'receipts',(SELECT jsonb_agg(to_jsonb(r) ORDER BY request_id) FROM audio_waveform_requests r))::text`
	var before, after string
	if err := source.QueryRow(ctx, witness).Scan(&before); err != nil {
		t.Fatal("read original waveform history", err)
	}
	if err := target.QueryRow(ctx, witness).Scan(&after); err != nil {
		t.Fatal("read restored waveform history", err)
	}
	if before != after {
		t.Fatal("raw recovery changed explicit generation intent or queue history")
	}
	for name, content := range sidecars {
		got, err := os.ReadFile(filepath.Join(sidecarDirectory, name))
		if err != nil || string(got) != content {
			t.Fatalf("database recovery changed source-side waveform file %s: %v", name, err)
		}
	}
}
