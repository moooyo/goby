package database_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestLocalNFORecoveryMigrationBoundsSourcesAndWork(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	themeOwnersMigrateTo(t, ctx, pool, 80)
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('nfo-library','NFO','mixed');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder) VALUES
		('nfo-file','nfo-library','Film','film','Movie',false),('nfo-folder','nfo-library','Show','show','Series',true);
		INSERT INTO scan_jobs(id,library_id,status,finished_at) VALUES
		('nfo-file-scan','nfo-library','Completed',clock_timestamp()),('nfo-folder-scan','nfo-library','Completed',clock_timestamp());
		UPDATE task_system_events SET sequence=9007199254740993,occurred_at='2026-01-01T00:00:00.123456Z'
		WHERE name='DolbyVisionVerificationRequested'`); err != nil {
		t.Fatalf("seed schema80 recovery boundary: %v", err)
	}
	themeOwnersMigrateTo(t, ctx, pool, 81)
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM local_nfo_recovery_sources)
		AND NOT EXISTS (SELECT 1 FROM local_nfo_recovery_work)
		AND EXISTS (SELECT 1 FROM task_system_events WHERE name='NFORecoveryRequested'
			AND sequence=0 AND occurred_at IS NOT NULL AND lifecycle_key='')
		AND EXISTS (SELECT 1 FROM task_system_events WHERE name='DolbyVisionVerificationRequested'
			AND sequence=9007199254740993 AND occurred_at='2026-01-01T00:00:00.123456Z' AND lifecycle_key='')`).Scan(&valid); err != nil || !valid {
		t.Fatalf("migration inferred recovery state or changed the prior event counter: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_definitions(id,key,name) VALUES(repeat('a',32),'nfo-fixture','Recover local NFO');
		INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind,system_event) VALUES
		(repeat('b',32),repeat('a',32),1,0,'system_event','NFORecoveryRequested');
		INSERT INTO local_nfo_recovery_sources
		(item_id,scan_job_id,source_key,directory_identity,root_identity,relative_path,media_change_ns,document,document_hash) VALUES
		('nfo-file','nfo-file-scan','file-source','1:2','1:3','film.nfo',9007199254740993,convert_to(repeat('x',2097152),'UTF8'),repeat('a',64)),
		('nfo-folder','nfo-folder-scan','folder-source','1:4','1:3','show/tvshow.nfo',0,decode('78','hex'),repeat('b',64));
		INSERT INTO local_nfo_recovery_work
		(item_id,source_key,directory_identity,root_identity,relative_path,media_change_ns,version,reason) VALUES
		('nfo-file','file-source','1:2','1:3','film.nfo',9007199254740993,repeat('x',512),'empty'),
		('nfo-folder','folder-source','1:4','1:3','show/tvshow.nfo',0,'damaged-folder-sidecar','malformed')`); err != nil {
		t.Fatalf("seed recovery documents, queued work, and a system event trigger: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*)=2 FROM local_nfo_recovery_work
		WHERE status='pending' AND attempts=0 AND retry_at='-infinity' AND isfinite(confirmed_at)`).Scan(&valid); err != nil || !valid {
		t.Fatalf("new recovery work did not start ready and unattempted: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT i.indisvalid AND i.indrelid='local_nfo_recovery_work'::regclass AND i.indnkeyatts=2
		AND pg_get_indexdef(i.indexrelid,1,true)='retry_at' AND pg_get_indexdef(i.indexrelid,2,true)='item_id'
		AND pg_get_expr(i.indpred,i.indrelid,true)=$$status = ANY (ARRAY['pending'::text, 'verify_outcome'::text])$$
		FROM pg_index i WHERE i.indexrelid='local_nfo_recovery_work_due_idx'::regclass`).Scan(&valid); err != nil || !valid {
		t.Fatalf("recovery work index does not cover both due states: valid=%v error=%v", valid, err)
	}
	for _, tc := range []struct{ name, table, patch, state string }{
		{"empty document", "sources", `document=decode('','hex')`, "23514"},
		{"oversized document", "sources", `document=convert_to(repeat('x',2097153),'UTF8')`, "23514"},
		{"invalid document hash", "sources", `document_hash=repeat('A',64)`, "23514"},
		{"negative source timestamp", "sources", `media_change_ns=-1`, "23514"},
		{"duplicate source", "sources", `item_id='nfo-folder'`, "23505"},
		{"missing scan", "sources", `scan_job_id='unknown-scan'`, "23503"},
		{"negative work timestamp", "work", `media_change_ns=-1`, "23514"},
		{"oversized version", "work", `version=repeat('x',513)`, "23514"},
		{"unknown reason", "work", `reason='permission_error'`, "23514"},
		{"unknown status", "work", `status='unknown',retry_at='infinity'`, "23514"},
		{"pending without retry", "work", `retry_at='infinity'`, "23514"},
		{"terminal with retry", "work", `status='no_source'`, "23514"},
		{"pending final attempt", "work", `attempts=3`, "23514"},
		{"verification before final attempt", "work", `status='verify_outcome',attempts=2,retry_at=clock_timestamp()`, "23514"},
		{"verification without retry", "work", `status='verify_outcome',attempts=3,retry_at='infinity'`, "23514"},
		{"awaiting import with retry", "work", `status='awaiting_import',attempts=3,retry_at=clock_timestamp()`, "23514"},
		{"over budget", "work", `status='retry_exhausted',retry_at='infinity',attempts=4`, "23514"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, `UPDATE local_nfo_recovery_`+tc.table+` SET `+tc.patch+` WHERE item_id='nfo-file'`)
			var pgError *pgconn.PgError
			if !errors.As(err, &pgError) || pgError.Code != tc.state {
				t.Fatalf("invalid recovery state: error = %v, want PostgreSQL state %s", err, tc.state)
			}
		})
	}
	if result, err := pool.Exec(ctx, `UPDATE local_nfo_recovery_work SET attempts=2 WHERE status='pending';
		UPDATE local_nfo_recovery_work SET attempts=attempts+1,status='verify_outcome',retry_at='2026-01-01T00:45:00Z'
		WHERE status='pending' AND attempts=2 AND retry_at<='2026-01-01T00:00:00Z'`); err != nil || result.RowsAffected() != 2 {
		t.Fatalf("reserve both final writes for outcome verification: rows=%d error=%v", result.RowsAffected(), err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*)=2 FROM local_nfo_recovery_work
		WHERE status='verify_outcome' AND attempts=3 AND retry_at='2026-01-01T00:45:00Z'`).Scan(&valid); err != nil || !valid {
		t.Fatalf("final write reservations were not scheduled for verification: valid=%v error=%v", valid, err)
	}
	if result, err := pool.Exec(ctx, `UPDATE local_nfo_recovery_work
		SET status=CASE WHEN item_id='nfo-file' THEN 'retry_exhausted' ELSE 'awaiting_import' END,retry_at='infinity'
		WHERE status='verify_outcome' AND attempts=3 AND retry_at<='2026-01-01T00:45:00Z'`); err != nil || result.RowsAffected() != 2 {
		t.Fatalf("resolve both observed outcomes without reserving another write: rows=%d error=%v", result.RowsAffected(), err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*)=2 FROM local_nfo_recovery_work WHERE attempts=3 AND retry_at='infinity'
		AND (item_id,status) IN (('nfo-file','retry_exhausted'),('nfo-folder','awaiting_import'))`).Scan(&valid); err != nil || !valid {
		t.Fatalf("exhausted and awaiting-import work were not recorded separately: valid=%v error=%v", valid, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM scan_jobs WHERE id='nfo-file-scan';
		DELETE FROM items WHERE id='nfo-folder'`); err != nil {
		t.Fatalf("remove the scan and folder: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM local_nfo_recovery_sources)
		AND (SELECT count(*)=1 FROM local_nfo_recovery_work)
		AND EXISTS(SELECT 1 FROM local_nfo_recovery_work WHERE item_id='nfo-file' AND status='retry_exhausted'
			AND attempts=3 AND retry_at='infinity')`).Scan(&valid); err != nil || !valid {
		t.Fatalf("scan and item deletion did not preserve only the surviving item's work: %v", err)
	}
}
