//go:build linux

package backuppg

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/library"
)

func seedBitmapSubtitleArchiveState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type)
		VALUES('bitmap-library','Bitmap subtitle history','movies'),('bitmap-other-library','Other history','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('bitmap-root','bitmap-library','/retired/bitmap','/retired','bitmap'),
		('bitmap-next-root','bitmap-library','/retired/bitmap-next','/retired','bitmap-next'),
		('bitmap-other-root','bitmap-other-library','/retired/bitmap-other','/retired','bitmap-other');
		INSERT INTO items(id,library_id,root_id,relative_path,name,sort_name,type)
		VALUES('bitmap-item','bitmap-library','bitmap-root','nested/Film.mkv','Film','film','Movie');
		INSERT INTO item_subtitles(item_id,root_id,stream_index,active,relative_path,file_identity,source_hash,
		file_size,modified_at,change_time_ns,codec,mime_type)
		VALUES('bitmap-item','bitmap-root',1,false,'nested/Film.retired.srt','retired-text',repeat('c',64),100,'2026-01-01T00:00:00Z',9007199254740993,'srt','application/x-subrip');
		INSERT INTO item_owned_subtitles(item_id,root_id,stream_index,source_revision,active,codec,content,content_sha256)
		VALUES('bitmap-item','bitmap-root',2,'retired-primary-revision',false,'srt',decode('6162','hex'),encode(sha256(decode('6162','hex')),'hex'))`); err != nil {
		t.Fatal("seed independent bitmap subtitle identity namespace", err)
	}
	for _, test := range []struct {
		index          int
		paired, active bool
		sourceStream   int
	}{
		{4, false, true, 0}, {5, true, true, 0}, {6, true, true, 1}, {2147483647, false, false, 0},
	} {
		track, relative, raw := bitmapSubtitleArchiveFixture(t, test.paired)
		track.Index, track.SourceStreamIndex = test.index, test.sourceStream
		if test.sourceStream == 1 {
			track.Language, track.Title = "jpn", "Japanese"
			track.IsDefault, track.IsForced, track.IsHearingImpaired = false, true, true
		}
		track.Tag = library.BitmapSubtitleSourceHash(track.Format, track.SourceStreamIndex, track.Components)
		if _, err := pool.Exec(ctx, `INSERT INTO item_bitmap_subtitles(item_id,root_id,stream_index,active,
			relative_path,source_stream_index,format,codec,source_hash,language,title,is_default,is_forced,is_hearing_impaired,components)
			VALUES('bitmap-item','bitmap-root',$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, track.Index, test.active,
			relative, track.SourceStreamIndex, track.Format, track.Codec, track.Tag, track.Language, track.Title,
			track.IsDefault, track.IsForced, track.IsHearingImpaired, raw); err != nil {
			t.Fatal("seed retained SUP and multi-language IDX/SUB component snapshots", err)
		}
	}
}

