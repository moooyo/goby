//go:build linux

package recovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

const selectedPhase2RecoveryRevision int64 = 9007199254740993

type selectedPhase2RecoveryOperation struct {
	id, itemID, kind, state, phase, restoredState string
	changed                                       bool
	row                                           string
}

type selectedPhase2RecoveryFixture struct {
	operations []selectedPhase2RecoveryOperation
	files      map[string][]byte
	image      []byte
}

const selectedPhase2RecoveryRawSQL = `SELECT jsonb_build_object(
	'operations',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM media_operations o),
	'cues',(SELECT jsonb_agg(to_jsonb(c) ORDER BY operation_id,ordinal) FROM media_operation_cues c),
	'owned',(SELECT jsonb_agg(to_jsonb(s) ORDER BY item_id,stream_index) FROM item_owned_subtitles s),
	'covers',(SELECT jsonb_agg(to_jsonb(a) ORDER BY item_id) FROM item_embedded_artwork a))::text`

// These are the only mutable execution fields. Everything else, including
// request evidence, journals, reviewed text, image bytes and published assets,
// remains part of the exact preservation witness.
const selectedPhase2RecoveryRetainedSQL = `SELECT jsonb_build_object(
	'operations',(SELECT jsonb_agg(to_jsonb(o)-ARRAY['state','revision','worker_token',
		'cancel_requested_at','apply_actor_id','apply_credential_id','apply_request_id',
		'apply_fingerprint','apply_revision','error_code','error_message','finished_at','updated_at'] ORDER BY id)
		FROM media_operations o),
	'cues',(SELECT jsonb_agg(to_jsonb(c) ORDER BY operation_id,ordinal) FROM media_operation_cues c),
	'owned',(SELECT jsonb_agg(to_jsonb(s) ORDER BY item_id,stream_index) FROM item_owned_subtitles s),
	'covers',(SELECT jsonb_agg(to_jsonb(a) ORDER BY item_id) FROM item_embedded_artwork a),
	'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i),
	'roots',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM library_roots r))::text`

