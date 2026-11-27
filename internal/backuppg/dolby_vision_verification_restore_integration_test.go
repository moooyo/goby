//go:build linux

package backuppg

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedDolbyVisionArchiveSource(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	// Preserve preexisting inline evidence as ordinary item data, even though
	// it was never written by the independent verification task.
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('dv-library','Dolby Vision','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('dv-root','dv-library','/media/dv','/media','dv');
		INSERT INTO items(id,library_id,root_id,name,sort_name,type,path,relative_path,media)
		VALUES('dv-item','dv-library','dv-root','Film','film','Movie','/media/dv/film.mp4','film.mp4',
		'{"ProbeVersion":8,"Streams":[{"Index":2,"Codec":"hevc","CodecType":"video","DolbyVision":{
		"Profile":8,"Level":6,"RPUPresent":true,"BLPresent":true,"CompatibilityID":1,"MetadataCompression":"none",
		"RPUVerified":true,"RPUProfile":8,"ResidualDisabled":true,"RPUFrameCount":240}}]}');
		INSERT INTO scan_jobs(id,library_id,status,finished_at)
		VALUES('dv-scan','dv-library','Completed','2026-01-01T00:00:00Z');
		INSERT INTO local_metadata_ingest(item_id,scan_job_id,directory_identity,source_key,index_number,parent_index_number,parent_number_defined,import_nfo,import_images,enrich_online)
		VALUES('dv-item','dv-scan','1:2','["retained-source"]',0,0,false,true,false,false);
		INSERT INTO video_seek_ingest(item_id,scan_job_id,source_revision,index_version,retry_at)
		VALUES('dv-item','dv-scan','retained-source',1,'2026-01-01T01:00:00Z');
		UPDATE task_system_events SET sequence=7,occurred_at='2026-01-01T02:00:00.123456Z' WHERE name='VideoSeekIngestRequested'`); err != nil {
		t.Fatalf("seed schema79-compatible source and independent ingest work: %v", err)
	}
}

func TestPostgreSQLDolbyVisionVerificationArchiveRoundTrip(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 80)
	seedDolbyVisionArchiveSource(t, ctx, source)
	if _, err := source.Exec(ctx, `INSERT INTO dolby_vision_verifications
		(item_id,stream_index,scan_job_id,source_revision,verifier_version,tool_identity,evidence,attempts,retry_at,verified_at) VALUES
		('dv-item',2,'dv-scan','revision-1',1,'ffmpeg:fixture',
		'{"StreamIndex":2,"Verified":true,"Profile":8,"ResidualMixed":false,"ResidualDisabled":true,"FrameCount":240,"InBandParameterSets":true,"Reason":""}',
		1,'infinity','2026-01-01T03:00:00.123456Z'),
		('dv-item',3,'dv-scan','revision-1',1,'ffmpeg:fixture',NULL,DEFAULT,DEFAULT,NULL),
		('dv-item',4,'dv-scan','revision-1',1,'ffmpeg:fixture',NULL,2,'2026-01-01T04:00:00.123456Z',NULL),
		('dv-item',5,'dv-scan','revision-1',1,'ffmpeg:fixture',
		'{"StreamIndex":5,"Verified":false,"Profile":0,"ResidualMixed":false,"ResidualDisabled":false,"FrameCount":0,"InBandParameterSets":false,"Reason":"unsupported_configuration"}',
		1,'infinity',NULL),
		('dv-item',6,'dv-scan','revision-1',1,'ffmpeg:fixture',
		'{"StreamIndex":6,"Verified":false,"Profile":0,"ResidualMixed":false,"ResidualDisabled":false,"FrameCount":0,"InBandParameterSets":false,"Reason":"scan_timeout"}',
		3,'infinity',NULL);
		UPDATE task_system_events SET sequence=9007199254740993,occurred_at='2026-01-01T05:00:00.123456Z'
		WHERE name='DolbyVisionVerificationRequested'`); err != nil {
		t.Fatalf("seed independent verification result, queue, and signal states: %v", err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	if facts.SchemaVersion != 80 {
		t.Fatalf("verification archive used unexpected schema %d", facts.SchemaVersion)
	}
	var rows int64
	for _, fact := range facts.Tables {
		if fact.Name == "dolby_vision_verifications" {
			rows = fact.Rows
			break
		}
	}
	if rows != 5 {
		t.Fatalf("snapshot verification row count = %d, want 5", rows)
	}
	_, sequences := unchangedSourceWitness(t, ctx, source, options)
	result, err := Restore(ctx, source, target, archive, facts, options)
	if err != nil || result.SourceVersion != 80 || result.CurrentVersion != currentRecoveryVersion(t) || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore raw verification state: %v", err)
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	assertHistoricalRecoveryFacts(t, ctx, source, target, facts, after, sequences, targetSequences)
}

func TestPostgreSQLDolbyVisionVerificationSchema79ArchiveUpgradesWithoutInventingEvidence(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 79)
	seedDolbyVisionArchiveSource(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	_, sequences := unchangedSourceWitness(t, ctx, source, options)
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 79 || result.CurrentVersion != currentRecoveryVersion(t) || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore and upgrade the published schema79 archive: %v", err)
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	assertHistoricalRecoveryFacts(t, ctx, source, target, facts, after, sequences, targetSequences)
	var neutral bool
	if err := target.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM dolby_vision_verifications)
		AND EXISTS (SELECT 1 FROM task_system_events WHERE name='DolbyVisionVerificationRequested'
		AND sequence=0 AND occurred_at IS NOT NULL AND lifecycle_key='')`).Scan(&neutral); err != nil || !neutral {
		t.Fatalf("schema79 upgrade invented verification evidence or pending work: %v", err)
	}
	assertSourceWitness(t, ctx, source, options, facts, sequences)
}
