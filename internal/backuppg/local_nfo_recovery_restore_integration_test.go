//go:build linux

package backuppg

import "testing"

func TestPostgreSQLLocalNFORecoveryArchivePreservesPrivateEvidence(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	if _, err := source.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('nfo-library','NFO','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('nfo-root','nfo-library','/media/nfo','/media','nfo');
		INSERT INTO items(id,library_id,root_id,name,sort_name,type,path,relative_path) VALUES
		('nfo-film','nfo-library','nfo-root','Film','film','Movie','/media/nfo/film.mp4','film.mp4'),
		('nfo-unsourced','nfo-library','nfo-root','Other','other','Movie','/media/nfo/other.mp4','other.mp4');
		INSERT INTO scan_jobs(id,library_id,status,finished_at) VALUES('nfo-scan','nfo-library','Completed','2026-01-01T00:00:00Z');
		INSERT INTO local_nfo_recovery_sources
		(item_id,scan_job_id,source_key,directory_identity,root_identity,relative_path,media_change_ns,document,document_hash) VALUES
		('nfo-film','nfo-scan','film-source','1:2','1:3','film.nfo',9007199254740993,
		decode('efbbbf3c6d6f7669652f3e0d0a','hex'),repeat('a',64));
		INSERT INTO local_nfo_recovery_work
		(item_id,source_key,directory_identity,root_identity,relative_path,media_change_ns,version,reason,confirmed_at,attempts,retry_at,status) VALUES
		('nfo-film','film-source','1:2','1:3','film.nfo',9007199254740993,'damaged-film','malformed',
		'2026-01-01T00:00:01.123456Z',2,'2026-01-01T00:01:01.123456Z','pending'),
		('nfo-unsourced','other-source','1:2','1:3','other.nfo',42,'damaged-other','empty',
		'2026-01-01T00:00:04.654321Z',0,'infinity','no_source');
		UPDATE task_system_events SET sequence=7,occurred_at='2026-01-01T00:00:04.654321Z' WHERE name='NFORecoveryRequested'`); err != nil {
		t.Fatalf("seed independent NFO recovery source, work and event state: %v", err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	assertCurrentRecoveryFacts(t, facts)
	rows := make(map[string]int64)
	for _, fact := range facts.Tables {
		rows[fact.Name] = fact.Rows
	}
	if rows["local_nfo_recovery_sources"] != 1 || rows["local_nfo_recovery_work"] != 2 {
		t.Fatal("the archive omitted private NFO evidence or recovery work")
	}
	result, err := Restore(ctx, source, target, archive, facts, options)
	if err != nil || result.SourceVersion != facts.SchemaVersion || result.CurrentVersion != facts.SchemaVersion || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore exact NFO recovery evidence and work: %v", err)
	}
	for _, table := range []string{"local_nfo_recovery_sources", "local_nfo_recovery_work", "task_system_events"} {
		if historicalArchiveRows(t, ctx, source, table, facts.SchemaVersion) != historicalArchiveRows(t, ctx, target, table, facts.SchemaVersion) {
			t.Errorf("restore changed rows in %s", table)
		}
	}
}
