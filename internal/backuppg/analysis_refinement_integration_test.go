//go:build linux

package backuppg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/library"
)

func TestPostgreSQLAnalysisRefinementPayloadPreservesVersionedBoundsThroughRawRestore(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedAnalysisArchiveState(t, ctx, source)
	const duration = 600 * introdetect.TicksPerSecond
	value := library.AnalysisFeatures{ContentSHA256: strings.Repeat("a", 64), AlgorithmProfile: "archive-refinement-v1",
		Audio: make([]introdetect.AudioSample, 4800), Visual: make([]introdetect.VisualSample, 1200),
		Refinement: make([]introdetect.RefinementSample, 1200)}
	for index := range value.Audio {
		value.Audio[index] = introdetect.AudioSample{StartTicks: int64(index) * introdetect.TicksPerSecond / 8,
			EndTicks: int64(index+1) * introdetect.TicksPerSecond / 8, Fingerprint: uint32(index)}
	}
	for index := range value.Visual {
		value.Visual[index] = introdetect.VisualSample{Ticks: int64(index) * introdetect.TicksPerSecond / 2,
			Hash: uint64(index), Contrast: 200, LumaKnown: true, Luma: [64]int8{32, -32}}
	}
	for index := range value.Refinement {
		value.Refinement[index].Ticks = int64(index) * introdetect.TicksPerSecond / 10
		for cell := range value.Refinement[index].Raster {
			value.Refinement[index].Raster[cell] = byte(index*17 + cell*31)
		}
	}
	payload, err := library.EncodeAnalysisFeatures(value, duration)
	if err != nil || len(payload) <= 256<<10 || len(payload) > 512<<10 || binary.LittleEndian.Uint16(payload[4:6]) != 3 {
		t.Fatalf("the bounded production-sized refinement payload was not encoded: bytes=%d error=%v", len(payload), err)
	}
	// Retained caches carry their own source snapshot and do not grant live
	// execution authority. This independent entry exercises actual large bytea
	// storage and archive transport without opening a historical media source.
	profile := strings.Repeat("f", 64)
	key := analysisStateFeatureKey("analysis-history-item", "refinement-source", profile)
	if _, err := source.Exec(ctx, `INSERT INTO analysis_feature_cache(cache_key,item_id,source_revision,profile_fingerprint,
		content_sha256,algorithm_profile,duration_ticks,payload,bytes)
		VALUES($1,'analysis-history-item','refinement-source',$2,$3,$4,$5,$6,$7)`,
		key, profile, value.ContentSHA256, value.AlgorithmProfile, duration, payload, len(payload)); err != nil {
		t.Fatalf("schema 53 did not store a refinement payload larger than 256 KiB: %v", err)
	}
	var saved []byte
	if err := source.QueryRow(ctx, `SELECT payload FROM analysis_feature_cache WHERE cache_key=$1`, key).Scan(&saved); err != nil || !bytes.Equal(saved, payload) {
		t.Fatalf("the real feature-cache row changed its binary payload: %v", err)
	}
	decoded, err := library.DecodeAnalysisFeatures(saved, duration)
	if err != nil || !reflect.DeepEqual(decoded, value) {
		t.Fatalf("the stored full refinement snapshot failed readback: %v", err)
	}
	for _, version := range []int64{50, 51, 52, 53} {
		tx, err := source.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := configureTransaction(ctx, tx, options.Schema); err != nil {
			rollback(tx)
			t.Fatal(err)
		}
		err = validateAnalysisFeatureState(ctx, tx, version)
		rollback(tx)
		if version < 53 && !errors.Is(err, ErrSchema) || version == 53 && err != nil {
			t.Fatalf("schema %d changed its original feature-payload admission: %v", version, err)
		}
	}
	for _, codec := range []int{1, 2} {
		tx, err := source.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := configureTransaction(ctx, tx, options.Schema); err != nil {
			rollback(tx)
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE analysis_feature_cache SET payload=set_byte(payload,4,$2) WHERE cache_key=$1`, key, codec); err != nil {
			rollback(tx)
			t.Fatal(err)
		}
		err = validateAnalysisFeatureState(ctx, tx, 53)
		rollback(tx)
		if !errors.Is(err, ErrSchema) {
			t.Fatalf("schema 53 incorrectly enlarged historical codec %d: %v", codec, err)
		}
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	if facts.SchemaVersion != 53 {
		t.Fatalf("the refinement archive is not bound to schema 53: %d", facts.SchemaVersion)
	}
	if _, err := Restore(ctx, source, target, archive, facts, options); err != nil {
		t.Fatalf("restore the actual large refinement cache through the verified archive: %v", err)
	}
	var restored []byte
	if err := target.QueryRow(ctx, `SELECT payload FROM analysis_feature_cache WHERE cache_key=$1`, key).Scan(&restored); err != nil || !bytes.Equal(restored, payload) {
		t.Fatalf("raw restore lost or changed the complete refinement payload: %v", err)
	}
	decoded, err = library.DecodeAnalysisFeatures(restored, duration)
	if err != nil || !reflect.DeepEqual(decoded, value) {
		t.Fatalf("restored refinement samples differ from the original source snapshot: %v", err)
	}
}
