//go:build linux

package recoverydb

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/lifecycle"
)

// Reuse the exclusive pair owned by the existing recovery-store sequence.
// Mutations alter one durable table at a time, so an unrelated write cannot
// conceal a missing phase two fingerprint from the retained reset proof.
func assertRecoverySelectedPhase2ResetProtection(t *testing.T, f *recoveryStoreFixture, original Retained) {
	t.Helper()
	f.exec(t, f.source, `INSERT INTO libraries(id,name,collection_type)
		VALUES('reset-phase2-library','Media operation reset witness','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('reset-phase2-root','reset-phase2-library','/offline/phase2/media','/offline/phase2','media');
		INSERT INTO items(id,library_id,root_id,name,sort_name,type,relative_path)
		VALUES('reset-phase2-item','reset-phase2-library','reset-phase2-root','Media operation reset witness','media operation reset witness','Movie','movie.mkv');
		INSERT INTO media_operations(id,kind,revision,item_id,library_id,root_id,source_item_id,source_library_id,source_root_id,
			request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,source_revision,stream_index,
			parameters,source_snapshot,execution_snapshot,state,result_summary,result_hash)
		VALUES(repeat('7',32),'subtitle_ocr',9007199254740993,'reset-phase2-item','reset-phase2-library','reset-phase2-root',
			'reset-phase2-item','reset-phase2-library','reset-phase2-root','recovery-admin','historical-phase2-credential',
			'phase2-reset-request',decode(repeat('ab',32),'hex'),'phase2-source','phase2-source-revision',7,
			'{"ModelIds":["eng"],"OutputFormat":"vtt"}','{}','{}','ready','{"CueCount":1}',repeat('a',64));
		INSERT INTO media_operation_cues(operation_id,ordinal,original_start_ticks,original_end_ticks,original_text,
			start_ticks,end_ticks,text,confidence,warnings)
		VALUES(repeat('7',32),0,0,20000000,'Original reset OCR',10000000,30000000,'Reviewed reset OCR',42.5,'["Retained warning"]');
		INSERT INTO media_operations(id,kind,item_id,library_id,root_id,source_item_id,source_library_id,source_root_id,
			request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,source_revision,stream_index,
			parameters,source_snapshot,execution_snapshot,state,result_hash,publication_phase,journal)
		VALUES(repeat('8',32),'remove_embedded_subtitle','reset-phase2-item','reset-phase2-library','reset-phase2-root',
			'reset-phase2-item','reset-phase2-library','reset-phase2-root','recovery-admin','historical-phase2-credential',
			'phase2-publication-reset-request',decode(repeat('bc',32),'hex'),'phase2-source','phase2-source-revision',7,
			'{}','{}','{}','recovery_required',repeat('b',64),'prepared',
			'{"Version":1,"StageName":"retained-publication-stage","Checkpoint":"prepared"}')`)
	oldSubtitle := []byte("WEBVTT\n\n00:00:01.000 --> 00:00:03.000\nOriginal reset subtitle\n")
	newSubtitle := []byte("WEBVTT\n\n00:00:01.000 --> 00:00:03.000\nChanged reset subtitle\n")
	encodeImage := func(value uint8) []byte {
		img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		img.SetNRGBA(0, 0, color.NRGBA{R: value, G: 128, B: 255, A: 255})
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, img); err != nil {
			t.Fatal("encode the phase two reset image witness")
		}
		return encoded.Bytes()
	}
	oldImage, newImage := encodeImage(40), encodeImage(80)
	hash := func(data []byte) string {
		digest := sha256.Sum256(data)
		return hex.EncodeToString(digest[:])
	}
	f.exec(t, f.source, `UPDATE media_operation_cues SET image_png=$1,image_sha256=$2 WHERE operation_id=repeat('7',32)`, oldImage, hash(oldImage))
	f.exec(t, f.source, `INSERT INTO item_owned_subtitles(item_id,root_id,stream_index,source_revision,codec,content,content_sha256)
		VALUES('reset-phase2-item','reset-phase2-root',8,'phase2-source-revision','vtt',$1,$2)`, oldSubtitle, hash(oldSubtitle))
	f.exec(t, f.source, `INSERT INTO item_embedded_artwork(item_id,source_revision,probe_version,extraction_version,status,
		stream_index,picture_type,source_hash,mime_type,width,height,content)
		VALUES('reset-phase2-item','embedded-source-v1-'||repeat('a',32),8,1,'ready',9,'Front',$2,'image/png',1,1,$1)`, oldImage, hash(oldImage))

	observe := func(name, table string, mutate, undo func(*testing.T)) {
		t.Helper()
		if !t.Run(name, func(t *testing.T) {
			before := f.capture(t, f.source)
			mutate(t)
			after := f.capture(t, f.source)
			if before.RawMarker != after.RawMarker || before.Marker != after.Marker || len(before.Facts.Tables) != len(after.Facts.Tables) {
				t.Fatal("a phase two state mutation changed the local generation or catalog inventory")
			}
			changed := false
			for index, previous := range before.Facts.Tables {
				current := after.Facts.Tables[index]
				if reflect.DeepEqual(previous, current) {
					continue
				}
				if previous.Name != table || current.Name != table || current.Rows != previous.Rows || changed {
					t.Fatal("a phase two reset witness changed an unexpected durable table or row count")
				}
				changed = true
			}
			if !changed {
				t.Fatalf("the retained reset proof omitted changed phase two state in %s", table)
			}
			if err := f.source.store.ResetOwnedTarget(f.ctx,
				recoveryActive(lifecycle.DatabaseRecovery, recoveryFixtureGeneration), before); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale media operation fingerprints authorized an owned target reset: %v", err)
			}
			f.assertFacts(t, f.source, after.Facts)
			f.assertNamespace(t, f.source, false)
			f.assertEmpty(t, f.target)
			undo(t)
			f.assertFacts(t, f.source, before.Facts)
		}) {
			t.Fatal("stop the exclusive recovery sequence after an inexact phase two reset result")
		}
	}
	observe("review_revision", "media_operations", func(t *testing.T) {
		f.exec(t, f.source, `UPDATE media_operations SET revision=9007199254740994 WHERE id=repeat('7',32)`)
	}, func(t *testing.T) {
		f.exec(t, f.source, `UPDATE media_operations SET revision=9007199254740993 WHERE id=repeat('7',32)`)
	})
	observe("apply_authority", "media_operations", func(t *testing.T) {
		f.exec(t, f.source, `UPDATE media_operations SET apply_actor_id='recovery-admin',apply_credential_id='new-native-credential',
			apply_request_id='new-explicit-apply',apply_fingerprint=decode(repeat('cd',32),'hex'),apply_revision=9007199254740993 WHERE id=repeat('7',32)`)
	}, func(t *testing.T) {
		f.exec(t, f.source, `UPDATE media_operations SET apply_actor_id='',apply_credential_id='',apply_request_id='',apply_fingerprint=NULL,apply_revision=NULL WHERE id=repeat('7',32)`)
	})
	observe("publication_journal", "media_operations", func(t *testing.T) {
		f.exec(t, f.source, `UPDATE media_operations SET journal='{"Version":1,"StageName":"new-retained-publication-stage","Checkpoint":"prepared"}' WHERE id=repeat('8',32)`)
	}, func(t *testing.T) {
		f.exec(t, f.source, `UPDATE media_operations SET journal='{"Version":1,"StageName":"retained-publication-stage","Checkpoint":"prepared"}' WHERE id=repeat('8',32)`)
	})
	observe("catalog_committed_checkpoint", "media_operations", func(t *testing.T) {
		f.exec(t, f.source, `UPDATE media_operations SET publication_phase='catalog_committed' WHERE id=repeat('8',32)`)
	}, func(t *testing.T) {
		f.exec(t, f.source, `UPDATE media_operations SET publication_phase='prepared' WHERE id=repeat('8',32)`)
	})
	observe("corrected_cue_text", "media_operation_cues", func(t *testing.T) {
		f.exec(t, f.source, `UPDATE media_operation_cues SET text='Newly reviewed reset OCR' WHERE operation_id=repeat('7',32) AND ordinal=0`)
	}, func(t *testing.T) {
		f.exec(t, f.source, `UPDATE media_operation_cues SET text='Reviewed reset OCR' WHERE operation_id=repeat('7',32) AND ordinal=0`)
	})
	observe("original_cue_image", "media_operation_cues", func(t *testing.T) {
		f.exec(t, f.source, `UPDATE media_operation_cues SET image_png=$1,image_sha256=$2 WHERE operation_id=repeat('7',32) AND ordinal=0`, newImage, hash(newImage))
	}, func(t *testing.T) {
		f.exec(t, f.source, `UPDATE media_operation_cues SET image_png=$1,image_sha256=$2 WHERE operation_id=repeat('7',32) AND ordinal=0`, oldImage, hash(oldImage))
	})
	observe("owned_subtitle_content", "item_owned_subtitles", func(t *testing.T) {
		f.exec(t, f.source, `UPDATE item_owned_subtitles SET content=$1,content_sha256=$2 WHERE item_id='reset-phase2-item' AND stream_index=8`, newSubtitle, hash(newSubtitle))
	}, func(t *testing.T) {
		f.exec(t, f.source, `UPDATE item_owned_subtitles SET content=$1,content_sha256=$2 WHERE item_id='reset-phase2-item' AND stream_index=8`, oldSubtitle, hash(oldSubtitle))
	})
	observe("embedded_cover_content", "item_embedded_artwork", func(t *testing.T) {
		f.exec(t, f.source, `UPDATE item_embedded_artwork SET content=$1,source_hash=$2 WHERE item_id='reset-phase2-item'`, newImage, hash(newImage))
	}, func(t *testing.T) {
		f.exec(t, f.source, `UPDATE item_embedded_artwork SET content=$1,source_hash=$2 WHERE item_id='reset-phase2-item'`, oldImage, hash(oldImage))
	})
	observe("embedded_cover_source", "item_embedded_artwork", func(t *testing.T) {
		f.exec(t, f.source, `UPDATE item_embedded_artwork SET source_revision='embedded-source-v1-'||repeat('b',32) WHERE item_id='reset-phase2-item'`)
	}, func(t *testing.T) {
		f.exec(t, f.source, `UPDATE item_embedded_artwork SET source_revision='embedded-source-v1-'||repeat('a',32) WHERE item_id='reset-phase2-item'`)
	})
	f.exec(t, f.source, `DELETE FROM media_operations WHERE id IN (repeat('7',32),repeat('8',32));
		DELETE FROM libraries WHERE id='reset-phase2-library'`)
	f.assertFacts(t, f.source, original.Facts)
	if f.read(t, f.source) != original.RawMarker {
		t.Fatal("phase two reset protection changed the retained local generation")
	}
}
