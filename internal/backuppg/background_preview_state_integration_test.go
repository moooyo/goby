//go:build linux

package backuppg

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedBackgroundPreviewArchiveState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type)
		VALUES('background-library','Background history','movies'),('background-other-library','Other history','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('background-root','background-library','/retired/background','/retired','background'),
		('background-other-root','background-other-library','/retired/other','/retired','other');
		INSERT INTO items(id,library_id,root_id,relative_path,name,sort_name,type,is_folder)
		VALUES('background-running','background-library','background-root','running.mkv','Running','running','Movie',false),
		('background-pending','background-library','background-root','pending.mkv','Pending','pending','Episode',false),
		('background-ready','background-library','background-root','ready.mkv','Ready','ready','Movie',false);
		INSERT INTO sessions(id,user_id,token_hash,kind,created_at,expires_at,revoked_at)
		VALUES('background-revoked-session','backup-admin',decode(repeat('c7',32),'hex'),'admin','2019-01-01T00:00:00Z','2020-01-01T00:00:00Z',clock_timestamp());
		INSERT INTO task_definitions(id,key,name)
		VALUES(repeat('a',32),'media.background_preview_generation','Background history'),
		(repeat('f',32),'library.scan','Unrelated task');
		INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,started_at,total_children)
		VALUES(repeat('b',32),repeat('a',32),'running','manual','backup-admin','background-revoked-session','admin',
		'media.background_preview_generation','Background history',clock_timestamp(),1);
		INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,state,finished_at)
		VALUES(repeat('c',32),repeat('b',32),'background-library','Background history',0,'interrupted',clock_timestamp());
		INSERT INTO item_background_preview_settings(item_id,revision,manual_start_ticks,updated_by)
		VALUES('background-pending',3,999000000,'backup-admin');
		INSERT INTO background_preview_queue(item_id,operation_id,requested_revision,completed_revision,claimed_revision,
			force,manual,actor_user_id,actor_session_id,state,run_id,child_id,source_revision,reused,error_code,started_at,finished_at)
		VALUES('background-running',repeat('1',32),2,0,1,true,true,'retired-user','retired-session','running',repeat('b',32),repeat('c',32),'retired-source',false,'',clock_timestamp(),NULL),
		('background-pending',repeat('2',32),3,2,2,true,true,'retired-user','retired-session','pending',repeat('d',32),repeat('e',32),'older-source',true,'server_interrupted',clock_timestamp(),clock_timestamp()),
		('background-ready',repeat('3',32),2,2,2,false,true,'backup-admin','background-revoked-session','ready',repeat('b',32),repeat('c',32),'retired-source',true,'',clock_timestamp(),clock_timestamp());
		INSERT INTO background_preview_requests(request_id,fingerprint,queued)
		VALUES('retained-force-receipt',repeat('4',64),3)`); err != nil {
		t.Fatalf("seed retained background preview state: %v", err)
	}
}

func TestPostgreSQLBackgroundPreviewArchiveValidatesOwnershipWithoutReauthorizingHistory(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	seedBackgroundPreviewArchiveState(t, ctx, source)
	for _, test := range []struct {
		name, mutation string
		valid          bool
	}{
		{"retained_history", "", true},
		{"removed_source_facts", `UPDATE items SET media=NULL,file_size=0,file_identity='',modified_at=NULL WHERE id='background-running'`, true},
		{"manual_start_exceeds_replacement_duration", `UPDATE items SET media='{"DurationTicks":100000000}' WHERE id='background-pending'`, true},
		{"deleted_actor_session", `DELETE FROM sessions WHERE id='background-revoked-session'`, true},
		{"cancelled_before_claim", `UPDATE background_preview_queue SET state='cancelled',completed_revision=requested_revision,claimed_revision=0,
			run_id='',child_id='',source_revision='',error_code='request_authority_revoked' WHERE item_id='background-running'`, true},
		{"missing_settings", `DELETE FROM background_preview_settings`, false},
		{"nonvideo_owner", `UPDATE items SET type='Audio' WHERE id='background-pending'`, false},
		{"folder_owner", `UPDATE items SET is_folder=true WHERE id='background-ready'`, false},
		{"cross_library_root", `UPDATE items SET root_id='background-other-root' WHERE id='background-running'`, false},
		{"running_without_source", `UPDATE background_preview_queue SET source_revision='' WHERE item_id='background-running'`, false},
		{"missing_running_run", `UPDATE background_preview_queue SET run_id=repeat('9',32) WHERE item_id='background-running'`, false},
		{"missing_running_child", `UPDATE background_preview_queue SET child_id=repeat('9',32) WHERE item_id='background-running'`, false},
		{"cross_library_child", `UPDATE task_run_children SET library_id='background-other-library' WHERE id=repeat('c',32)`, false},
		{"wrong_task_kind", `UPDATE task_runs SET task_id=repeat('f',32),task_key='library.scan' WHERE id=repeat('b',32)`, false},
		{"unfinished_ready", `UPDATE background_preview_queue SET completed_revision=1 WHERE item_id='background-ready'`, false},
		{"completed_pending", `UPDATE background_preview_queue SET completed_revision=requested_revision WHERE item_id='background-pending'`, false},
		{"completed_running_claim", `UPDATE background_preview_queue SET completed_revision=claimed_revision WHERE item_id='background-running'`, false},
		{"half_history_reference", `UPDATE background_preview_queue SET child_id='' WHERE item_id='background-pending'`, false},
		{"wrong_retained_session_kind", `UPDATE sessions SET kind='emby' WHERE id='background-revoked-session'`, false},
		{"wrong_retained_session_user", `UPDATE background_preview_queue SET actor_user_id='retired-user' WHERE item_id='background-ready'`, false},
		{"control_in_actor", `UPDATE background_preview_queue SET actor_user_id=E'retired\nuser' WHERE item_id='background-pending'`, false},
		{"space_in_receipt", `UPDATE background_preview_requests SET request_id=' retained-force-receipt'`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal("begin isolated background state mutation")
			}
			defer rollback(tx)
			if err := configureTransaction(ctx, tx, options.Schema); err != nil {
				t.Fatal(err)
			}
			if test.mutation != "" {
				if _, err := tx.Exec(ctx, test.mutation); err != nil {
					t.Fatalf("apply SQL-valid background state mutation: %v", err)
				}
			}
			err = validateBackgroundPreviewState(ctx, tx, 57)
			if test.valid && err != nil || !test.valid && !errors.Is(err, ErrSchema) {
				t.Fatalf("background archive validity = %v, result = %v", test.valid, err)
			}
		})
	}
}

func TestPostgreSQLBackgroundPreviewRawRestoreRetainsQueueAndExplicitRegenerationReceipts(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedBackgroundPreviewArchiveState(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	if _, err := Restore(ctx, source, target, archive, facts, options); err != nil {
		t.Fatalf("restore retained background preview state: %v", err)
	}
	const witness = `SELECT jsonb_build_object(
		'settings',(SELECT to_jsonb(s) FROM background_preview_settings s),
		'items',(SELECT jsonb_agg(to_jsonb(s) ORDER BY item_id) FROM item_background_preview_settings s),
		'queue',(SELECT jsonb_agg(to_jsonb(q) ORDER BY item_id) FROM background_preview_queue q),
		'receipts',(SELECT jsonb_agg(to_jsonb(r) ORDER BY request_id) FROM background_preview_requests r))::text`
	var before, after string
	if err := source.QueryRow(ctx, witness).Scan(&before); err != nil {
		t.Fatal("read original background history", err)
	}
	if err := target.QueryRow(ctx, witness).Scan(&after); err != nil {
		t.Fatal("read restored background history", err)
	}
	if before != after {
		t.Fatal("raw recovery changed explicit generation intent or queue history")
	}
}
