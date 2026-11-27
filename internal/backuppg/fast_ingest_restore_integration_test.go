//go:build linux

package backuppg

import "testing"

func TestPostgreSQLFastIngestArchivePreservesIndependentQueues(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	if _, err := source.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('ingest-lib','Ingest','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES('ingest-root','ingest-lib','/media/ingest','/media','ingest');
		INSERT INTO items(id,library_id,root_id,name,sort_name,type,path,relative_path) VALUES
			('ingest-film','ingest-lib','ingest-root','Film','film','Movie','/media/ingest/film.mp4','film.mp4');
		INSERT INTO scan_jobs(id,library_id,status,finished_at) VALUES('ingest-scan','ingest-lib','Completed',clock_timestamp());
		INSERT INTO local_metadata_ingest(item_id,scan_job_id,directory_identity,source_key,index_number,parent_index_number,parent_number_defined,import_nfo,import_images,enrich_online)
		VALUES('ingest-film','ingest-scan','1:2','["source"]',0,0,false,true,true,true);
		INSERT INTO video_seek_ingest(item_id,scan_job_id,source_revision,index_version,retry_at)
		VALUES('ingest-film','ingest-scan','source',1,'2026-01-01T00:00:00Z');
		UPDATE task_system_events SET sequence=1,occurred_at=clock_timestamp() WHERE name IN
		('LocalMetadataIngestRequested','OnlineMetadataIngestRequested','VideoSeekIngestRequested');`); err != nil {
		t.Fatal(err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	result, err := Restore(ctx, source, target, archive, facts, options)
	if err != nil || result.SourceVersion != currentRecoveryVersion(t) || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore pending ingest state: %v", err)
	}
	var restored bool
	if err := target.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM local_metadata_ingest WHERE item_id='ingest-film' AND
		scan_job_id='ingest-scan' AND import_nfo AND import_images AND enrich_online) AND
		EXISTS(SELECT 1 FROM video_seek_ingest WHERE item_id='ingest-film' AND indexes IS NULL AND retry_at='2026-01-01T00:00:00Z') AND
		(SELECT count(*) FROM task_system_events WHERE sequence=1 AND name IN
		('LocalMetadataIngestRequested','OnlineMetadataIngestRequested','VideoSeekIngestRequested'))=3`).Scan(&restored); err != nil || !restored {
		t.Fatalf("archive did not retain recoverable work, retry state and dedicated signals: %v", err)
	}
}
