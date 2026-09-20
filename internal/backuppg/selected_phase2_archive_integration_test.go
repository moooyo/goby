//go:build linux

package backuppg

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type selectedPhase2Query interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func selectedPhase2ArchiveWitness(t *testing.T, ctx context.Context, query selectedPhase2Query) string {
	t.Helper()
	var snapshot string
	if err := query.QueryRow(ctx, `SELECT jsonb_build_object(
		'operations',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM media_operations o),
		'cues',(SELECT jsonb_agg(to_jsonb(c) ORDER BY operation_id,ordinal) FROM media_operation_cues c),
		'owned',(SELECT jsonb_agg(to_jsonb(s) ORDER BY item_id,stream_index) FROM item_owned_subtitles s),
		'covers',(SELECT jsonb_agg(to_jsonb(a) ORDER BY item_id) FROM item_embedded_artwork a))::text`).Scan(&snapshot); err != nil {
		t.Fatalf("read complete media operation and derivative archive witness: %v", err)
	}
	return snapshot
}

func seedSelectedPhase2ArchiveWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	picture.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 127, B: 63, A: 255})
	picture.SetNRGBA(1, 0, color.NRGBA{R: 15, G: 31, B: 63, A: 127})
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, picture); err != nil {
		t.Fatalf("encode the private cue and cover storage witness: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type)
		VALUES('phase2-library','Media Operation Archive','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('phase2-root','phase2-library','/synthetic/phase2','/synthetic','phase2');
		INSERT INTO items(id,library_id,root_id,name,sort_name,type,path,relative_path,file_identity,file_size,modified_at,media)
		SELECT 'phase2-'||name,'phase2-library','phase2-root',name,name,kind,
		 '/synthetic/phase2/'||name||'.mkv',name||'.mkv','retained-'||name,4096,'2020-01-03T00:00:00Z',
		 '{"ProbeVersion":6,"DurationTicks":90000000,"Container":"mkv","Streams":[]}'::jsonb
		FROM (VALUES('review','Movie'),('published','Movie'),('prepared','Movie'),('committed','Movie'),
		 ('running','Movie'),('cover','Audio'),('no-cover','Audio'),('failed-cover','Audio')) AS fixture(name,kind);
		INSERT INTO media_operations(id,kind,revision,item_id,library_id,root_id,source_item_id,source_library_id,source_root_id,
		 request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,source_revision,stream_index,
		 parameters,source_snapshot,execution_snapshot,state,worker_token,progress_stage,processed,total,
		 result_summary,result_hash,publication_phase,journal,apply_actor_id,apply_credential_id,
		 apply_request_id,apply_fingerprint,apply_revision,created_at,updated_at,started_at,finished_at)
		SELECT repeat(letter,32),kind,9007199254740993,'phase2-'||name,'phase2-library','phase2-root',
		 'phase2-'||name,'phase2-library','phase2-root','backup-admin','old-native-credential','archive-'||name,
		 decode(repeat(letter,64),'hex'),'mediasource_phase2-'||name,'retained-source-'||name,2,
		 CASE WHEN kind='subtitle_ocr' THEN '{"ModelIds":["eng"],"OutputFormat":"srt","Language":"en","Title":"Reviewed caption"}'::jsonb
		 ELSE '{"Profile":"matroska-v1"}'::jsonb END,
		 jsonb_build_object('ItemID','phase2-'||name,'RelativePath',name||'.mkv','FileIdentity','retained-'||name,
		  'Media',jsonb_build_object('DurationTicks',90000000),'ExactInteger',9007199254740993),
		 '{"Tool":"private-execution-witness","Version":1}'::jsonb,state,
		 CASE WHEN state IN ('running','applying') THEN repeat('f',32) ELSE '' END,'retained-stage',2,3,
		 '{"Retained":[null,true,"exact"]}'::jsonb,repeat(letter,64),phase,
		 CASE WHEN kind='remove_embedded_subtitle' THEN jsonb_build_object('Version',1,'StageName','.goby-retained-'||name,
		  'Host',repeat('1',64),'SourceHash',repeat('2',64),'CandidateHash',repeat('3',64),'ExactInteger',9007199254740993)
		 ELSE '{}'::jsonb END,
		 CASE WHEN state='applying' THEN 'backup-admin' ELSE '' END,
		 CASE WHEN state='applying' THEN 'old-native-credential' ELSE '' END,
		 CASE WHEN state='applying' THEN 'old-apply-'||name ELSE '' END,
		 CASE WHEN state='applying' THEN decode(repeat('a1',32),'hex') ELSE NULL END,
		 CASE WHEN state='applying' THEN 9007199254740991 ELSE NULL END,
		 '2020-01-04T00:00:00Z','2020-01-05T00:00:00Z','2020-01-04T00:00:01Z',
		 CASE WHEN state='completed' THEN '2020-01-05T00:00:00Z'::timestamptz ELSE NULL END
		FROM (VALUES('a','review','subtitle_ocr','ready','none'),('b','published','subtitle_ocr','completed','none'),
		 ('c','prepared','remove_embedded_subtitle','applying','prepared'),
		 ('d','committed','remove_embedded_subtitle','applying','catalog_committed'),
		 ('e','running','subtitle_ocr','running','none')) AS fixture(letter,name,kind,state,phase)`); err != nil {
		t.Fatalf("seed retained operation revisions and publication journals: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO media_operation_cues(operation_id,ordinal,original_start_ticks,original_end_ticks,
		 original_text,start_ticks,end_ticks,text,included,confidence,warnings,image_sha256,image_png,is_forced,is_hearing_impaired)
		VALUES(repeat('a',32),0,10000000,20000000,'Original OCR line',11000000,22000000,'Reviewed OCR line',true,91.25,
		 '["low_contrast"]',encode(sha256($1::bytea),'hex'),$1,true,false),
		 (repeat('a',32),1,30000000,40000000,'Original excluded line',31000000,42000000,'Reviewed excluded line',false,NULL,
		 '[]','',decode('','hex'),false,true)`, imageBytes.Bytes()); err != nil {
		t.Fatalf("seed exact original and reviewed OCR cue bytes: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO item_owned_subtitles(item_id,root_id,stream_index,operation_id,source_revision,active,codec,language,title,
		 is_default,is_forced,is_hearing_impaired,content,content_sha256,created_at,updated_at,retired_at)
		VALUES('phase2-published','phase2-root',7,repeat('b',32),'retained-owned-source',true,'srt','en','Final reviewed caption',
		 true,true,false,$1,encode(sha256($1::bytea),'hex'),'2020-01-06T00:00:00Z','2020-01-07T00:00:00Z',NULL),
		 ('phase2-published','phase2-root',8,NULL,'outdated-owned-source',false,'srt','en','Retired caption',
		 false,false,true,$1,encode(sha256($1::bytea),'hex'),'2020-01-06T00:00:00Z','2020-01-07T00:00:00Z','2020-01-07T00:00:00Z')`,
		[]byte("1\n00:00:01,100 --> 00:00:02,200\nReviewed caption with \\N retained.\n\n")); err != nil {
		t.Fatalf("seed exact published and retired owned subtitle bytes: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO item_embedded_artwork(item_id,source_revision,probe_version,extraction_version,status,
		 stream_index,picture_type,source_hash,mime_type,width,height,content,inspected_at)
		VALUES('phase2-cover','embedded-source-v1-'||repeat('1',32),6,1,'ready',3,'Front',
		 encode(sha256($1::bytea),'hex'),'image/png',2,1,$1,'2020-01-08T00:00:00.123456Z')`, imageBytes.Bytes()); err != nil {
		t.Fatalf("seed exact embedded cover payload and provenance: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO item_embedded_artwork(item_id,source_revision,probe_version,extraction_version,status,failure_code,inspected_at)
		VALUES('phase2-no-cover','embedded-source-v1-'||repeat('2',32),6,1,'none','','2020-01-08T00:00:01.234567Z'),
		 ('phase2-failed-cover','embedded-source-v1-'||repeat('3',32),6,1,'failed','extraction_failed','2020-01-08T00:00:02.345678Z')`); err != nil {
		t.Fatalf("seed retained missing and failed cover decisions: %v", err)
	}
}

func TestPostgreSQLSelectedPhase2ArchivePreservesReviewPublicationAndDerivativeState(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedSelectedPhase2ArchiveWitness(t, ctx, source)
	want := selectedPhase2ArchiveWitness(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	counts := make(map[string]int64)
	for _, table := range facts.Tables {
		counts[table.Name] = table.Rows
	}
	for name, expected := range map[string]int64{"media_operations": 5, "media_operation_cues": 2,
		"item_owned_subtitles": 2, "item_embedded_artwork": 3} {
		if counts[name] != expected {
			t.Fatalf("archive omitted nonempty phase2 table %s: rows=%d want=%d", name, counts[name], expected)
		}
	}
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.CurrentVersion != currentRecoveryVersion(t) || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore the complete raw phase2 archive: %v", err)
	}
	if selectedPhase2ArchiveWitness(t, ctx, target) != want {
		t.Fatal("raw restoration changed OCR originals or edits, publication evidence, derivative bytes, or CAS stamps")
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	if !equalJSON(after, facts) || !reflect.DeepEqual(targetSequences, sequences) {
		t.Fatal("raw phase2 restoration changed complete archived fingerprints or sequence state")
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func TestPostgreSQLSelectedPhase2Schema43UpgradeDoesNotInferMediaWork(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 43)
	seedThemeSnapshotWitness(t, ctx, source)
	if _, err := source.Exec(ctx, `UPDATE users SET local_credentials_revision=9007199254740993,
		profile_pin_ciphertext=decode('47505001'||repeat('91',32),'hex'),local_password_failures=2,
		local_password_blocked_until='2030-01-01T00:00:00Z' WHERE id='backup-admin';
		INSERT INTO item_intro_state(item_id,revision,source_revision,start_ticks,end_ticks,provenance,last_edited_by,last_edited_at)
		VALUES('theme-owner',9007199254740993,'retained-schema43-source',10000000,20000000,'Manual','backup-admin',
		 '2020-01-03T00:00:00.123456Z')`); err != nil {
		t.Fatalf("seed existing schema43 credential and intro state: %v", err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	if facts.SchemaVersion != 43 || len(facts.Tables) != 49 || len(facts.MigrationChecksums) != 43 {
		t.Fatal("the phase2 upgrade fixture is not the actual published schema43 archive")
	}
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 43 || result.CurrentVersion != currentRecoveryVersion(t) || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("upgrade the original schema43 archive: %v", err)
	}
	var empty bool
	if err := target.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM media_operations)
		AND NOT EXISTS(SELECT 1 FROM media_operation_cues) AND NOT EXISTS(SELECT 1 FROM item_owned_subtitles)
		AND NOT EXISTS(SELECT 1 FROM item_embedded_artwork)`).Scan(&empty); err != nil || !empty {
		t.Fatalf("historical restoration inferred media jobs, OCR results, owned subtitles, or cover caches: %v", err)
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	assertHistoricalRecoveryFacts(t, ctx, source, target, facts, after, sequences, targetSequences)
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func TestPostgreSQLSelectedPhase2RestoreRejectsCorruptDerivativeFinalizerAndRetries(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedSelectedPhase2ArchiveWitness(t, ctx, source)
	want := selectedPhase2ArchiveWitness(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	for _, fixture := range []struct{ name, mutation string }{
		{"cue_image", `UPDATE media_operation_cues SET image_sha256=repeat('0',64) WHERE operation_id=repeat('a',32) AND ordinal=0`},
		{"owned_subtitle", `UPDATE item_owned_subtitles SET content_sha256=repeat('0',64) WHERE item_id='phase2-published' AND stream_index=7`},
		{"embedded_cover", `UPDATE item_embedded_artwork SET source_hash=repeat('0',64) WHERE item_id='phase2-cover'`},
		{"publication_owner", `UPDATE items SET root_id=NULL WHERE id='phase2-prepared'`},
	} {
		if !t.Run(fixture.name, func(t *testing.T) {
			called, mutated := false, false
			failed, err := RestoreOfflineFinalized(ctx, target, archive, facts, offline,
				func(ctx context.Context, tx pgx.Tx, raw RestoreResult) error {
					called = true
					if !equalJSON(raw.Tables, facts.Tables) || selectedPhase2ArchiveWitness(t, ctx, tx) != want {
						return errors.New("the finalizer did not receive the exact archived media state")
					}
					tag, err := tx.Exec(ctx, fixture.mutation)
					mutated = err == nil && tag.RowsAffected() == 1
					return err
				})
			if !called || !mutated || !errors.Is(err, ErrSchema) || failed.CurrentVersion != 0 {
				t.Fatalf("restoration committed SQL-valid but invalid media derivative state: %v", err)
			}
			assertThemeRestoreTargetEmpty(t, ctx, target, options.Schema)
			assertSourceWitness(t, ctx, source, options, before, sequences)
			if _, err := archive.Seek(0, 0); err != nil {
				t.Fatal("rewind the unchanged phase2 archive after finalizer rollback")
			}
		}) {
			return
		}
	}
	if _, err := RestoreOffline(ctx, target, archive, facts, offline); err != nil {
		t.Fatalf("retry the unchanged phase2 archive after every rollback: %v", err)
	}
	if selectedPhase2ArchiveWitness(t, ctx, target) != want {
		t.Fatal("retry changed the original media operation or derivative rows")
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}
