package server

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func TestCreditsFingerprintCacheBoundsAndDetachedOwnership(t *testing.T) {
	for _, points := range []int{1, introskipper.MaxFingerprintPoints} {
		t.Run(fmt.Sprintf("points-%d", points), func(t *testing.T) {
			var cache creditsFingerprintCache
			first := sha256.Sum256([]byte("first"))
			raw := make([]uint32, points)
			raw[0] = 27
			cache.put(first, raw)
			raw[0] = 99
			got := cache.get(first)
			if len(got) != points || got[0] != 27 {
				t.Fatal("cache retained its caller's mutable fingerprint storage")
			}
			got[0] = 88
			if cache.get(first)[0] != 27 {
				t.Fatal("a cache reader mutated an immutable entry")
			}
			for index := 0; index < creditsFingerprintCacheEntries*3; index++ {
				cache.put(sha256.Sum256([]byte(fmt.Sprint(index))), raw)
				if cache.count > creditsFingerprintCacheEntries || cache.bytes > creditsFingerprintCacheBytes || len(cache.entries) != cache.count {
					t.Fatal("cache exceeded the entry or combined key and fingerprint byte bound")
				}
			}
			if len(cache.get(first)) != 0 {
				t.Fatal("FIFO did not evict an inactive entry")
			}
			accounted := 0
			for _, words := range cache.entries {
				accounted += sha256.Size + len(words)*4
			}
			if accounted != cache.bytes {
				t.Fatal("eviction lost key or payload byte accounting")
			}
		})
	}
	var cache creditsFingerprintCache
	key := sha256.Sum256([]byte("unavailable"))
	cache.put(key, nil)
	cache.put(key, []uint32{})
	cache.put(key, make([]uint32, introskipper.MaxFingerprintPoints+1))
	if cache.count != 0 || len(cache.get(key)) != 0 {
		t.Fatal("empty or oversized output entered the successful fingerprint cache")
	}
}

func TestCreditsFingerprintCacheConcurrentOwnership(t *testing.T) {
	var cache creditsFingerprintCache
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for index := 0; index < 100; index++ {
				key := sha256.Sum256([]byte(fmt.Sprintf("%d/%d", worker, index)))
				raw := []uint32{uint32(worker), uint32(index)}
				cache.put(key, raw)
				if got := cache.get(key); len(got) != 0 && !reflect.DeepEqual(got, raw) {
					t.Error("concurrent insertion or eviction changed fingerprint ownership")
				}
			}
		}(worker)
	}
	workers.Wait()
	if cache.count > creditsFingerprintCacheEntries || cache.bytes > creditsFingerprintCacheBytes {
		t.Fatal("concurrent insertion exceeded cache bounds")
	}
}

func TestCreditsFingerprintKeyInvalidatesEveryExtractionInput(t *testing.T) {
	work := library.AnalysisWork{RunID: "run", Execution: library.AnalysisExecutionProfile{
		FingerprintSHA256: strings.Repeat("a", 64), IntroProfile: "execution", IntroSkipperOptions: introskipper.DefaultOptions()}}
	source := library.AnalysisSource{SourceRevision: "revision", DurationTicks: 475*media.TicksPerSecond + 1}
	content, profile := strings.Repeat("b", 64), "audio-profile"
	initial, ok := creditsFingerprintKey(work, source, content, profile, 2)
	if !ok {
		t.Fatal("valid extraction identity was not cacheable")
	}
	for _, change := range []struct {
		name          string
		work          func(*library.AnalysisWork)
		source        func(*library.AnalysisSource)
		hash, profile string
		index         int
	}{
		{name: "run", work: func(w *library.AnalysisWork) { w.RunID = "next-run" }},
		{name: "revision", source: func(s *library.AnalysisSource) { s.SourceRevision = "next-revision" }},
		{name: "full-content", hash: strings.Repeat("c", 64)},
		{name: "audio-stream", index: 3},
		{name: "exact-window", source: func(s *library.AnalysisSource) { s.DurationTicks++ }},
		{name: "options", work: func(w *library.AnalysisWork) { w.Execution.IntroSkipperOptions.MaximumTimeSkip += 0.5 }},
		{name: "execution-profile", work: func(w *library.AnalysisWork) { w.Execution.IntroProfile += "-changed" }},
		{name: "audio-profile", profile: profile + "-changed"},
		{name: "tool", work: func(w *library.AnalysisWork) { w.Execution.FingerprintSHA256 = strings.Repeat("d", 64) }},
	} {
		t.Run(change.name, func(t *testing.T) {
			changedWork, changedSource, changedHash, changedProfile, index := work, source, content, profile, 2
			if change.work != nil {
				change.work(&changedWork)
			}
			if change.source != nil {
				change.source(&changedSource)
			}
			if change.hash != "" {
				changedHash = change.hash
			}
			if change.profile != "" {
				changedProfile = change.profile
			}
			if change.index != 0 {
				index = change.index
			}
			key, ok := creditsFingerprintKey(changedWork, changedSource, changedHash, changedProfile, index)
			if !ok || key == initial {
				t.Fatal("changed extraction inputs reused a raw tail fingerprint")
			}
		})
	}
	work.RunID = strings.Repeat("x", 1025)
	if _, ok := creditsFingerprintKey(work, source, content, profile, 2); ok {
		t.Fatal("unbounded key input entered the optional cache")
	}
}