// Abandoned execution rows are seeded at their durable checkpoint boundary.
// The archive, authenticated fingerprints, finalizer, owner startup and fresh
// administrator apply/recovery admissions all use their production paths.
func TestEngineSelectedPhase2ArchiveRestoreClearsExecutionAuthority(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newEngineRecoveryFixture(t)
	phase2 := seedSelectedPhase2Recovery(t, f)
	rawBefore := recoveryEngineJSONState(t, f.ctx, f.source, selectedPhase2RecoveryRawSQL)
	retainedBefore := recoveryEngineJSONState(t, f.ctx, f.source, selectedPhase2RecoveryRetainedSQL)
	manifest, metadata := f.create(t)
	for _, name := range []string{"media_operations", "media_operation_cues", "item_owned_subtitles", "item_embedded_artwork"} {
		found := false
		for _, fact := range manifest.Source.Tables {
			found = found || fact.Name == name && fact.Rows > 0
		}
		if !found {
			t.Fatalf("the authenticated archive omitted populated phase two table %s", name)
		}
	}
	reader, err := f.objects.Snapshot(f.ctx, metadata.ID)
	if err != nil {
		t.Fatal("open the encrypted media operation archive")
	}
	defer reader.Close()
	passphrase := []byte("recovery-integration-passphrase")
	defer clear(passphrase)
	archive, err := f.engine.OpenArchive(f.ctx, reader, passphrase)
	if err != nil {
		t.Fatalf("authenticate the real media operation archive: %v", err)
	}
	defer archive.Close()
	releaseRecoveryEngineTestMemory()
	lease, err := database.AcquireLease(f.ctx, f.target)
	if err != nil {
		t.Fatal("lease the independently owned restore target")
	}
	defer lease.Close()

	// Refuse this first transaction only after witnessing the exact imported
	// rows. The same extracted archive is then restored by Archive.RestoreInto.
	refused := errors.New("phase two raw restore witness completed")
	called := false
	result, err := backuppg.RestoreFinalized(f.ctx, f.source, f.target, archive.Database(), manifest.Source, f.engine.options,
		func(ctx context.Context, tx pgx.Tx, raw backuppg.RestoreResult) error {
			called = true
			if raw.SourceVersion != manifest.Source.SchemaVersion || raw.CurrentVersion != manifest.Source.SchemaVersion || !reflect.DeepEqual(raw.Tables, manifest.Source.Tables) {
				t.Error("raw archive fingerprints changed before the application finalizer")
			}
			var state string
			if err := tx.QueryRow(ctx, selectedPhase2RecoveryRawSQL).Scan(&state); err != nil || state != rawBefore {
				t.Errorf("raw recovery lost execution grants, review evidence or publication journals before normalization: %v", err)
			}
			return refused
		})
	if !called || !errors.Is(err, refused) || result.SourceVersion != 0 || len(result.Tables) != 0 {
		t.Fatalf("raw archive witness did not roll back its complete transaction: %v", err)
	}
	var empty bool
	if err := f.target.QueryRow(f.ctx, `SELECT to_regclass('media_operations') IS NULL`).Scan(&empty); err != nil || !empty {
		t.Fatal("a refused finalizer committed imported media operation rows")
	}
	if _, err := archive.Database().Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the exact authenticated archive after the raw witness")
	}
	normalized, err := archive.RestoreInto(f.ctx, f.target, lease, f.targetConfig)
	if err != nil {
		t.Fatalf("restore media operations through the real application finalizer: %v", err)
	}
	var changed int64
	for _, operation := range phase2.operations {
		if operation.changed {
			changed++
		}
	}
	if normalized.SourceVersion != manifest.Source.SchemaVersion || normalized.CurrentVersion != manifest.Source.SchemaVersion || normalized.NormalizedMediaOperations != changed {
		t.Fatal("the finalizer did not report the exact imported media operation normalization")
	}
	assertSelectedPhase2RecoveryState(t, f.ctx, f.target, phase2, retainedBefore)
	assertSelectedPhase2RecoveryFiles(t, phase2)

	// Opening the restored catalog and repeating owner recovery must not
	// resume jobs or turn an imported cancellation into staging deletion.
	catalog, err := library.New(f.target, media.Prober{FFprobePath: f.targetConfig.FFprobePath, FFmpegPath: f.targetConfig.FFmpegPath}, f.targetConfig.MediaRoots)
	if err != nil {
		t.Fatalf("open the restored catalog owner: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := catalog.Close(ctx); err != nil {
			t.Error("close the restored media operation owner")
		}
	}()
	if err := catalog.RecoverMediaOperations(f.ctx); err != nil {
		t.Fatalf("normalize restored owner startup without executing media work: %v", err)
	}
	assertSelectedPhase2RecoveryState(t, f.ctx, f.target, phase2, retainedBefore)
	pending, err := catalog.PendingMediaOperations(f.ctx, 100)
	if err != nil || len(pending) != 0 {
		t.Fatal("restored operations became runnable without a new administrator request")
	}
	for _, operation := range phase2.operations {
		if _, err := catalog.ClaimMediaOperation(f.ctx, operation.id); !errors.Is(err, library.ErrMediaOperationState) {
			t.Fatalf("restored %s/%s operation retained an automatic claim: %v", operation.kind, operation.state, err)
		}
	}
	assertSelectedPhase2RecoveryFiles(t, phase2)

	restoredIdentities := identity.New(f.target)
	login, err := restoredIdentities.Authenticate(f.ctx, f.actor.User.Name, "recovery-administrator-password", identity.Client{Name: "Phase two recovery administrator"}, "admin")
	if err != nil {
		t.Fatal("issue a fresh restored administrator credential")
	}
	actor, err := restoredIdentities.Resolve(f.ctx, login.Token, "admin")
	if err != nil {
		t.Fatal("resolve the current restored administrator")
	}
	for _, operation := range phase2.operations {
		if operation.restoredState != "ready" && operation.restoredState != "recovery_required" {
			continue
		}
		current, err := catalog.GetMediaOperation(f.ctx, actor, operation.id)
		if err != nil {
			t.Fatalf("read preserved review or publication recovery state: %v", err)
		}
		request := library.MediaOperationApplyRequest{Revision: current.Revision, SourceRevision: current.SourceRevision, ResultHash: current.ResultHash, RequestID: "new-restored-grant-" + operation.id}
		admit := catalog.ApplyMediaOperation
		if operation.restoredState == "recovery_required" {
			admit = catalog.RecoverMediaOperation
		}
		if _, err := admit(f.ctx, f.actor, operation.id, request); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("an archived administrator principal could authorize restored media work: %v", err)
		}
		if operation.kind == library.MediaOperationOCR {
			data, err := catalog.GetMediaOperationCueImage(f.ctx, actor, operation.id, 0)
			if err != nil || !bytes.Equal(data, phase2.image) {
				t.Fatal("the fresh administrator cannot read the preserved original OCR image")
			}
		}
		admitted, err := admit(f.ctx, actor, operation.id, request)
		if err != nil || !admitted.Admitted || admitted.Operation.State != "applying" || admitted.Operation.ApplyActor.SessionID != actor.SessionID {
			t.Fatalf("a fresh explicit administrator request did not establish new media authority: %v", err)
		}
		work, err := catalog.ClaimMediaOperation(f.ctx, operation.id)
		if err != nil || !work.Apply || work.Discard || work.Operation.ApplyActor.SessionID != actor.SessionID || work.Token == "" {
			t.Fatalf("a fresh explicit media request could not acquire its fenced claim: %v", err)
		}
	}
	// Admission and claim do not themselves publish or purge media. Executor
	// filesystem behavior is covered by the library publication fixtures.
	assertSelectedPhase2RecoveryFiles(t, phase2)
	if after := recoveryEngineJSONState(t, f.ctx, f.source, selectedPhase2RecoveryRawSQL); after != rawBefore {
		t.Fatal("archive recovery changed the original operation history")
	}
	if after := recoveryEngineJSONState(t, f.ctx, f.source, selectedPhase2RecoveryRetainedSQL); after != retainedBefore {
		t.Fatal("archive recovery changed source review, subtitle or artwork bytes")
	}
}