func TestPostgreSQLBitmapSubtitleArchiveValidatesSnapshotsWithoutReauthorizingSources(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	seedBitmapSubtitleArchiveState(t, ctx, source)
	for _, test := range []struct {
		name, mutation string
		valid          bool
	}{
		{"retained_component_history", "", true},
		{"removed_probe_facts", `UPDATE items SET media=NULL,file_identity='',file_size=0,modified_at=NULL WHERE id='bitmap-item'`, true},
		{"new_embedded_indexes", `UPDATE items SET media='{"DurationTicks":100000000,"Streams":[{"Index":4,"CodecType":"subtitle","CodecName":"hdmv_pgs_subtitle"}]}' WHERE id='bitmap-item'`, true},
		{"same_library_move_before_scan", `UPDATE items SET root_id='bitmap-next-root' WHERE id='bitmap-item'`, true},
		{"reclassified_owner", `UPDATE items SET type='Audio' WHERE id='bitmap-item'`, true},
		{"cross_library_root", `UPDATE item_bitmap_subtitles SET root_id='bitmap-other-root' WHERE stream_index=4`, false},
		{"retired_cross_library_root", `UPDATE item_bitmap_subtitles SET root_id='bitmap-other-root' WHERE stream_index=2147483647`, false},
		{"text_index_collision", `UPDATE item_bitmap_subtitles SET stream_index=1 WHERE stream_index=4`, false},
		{"owned_index_collision", `UPDATE item_bitmap_subtitles SET stream_index=2 WHERE stream_index=4`, false},
		{"retired_index_collision", `UPDATE item_bitmap_subtitles SET stream_index=1 WHERE stream_index=2147483647`, false},
		{"noncanonical_path", `UPDATE item_bitmap_subtitles SET relative_path='nested//Film.en.sup' WHERE stream_index=4`, false},
		{"mismatched_relative_name", `UPDATE item_bitmap_subtitles SET relative_path='nested/Another.sup' WHERE stream_index=4`, false},
		{"unknown_component_field", `UPDATE item_bitmap_subtitles SET components=jsonb_set(components,'{0,Unknown}','true') WHERE stream_index=4`, false},
		{"mis_cased_component_field", `UPDATE item_bitmap_subtitles SET components=jsonb_set(components #- '{0,Name}','{0,name}','"Film.en.sup"') WHERE stream_index=4`, false},
		{"missing_component_field", `UPDATE item_bitmap_subtitles SET components=components #- '{0,Identity}' WHERE stream_index=4`, false},
		{"null_component_field", `UPDATE item_bitmap_subtitles SET components=jsonb_set(components,'{0,Size}','null') WHERE stream_index=4`, false},
		{"fractional_component_stamp", `UPDATE item_bitmap_subtitles SET components=jsonb_set(components,'{0,ModifiedNS}','1.5') WHERE stream_index=4`, false},
		{"traversing_component", `UPDATE item_bitmap_subtitles SET components=jsonb_set(components,'{0,Name}','"../Film.en.sup"') WHERE stream_index=4`, false},
		{"changed_pair_component", `UPDATE item_bitmap_subtitles SET components=jsonb_set(components,'{1,SHA256}',to_jsonb(repeat('d',64))) WHERE stream_index=5`, false},
		{"wrong_pair_companion", `UPDATE item_bitmap_subtitles SET components=jsonb_set(components,'{1,Name}','"Another.sub"') WHERE stream_index=5`, false},
		{"wrong_source_hash", `UPDATE item_bitmap_subtitles SET source_hash=repeat('0',64) WHERE stream_index=4`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			if err := configureTransaction(ctx, tx, options.Schema); err != nil {
				t.Fatal(err)
			}
			if test.mutation != "" {
				if _, err := tx.Exec(ctx, test.mutation); err != nil {
					t.Fatalf("apply SQL-valid bitmap subtitle mutation: %v", err)
				}
			}
			err = validateBitmapSubtitleState(ctx, tx, 61)
			if test.valid && err != nil || !test.valid && !errors.Is(err, ErrSchema) {
				t.Fatalf("bitmap subtitle archive validity = %v, result = %v", test.valid, err)
			}
		})
	}
}

