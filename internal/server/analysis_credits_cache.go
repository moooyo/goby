package server

import (
	"crypto/sha256"
	"encoding/json"
	"math"
	"sync"

	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

const (
	creditsFingerprintCacheEntries = 128
	creditsFingerprintCacheBytes   = 2 << 20
)

// This cache holds only detached raw tail words. Fixed-size keys include the run
// and freshly read content, never a child capability or a descriptor. FIFO
// eviction bounds both map overhead and the combined fingerprint/key storage.
type creditsFingerprintCache struct {
	mu      sync.Mutex
	entries map[[sha256.Size]byte][]uint32
	order   [creditsFingerprintCacheEntries][sha256.Size]byte
	oldest  int
	count   int
	bytes   int
}

func (c *creditsFingerprintCache) get(key [sha256.Size]byte) []uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]uint32(nil), c.entries[key]...)
}

func (c *creditsFingerprintCache) put(key [sha256.Size]byte, raw []uint32) {
	if len(raw) == 0 || len(raw) > introskipper.MaxFingerprintPoints {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; exists {
		return
	}
	size := sha256.Size + len(raw)*4
	for c.count == creditsFingerprintCacheEntries || c.bytes+size > creditsFingerprintCacheBytes {
		oldest := c.order[c.oldest]
		c.bytes -= sha256.Size + len(c.entries[oldest])*4
		delete(c.entries, oldest)
		c.oldest = (c.oldest + 1) % creditsFingerprintCacheEntries
		c.count--
	}
	if c.entries == nil {
		c.entries = make(map[[sha256.Size]byte][]uint32)
	}
	c.entries[key] = append([]uint32(nil), raw...)
	c.order[(c.oldest+c.count)%creditsFingerprintCacheEntries] = key
	c.count++
	c.bytes += size
}

func creditsFingerprintKey(work library.AnalysisWork, source library.AnalysisSource, hash, audioProfile string, index int) ([sha256.Size]byte, bool) {
	// Profiles and identifiers normally have much tighter admission limits. Keep
	// this optional cache bounded even if an internal caller supplies other data.
	for _, value := range []string{work.RunID, source.SourceRevision, hash, audioProfile, work.Execution.IntroProfile, work.Execution.FingerprintSHA256} {
		if len(value) == 0 || len(value) > 1024 {
			return [sha256.Size]byte{}, false
		}
	}
	key := struct {
		Run, Revision, Content, AudioProfile, ExecutionProfile, Tool string
		AudioIndex                                                   int
		Duration                                                     int64
		Start, End                                                   uint64
		Options                                                      introskipper.Options
	}{work.RunID, source.SourceRevision, hash, audioProfile, work.Execution.IntroProfile, work.Execution.FingerprintSHA256,
		index, source.DurationTicks, math.Float64bits(introskipper.CreditsFingerprintStartSeconds(source.DurationTicks)),
		math.Float64bits(float64(source.DurationTicks) / float64(media.TicksPerSecond)), work.Execution.IntroSkipperOptions}
	encoded, err := json.Marshal(key)
	if err != nil {
		return [sha256.Size]byte{}, false
	}
	return sha256.Sum256(encoded), true
}

type creditsFingerprintRead struct {
	hash      string
	raw       []uint32
	key       [sha256.Size]byte
	cacheable bool
}
