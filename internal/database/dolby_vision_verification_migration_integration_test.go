package database_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/database"
)

func TestDolbyVisionVerificationMigrationBoundsStateAndEvents(t *testing.T) {
	ctx, pool := migrationTestPool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('dv-library','DV','movies');
		INSERT INTO items(id,library_id,name,sort_name,type) VALUES
		('dv-first','dv-library','First','first','Movie'),('dv-second','dv-library','Second','second','Movie');
		INSERT INTO scan_jobs(id,library_id,status,finished_at) VALUES
		('dv-scan-first','dv-library','Completed',clock_timestamp()),('dv-scan-second','dv-library','Completed',clock_timestamp());
		INSERT INTO task_definitions(id,key,name) VALUES(repeat('a',32),'dv-fixture','Verify Dolby Vision');
		INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind,system_event) VALUES
		(repeat('b',32),repeat('a',32),1,0,'system_event','DolbyVisionVerificationRequested');
		INSERT INTO dolby_vision_verifications(item_id,stream_index,scan_job_id,source_revision,verifier_version,tool_identity) VALUES
		('dv-first',0,'dv-scan-first',repeat('r',128),1,repeat('t',512)),
		('dv-first',1,'dv-scan-first','revision',1,'tool'),('dv-second',0,'dv-scan-second','revision',1,'tool')`); err != nil {
		t.Fatalf("seed a new event trigger and independently queued stream identities: %v", err)
	}
	var pending bool
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM dolby_vision_verifications WHERE evidence IS NULL AND attempts=0 AND retry_at='-infinity' AND verified_at IS NULL)=3
		AND EXISTS(SELECT 1 FROM task_system_events WHERE name='DolbyVisionVerificationRequested'
			AND sequence=0 AND occurred_at IS NOT NULL AND lifecycle_key='')`).Scan(&pending); err != nil || !pending {
		t.Fatalf("verification migration did not initialize neutral queue and counter defaults: %v", err)
	}
	for _, tc := range []struct{ name, statement, state string }{
		{"negative stream", `stream_index=-1`, "23514"},
		{"duplicate stream", `stream_index=1`, "23505"},
		{"missing item", `item_id='absent'`, "23503"},
		{"missing scan", `scan_job_id='absent'`, "23503"},
		{"empty revision", `source_revision=''`, "23514"},
		{"oversized revision", `source_revision=repeat('r',129)`, "23514"},
		{"oversized tool", `tool_identity=repeat('t',513)`, "23514"},
		{"invalid verifier", `verifier_version=0`, "23514"},
		{"retry limit", `attempts=4`, "23514"},
		{"pending without retry", `retry_at='infinity'`, "23514"},
		{"pending verification stamp", `verified_at=clock_timestamp()`, "23514"},
		{"JSON null", `evidence='null',retry_at='infinity'`, "23514"},
		{"array evidence", `evidence='[]',retry_at='infinity'`, "23514"},
		{"wrong stream evidence", `evidence='{"StreamIndex":1,"Reason":"scan_timeout"}',retry_at='infinity'`, "23514"},
		{"uncensored reason", `evidence='{"StreamIndex":0,"Reason":"not_a_closed_reason"}',retry_at='infinity'`, "23514"},
		{"unknown fields", `evidence='{"StreamIndex":0,"Diagnostic":"stderr"}',retry_at='infinity'`, "23514"},
		{"oversized evidence", `evidence=jsonb_build_object('StreamIndex',0,'Reason',repeat('x',2048)),retry_at='infinity'`, "23514"},
		{"verification without timestamp", `evidence='{"StreamIndex":0,"Verified":true,"FrameCount":1}',retry_at='infinity'`, "23514"},
		{"verification with infinite timestamp", `evidence='{"StreamIndex":0,"Verified":true,"FrameCount":1}',retry_at='infinity',verified_at='infinity'`, "23514"},
		{"terminal result with retry", `evidence='{"StreamIndex":0,"Reason":"scan_timeout"}'`, "23514"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, `UPDATE dolby_vision_verifications SET `+tc.statement+` WHERE item_id='dv-first' AND stream_index=0`)
			var pgError *pgconn.PgError
			if !errors.As(err, &pgError) || pgError.Code != tc.state {
				t.Fatalf("invalid verification state: error = %v, want PostgreSQL state %s", err, tc.state)
			}
		})
	}
	for _, statement := range []string{
		`UPDATE task_triggers SET system_event='UnknownVerificationRequested'`,
		`INSERT INTO task_system_events(name) VALUES ('UnknownVerificationRequested')`,
	} {
		_, err := pool.Exec(ctx, statement)
		var pgError *pgconn.PgError
		if !errors.As(err, &pgError) || pgError.Code != "23514" {
			t.Fatalf("verification event migration admitted an unrecognized event: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM items WHERE id='dv-first'`); err != nil {
		t.Fatal(err)
	}
	var itemsRemoved bool
	if err := pool.QueryRow(ctx, `SELECT count(*)=1 AND min(item_id)='dv-second' FROM dolby_vision_verifications`).Scan(&itemsRemoved); err != nil || !itemsRemoved {
		t.Fatalf("item cascade did not remove exactly the item's verification rows: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM scan_jobs WHERE id='dv-scan-second'`); err != nil {
		t.Fatal(err)
	}
	var scanRemoved bool
	if err := pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM dolby_vision_verifications)
		AND EXISTS(SELECT 1 FROM items WHERE id='dv-second')`).Scan(&scanRemoved); err != nil || !scanRemoved {
		t.Fatalf("scan cascade did not remove only the scan's verification rows: %v", err)
	}
}