func TestPostgreSQLBitmapSubtitleRawRestoreRetainsIndexesAndPairedSourceEvidence(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedBitmapSubtitleArchiveState(t, ctx, source)
	mediaDirectory := t.TempDir()
	sidecarDirectory := filepath.Join(mediaDirectory, "nested", "backdrops", "goby-subtitle-timelines", "retained-source")
	if err := os.MkdirAll(sidecarDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	witnesses := map[string]string{
		filepath.Join(mediaDirectory, "nested", "Film.en.sup"):    "unchanged SUP bytes",
		filepath.Join(mediaDirectory, "nested", "Film.multi.idx"): "unchanged IDX bytes",
		filepath.Join(mediaDirectory, "nested", "Film.multi.sub"): "unchanged SUB bytes",
		filepath.Join(sidecarDirectory, "manifest.json"):          "retained manifest outside the database archive",
		filepath.Join(sidecarDirectory, "gen-retained.gstl"):      "retained generated timeline outside the database archive",
	}
	for name, content := range witnesses {
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := source.Exec(ctx, `UPDATE library_roots SET path=$1,allowed_path=$1,relative_path='.' WHERE id='bitmap-root'`, mediaDirectory); err != nil {
		t.Fatal("bind source-side archive witnesses", err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	if _, err := Restore(ctx, source, target, archive, facts, options); err != nil {
		t.Fatalf("restore bitmap subtitle inventory: %v", err)
	}
	const witness = `SELECT jsonb_build_object(
		'bitmap',(SELECT jsonb_agg(to_jsonb(t) ORDER BY item_id,stream_index) FROM item_bitmap_subtitles t),
		'text',(SELECT jsonb_agg(to_jsonb(t) ORDER BY item_id,stream_index) FROM item_subtitles t),
		'owned',(SELECT jsonb_agg(to_jsonb(t) ORDER BY item_id,stream_index) FROM item_owned_subtitles t))::text`
	var before, after string
	if err := source.QueryRow(ctx, witness).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := target.QueryRow(ctx, witness).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after || !json.Valid([]byte(after)) {
		t.Fatal("raw recovery changed public stream identities, language facts, or exact component snapshots")
	}
	for name, content := range witnesses {
		actual, err := os.ReadFile(name)
		if err != nil || string(actual) != content {
			t.Fatalf("database recovery touched source-side subtitle or timeline file %s: %v", filepath.Base(name), err)
		}
	}
}

func TestPostgreSQLBitmapSubtitleSnapshotAndRestoreRejectUnknownSourceEvidence(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedBitmapSubtitleArchiveState(t, ctx, source)
	valid, err := OpenSnapshot(ctx, source, options)
	if err != nil {
		t.Fatal("read exact source identity before introducing malformed evidence", err)
	}
	plan := valid.plan
	_ = valid.Close()
	if _, err := source.Exec(ctx, `UPDATE item_bitmap_subtitles SET components=jsonb_set(components,'{0,Unknown}','true') WHERE stream_index=4`); err != nil {
		t.Fatal(err)
	}
	before := historicalArchiveRows(t, ctx, source, "item_bitmap_subtitles", 61)
	snapshot, err := OpenSnapshot(ctx, source, options)
	if snapshot != nil {
		_ = snapshot.Close()
	}
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("snapshot accepted unknown bitmap component fields: %v", err)
	}
	// Construct a real archive whose schema and fingerprints are correct but
	// whose component payload is invalid. Restore must apply its own semantic
	// boundary instead of trusting row hashes or a previous snapshot check.
	archive, facts := uncheckedThemeArchive(t, ctx, source, plan)
	result, err := RestoreOffline(ctx, target, archive, facts, options)
	if !errors.Is(err, ErrArchive) || result.CurrentVersion != 0 {
		t.Fatalf("restore trusted row hashes without valid bitmap source evidence: %v", err)
	}
	assertThemeRestoreTargetEmpty(t, ctx, target, options.Schema)
	if historicalArchiveRows(t, ctx, source, "item_bitmap_subtitles", 61) != before {
		t.Fatal("backup or rejected restore repaired the invalid source inventory")
	}
}

func TestPostgreSQLBitmapSubtitleMigrationKeepsSchema60TimelineHistory(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 60)
	seedSubtitleTimelineArchiveState(t, ctx, source)
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 60 || result.CurrentVersion != currentRecoveryVersion(t) {
		t.Fatalf("restore schema 60 before bitmap inventory migration: %v", err)
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	assertHistoricalRecoveryFacts(t, ctx, source, target, facts, after, sequences, targetSequences)
	var empty bool
	if err := target.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM item_bitmap_subtitles)`).Scan(&empty); err != nil || !empty {
		t.Fatalf("historical restore invented external subtitle evidence: %v", err)
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}
