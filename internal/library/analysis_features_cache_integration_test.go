package library

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
)

func analysisCacheTestFeatures() AnalysisFeatures {
	return AnalysisFeatures{ContentSHA256: strings.Repeat("a1", 32), AlgorithmProfile: "analysis-cache-test-v1",
		Audio:  []introdetect.AudioSample{{StartTicks: 0, EndTicks: introdetect.TicksPerSecond, Fingerprint: 0xa55aa55a}},
		Visual: []introdetect.VisualSample{{Ticks: 0, Hash: 0x12345678, Contrast: 400}}}
}

func TestAnalysisFeatureCacheBindsSourceProfileAndImmutableContent(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	collection := libraryIntegrationCreate(t, ctx, store, "Analysis cache", "mixed", root)
	source := AnalysisSource{ItemID: "analysis-cache-item", SourceRevision: strings.Repeat("b2", 32), DurationTicks: 120 * introdetect.TicksPerSecond}
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type) VALUES($1,$2,'Cache source','cache source','Video')`, source.ItemID, collection.ID); err != nil {
		t.Fatal(err)
	}
	profile, fingerprint := DefaultAnalysisProfile(), strings.Repeat("c3", 32)
	value := analysisCacheTestFeatures()
	write := func(value AnalysisFeatures) error {
		return store.WithOwnedTx(ctx, func(tx OwnedTx) error { return putAnalysisFeatureCache(tx, source, fingerprint, profile, value) })
	}
	if err := write(value); err != nil {
		t.Fatal(err)
	}
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		read, found, err := loadAnalysisFeatureCache(tx, source, fingerprint)
		if err != nil || !found || !reflect.DeepEqual(read, value) {
			t.Fatalf("cache round trip lost feature facts: %+v %v %v", read, found, err)
		}
		changed := source
		changed.SourceRevision = strings.Repeat("d4", 32)
		if _, found, err := loadAnalysisFeatureCache(tx, changed, fingerprint); err != nil || found {
			t.Fatalf("a different source revision reused cached features: %v %v", found, err)
		}
		if _, found, err := loadAnalysisFeatureCache(tx, source, strings.Repeat("e5", 32)); err != nil || found {
			t.Fatalf("a different profile reused cached features: %v %v", found, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var before, after string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(cache)::text FROM analysis_feature_cache cache WHERE item_id=$1`, source.ItemID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*AnalysisFeatures){
		func(value *AnalysisFeatures) { value.ContentSHA256 = strings.Repeat("f6", 32) },
		func(value *AnalysisFeatures) { value.AlgorithmProfile = "different-extraction-facts" },
	} {
		conflicting := value
		mutate(&conflicting)
		if err := write(conflicting); !errors.Is(err, ErrAnalysisSourceChanged) {
			t.Fatalf("contradictory immutable cache identity was overwritten: %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT to_jsonb(cache)::text FROM analysis_feature_cache cache WHERE item_id=$1`, source.ItemID).Scan(&after); err != nil || after != before {
			t.Fatalf("rejected cache replacement changed the accepted entry: %v", err)
		}
	}
	for _, corruption := range []string{
		`UPDATE analysis_feature_cache SET payload=set_byte(payload,0,0)`,
		`UPDATE analysis_feature_cache SET source_revision=repeat('9',64)`,
		`UPDATE analysis_feature_cache SET duration_ticks=duration_ticks+1`,
		`UPDATE analysis_feature_cache SET content_sha256=repeat('8',64)`,
	} {
		if _, err := pool.Exec(ctx, corruption); err != nil {
			t.Fatal(err)
		}
		if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
			read, found, err := loadAnalysisFeatureCache(tx, source, fingerprint)
			if err != nil || found || !reflect.DeepEqual(read, AnalysisFeatures{}) {
				t.Fatalf("corrupt cache returned usable evidence: %+v %v %v", read, found, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM analysis_feature_cache`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("corrupt entry was not discarded: %d: %v", count, err)
		}
		if err := write(value); err != nil {
			t.Fatal(err)
		}
	}
	actor := metadataEditTestActor(t, ctx, pool, "analysis-cache-config-editor")
	configuration, err := store.GetAnalysisConfiguration(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateAnalysisConfiguration(ctx, actor, AnalysisConfigurationUpdate{Revision: configuration.Revision, Profile: configuration.Profile}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM analysis_feature_cache`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("no-op configuration update invalidated cached evidence: %d: %v", count, err)
	}
	configuration.Profile.PreviewQuality++
	if _, err := store.UpdateAnalysisConfiguration(ctx, actor, AnalysisConfigurationUpdate{Revision: configuration.Revision, Profile: configuration.Profile}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM analysis_feature_cache`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("profile change kept old cache entries: %d: %v", count, err)
	}
}

func TestAnalysisFeatureCacheEvictsLeastRecentlyUsedBytesAndBoundsRows(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	collection := libraryIntegrationCreate(t, ctx, store, "Bounded analysis cache", "mixed", root)
	profile, fingerprint := DefaultAnalysisProfile(), strings.Repeat("c3", 32)
	profile.FeatureCacheMaxBytes = 1 << 20
	value := analysisCacheTestFeatures()
	value.Audio = make([]introdetect.AudioSample, analysisFeaturesMaxAudio)
	for index := range value.Audio {
		start := int64(index) * (introdetect.TicksPerSecond / 100)
		value.Audio[index] = introdetect.AudioSample{StartTicks: start, EndTicks: start + introdetect.TicksPerSecond/100, Fingerprint: uint32(index)}
	}
	sources := make([]AnalysisSource, 7)
	for index := range sources {
		sources[index] = AnalysisSource{ItemID: fmt.Sprintf("analysis-cache-lru-%d", index), SourceRevision: strings.Repeat("b2", 32), DurationTicks: 120 * introdetect.TicksPerSecond}
		if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type) VALUES($1,$2,$1,$1,'Video')`, sources[index].ItemID, collection.ID); err != nil {
			t.Fatal(err)
		}
		if index == 6 {
			if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
				_, found, err := loadAnalysisFeatureCache(tx, sources[0], fingerprint)
				if err == nil && !found {
					t.Fatal("cache budget evicted a fitting entry too early")
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
			return putAnalysisFeatureCache(tx, sources[index], fingerprint, profile, value)
		}); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var size int64
	var oldest, touched bool
	if err := pool.QueryRow(ctx, `SELECT count(*),COALESCE(sum(bytes),0),
		bool_or(item_id='analysis-cache-lru-1'),bool_or(item_id='analysis-cache-lru-0') FROM analysis_feature_cache`).
		Scan(&count, &size, &oldest, &touched); err != nil || count != 6 || size > profile.FeatureCacheMaxBytes || oldest || !touched {
		t.Fatalf("LRU byte budget evicted the wrong source: count%d bytes%d oldest%v touched%v: %v", count, size, oldest, touched, err)
	}
	minimal := AnalysisFeatures{ContentSHA256: strings.Repeat("a1", 32), AlgorithmProfile: "p"}
	payload, err := EncodeAnalysisFeatures(minimal, sources[0].DurationTicks)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		if _, err := tx.Exec(`DELETE FROM analysis_feature_cache`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO analysis_feature_cache
			(cache_key,item_id,source_revision,profile_fingerprint,content_sha256,algorithm_profile,duration_ticks,payload,bytes,last_used_at)
			SELECT lpad(to_hex(n),64,'0'),$1,lpad(to_hex(n),64,'0'),$2,$3,'p',$4,$5,$6,
			clock_timestamp()-n*interval '1 second' FROM generate_series(1,8193) n`, sources[0].ItemID, fingerprint,
			minimal.ContentSHA256, sources[0].DurationTicks, payload, len(payload)); err != nil {
			return err
		}
		return trimAnalysisFeatureCache(tx, profile.FeatureCacheMaxBytes)
	}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*),COALESCE(sum(bytes),0),
		bool_or(source_revision=lpad(to_hex(8193),64,'0')),bool_or(source_revision=lpad(to_hex(1),64,'0')) FROM analysis_feature_cache`).
		Scan(&count, &size, &oldest, &touched); err != nil || count != analysisFeatureCacheMaxRows || size > profile.FeatureCacheMaxBytes || oldest || !touched {
		t.Fatalf("cache hard row limit or deterministic eviction failed: count%d bytes%d: %v", count, size, err)
	}
}