func seedSelectedPhase2Recovery(t *testing.T, f *engineRecoveryFixture) selectedPhase2RecoveryFixture {
	t.Helper()
	fixture := selectedPhase2RecoveryFixture{files: make(map[string][]byte)}
	var rootID, rootPath string
	if err := f.source.QueryRow(f.ctx, `SELECT id,path FROM library_roots WHERE library_id=$1`, f.libraryID).Scan(&rootID, &rootPath); err != nil {
		t.Fatal("read the existing approved media operation root")
	}
	// Publication admission correctly excludes a concurrently active scan.
	if _, err := f.source.Exec(f.ctx, `UPDATE scan_jobs SET status='Interrupted' WHERE library_id=$1`, f.libraryID); err != nil {
		t.Fatal("finish the scan witness before seeding publication checkpoints")
	}
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 120, G: 180, B: 220, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal("encode the retained review and embedded cover image")
	}
	fixture.image = encoded.Bytes()
	imageDigest := sha256.Sum256(fixture.image)
	imageHash := hex.EncodeToString(imageDigest[:])
	info, err := json.Marshal(media.Info{ProbeVersion: media.CurrentProbeVersion, Container: "matroska", DurationTicks: 60 * media.TicksPerSecond,
		Streams: []media.Stream{{Index: 0, CodecType: "video", Codec: "h264"}, {Index: 7, CodecType: "subtitle", Codec: "hdmv_pgs_subtitle", Language: "eng"}}})
	if err != nil {
		t.Fatal("encode the durable source probe snapshot")
	}
	for _, checkpoint := range []struct {
		kind, state, phase, restored string
		changed, cancelled           bool
	}{
		{library.MediaOperationOCR, "queued", "none", "interrupted", true, true},
		{library.MediaOperationOCR, "running", "none", "interrupted", true, false},
		{library.MediaOperationOCR, "ready", "none", "ready", false, false},
		{library.MediaOperationOCR, "applying", "none", "ready", true, true},
		{library.MediaOperationRemoveSubtitle, "applying", "none", "recovery_required", true, true},
		{library.MediaOperationRemoveSubtitle, "applying", "prepared", "recovery_required", true, true},
		{library.MediaOperationRemoveSubtitle, "applying", "catalog_committed", "recovery_required", true, true},
		{library.MediaOperationRemoveSubtitle, "interrupted", "none", "interrupted", true, true},
		{library.MediaOperationOCR, "completed", "done", "completed", false, false},
	} {
		op := selectedPhase2RecoveryOperation{id: recoveryEngineTestID(t), itemID: recoveryEngineTestID(t), kind: checkpoint.kind,
			state: checkpoint.state, phase: checkpoint.phase, restoredState: checkpoint.restored, changed: checkpoint.changed}
		filename := op.itemID + ".mkv"
		path := filepath.Join(rootPath, filename)
		content := []byte("original media checkpoint " + op.id)
		fixture.files[path] = content
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal("create the unchanged primary media witness")
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path,media,file_identity,file_size,modified_at)
			VALUES($1,$2,$3,$2,'Phase two recovery media','phase two recovery media',CASE WHEN $8='completed' THEN 'Audio' ELSE 'Movie' END,
			$4,$5,$6,$1,$7,'2026-09-01T00:00:00Z')`, op.itemID, f.libraryID, rootID, path, filename, info, len(content), op.state); err != nil {
			t.Fatal("seed the source-bound media operation item")
		}
		journal := []byte(`{}`)
		if op.kind == library.MediaOperationRemoveSubtitle {
			stageName := ".goby-delete-" + op.id
			stage := filepath.Join(rootPath, stageName)
			if err := os.Mkdir(stage, 0700); err != nil {
				t.Fatal("create the retained publication staging witness")
			}
			for _, name := range []string{"candidate", "payload"} {
				file := filepath.Join(stage, name)
				fixture.files[file] = []byte(name + " retained checkpoint " + op.id)
				if err := os.WriteFile(file, fixture.files[file], 0600); err != nil {
					t.Fatal("create a retained publication payload")
				}
			}
			journal, err = json.Marshal(map[string]any{"Version": 1, "OperationID": op.id, "StageName": stageName,
				"StageIdentity": "retained-stage-" + op.id, "Checkpoint": op.phase, "OriginalPath": path})
			if err != nil {
				t.Fatal("encode the retained publication checkpoint")
			}
		}
		if _, err := f.source.Exec(f.ctx, `INSERT INTO media_operations(id,kind,revision,item_id,library_id,root_id,
			source_item_id,source_library_id,source_root_id,request_actor_id,request_credential_id,request_id,request_fingerprint,
			media_source_id,source_revision,stream_index,parameters,source_snapshot,execution_snapshot,state,worker_token,
			progress_stage,processed,total,cancel_requested_at,result_summary,result_hash,publication_phase,journal,
			apply_actor_id,apply_credential_id,apply_request_id,apply_fingerprint,apply_revision,created_at,updated_at)
			SELECT $1,$2,$3,i.id,i.library_id,i.root_id,i.id,i.library_id,i.root_id,$4,$5,'archived-request-'||$1,decode(repeat('ab',32),'hex'),
			$6,`+library.MediaOperationSourceRevisionSQL+`,7,
			CASE WHEN $2='subtitle_ocr' THEN '{"ModelIds":["eng"],"OutputFormat":"vtt","Language":"eng","Title":"Retained OCR"}'::jsonb ELSE '{}'::jsonb END,
			jsonb_build_object('Media',i.media),'{}',$7,CASE WHEN $7 IN ('running','applying') THEN repeat('c',32) ELSE '' END,
			'checkpoint',2,3,CASE WHEN $8 THEN '2026-09-02T00:00:00Z'::timestamptz ELSE NULL END,
			'{"CueCount":1,"Retained":true}',repeat('d',64),$9,$10,
			CASE WHEN $7='applying' THEN $4 ELSE '' END,CASE WHEN $7='applying' THEN $5 ELSE '' END,
			CASE WHEN $7='applying' THEN 'archived-apply-'||$1 ELSE '' END,
			CASE WHEN $7='applying' THEN decode(repeat('ef',32),'hex') ELSE NULL END,
			CASE WHEN $7='applying' THEN $3-1 ELSE NULL END,'2026-09-01T00:00:00Z','2026-09-02T00:00:00Z'
			FROM items i WHERE i.id=$11`, op.id, op.kind, selectedPhase2RecoveryRevision, f.actor.User.ID, f.actor.SessionID,
			media.SourceID(op.itemID), op.state, checkpoint.cancelled, op.phase, journal, op.itemID); err != nil {
			t.Fatalf("seed a durable media operation checkpoint: %v", err)
		}
		if op.kind == library.MediaOperationOCR && (op.state == "ready" || op.state == "applying" || op.state == "completed") {
			if _, err := f.source.Exec(f.ctx, `INSERT INTO media_operation_cues(operation_id,ordinal,original_start_ticks,original_end_ticks,
				original_text,start_ticks,end_ticks,text,included,confidence,warnings,image_sha256,image_png,is_forced,is_hearing_impaired)
				VALUES($1,0,0,20000000,'Original OCR',10000000,30000000,'Reviewed retained OCR',true,42.5,
				'["Retained low confidence warning"]',$2,$3,true,true)`, op.id, imageHash, fixture.image); err != nil {
				t.Fatal("seed distinct original and reviewed OCR evidence")
			}
		}
		if op.state == "completed" {
			content := []byte("WEBVTT\n\n00:00:01.000 --> 00:00:03.000\nPublished retained OCR\n")
			digest := sha256.Sum256(content)
			if _, err := f.source.Exec(f.ctx, `INSERT INTO item_owned_subtitles(item_id,root_id,stream_index,operation_id,source_revision,
				codec,language,title,is_default,is_forced,is_hearing_impaired,content,content_sha256)
				SELECT item_id,root_id,8,id,source_revision,'vtt','eng','Retained published OCR',true,true,true,$2,$3
				FROM media_operations WHERE id=$1`, op.id, content, hex.EncodeToString(digest[:])); err != nil {
				t.Fatal("seed published subtitle bytes independently of the worker")
			}
			if _, err := f.source.Exec(f.ctx, `INSERT INTO item_embedded_artwork(item_id,source_revision,probe_version,extraction_version,
				status,stream_index,picture_type,source_hash,mime_type,width,height,content)
				SELECT item_id,'embedded-source-v1-'||substring(source_revision FROM length('media-operation-source-v1-')+1),
				$2,1,'ready',9,'Front',$3,'image/png',2,2,$4 FROM media_operations WHERE id=$1`, op.id, media.CurrentProbeVersion, imageHash, fixture.image); err != nil {
				t.Fatal("seed retained embedded cover bytes and their source binding")
			}
		}
		if err := f.source.QueryRow(f.ctx, `SELECT to_jsonb(o)::text FROM media_operations o WHERE id=$1`, op.id).Scan(&op.row); err != nil {
			t.Fatal("capture the exact historical operation row")
		}
		fixture.operations = append(fixture.operations, op)
	}
	return fixture
}

func assertSelectedPhase2RecoveryState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture selectedPhase2RecoveryFixture, retained string) {
	t.Helper()
	if current := recoveryEngineJSONState(t, ctx, pool, selectedPhase2RecoveryRetainedSQL); current != retained {
		t.Fatal("recovery changed immutable operation evidence, journals, cues, subtitle bytes or embedded covers")
	}
	for _, operation := range fixture.operations {
		var state, phase, row string
		var revision int64
		var authorityCleared bool
		if err := pool.QueryRow(ctx, `SELECT state,publication_phase,revision,
			worker_token='' AND cancel_requested_at IS NULL AND apply_actor_id='' AND apply_credential_id=''
			AND apply_request_id='' AND apply_fingerprint IS NULL AND apply_revision IS NULL,to_jsonb(o)::text
			FROM media_operations o WHERE id=$1`, operation.id).Scan(&state, &phase, &revision, &authorityCleared, &row); err != nil {
			t.Fatalf("read the normalized media operation: %v", err)
		}
		wantRevision := selectedPhase2RecoveryRevision
		if operation.changed {
			wantRevision++
		}
		if state != operation.restoredState || phase != operation.phase || revision != wantRevision || !authorityCleared {
			t.Fatalf("recovered %s/%s operation retained an unsafe state, stale authority or incorrect revision", operation.kind, operation.state)
		}
		if !operation.changed && row != operation.row {
			t.Fatal("recovery changed an already safe ready draft or completed operation history")
		}
	}
}

func assertSelectedPhase2RecoveryFiles(t *testing.T, fixture selectedPhase2RecoveryFixture) {
	t.Helper()
	for path, expected := range fixture.files {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatal("restore or owner startup renamed, replaced or purged a retained media checkpoint")
		}
	}
}
