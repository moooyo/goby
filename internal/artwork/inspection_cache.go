package artwork

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sync"
)

const inspectionCacheEntries = 512

// InspectionCache retains only successful image metadata for concurrent callers.
// Its zero value is ready for use, and it must not be copied after first use.
// It retains no readers, descriptors or image pixels, and evicts in FIFO order.
type InspectionCache struct {
	mu      sync.Mutex
	entries map[[sha256.Size]byte]Info
	order   [inspectionCacheEntries][sha256.Size]byte
	next    int
}

// InspectJoined fully reads and hashes the current reader before reusing any
// metadata. Misses validate every image/GIF limit through the ordinary decoder.
// All reads retain the shared decode slot until they finish, even on cancellation.
// The reader remains caller-owned; a hit does not prove its filesystem identity.
func (cache *InspectionCache) InspectJoined(ctx context.Context, reader io.Reader) (Info, error) {
	var digest [sha256.Size]byte
	hit := false
	info, err := withSlotJoined(ctx, func() (Info, error) {
		data, err := readImageData(ctx, reader)
		if err != nil {
			return Info{}, err
		}
		digest = sha256.Sum256(data)
		if cached, exists := cache.lookup(digest); exists {
			hit = true
			return cached, nil
		}
		source, err := decodeImageData(ctx, data, hex.EncodeToString(digest[:]))
		return source.info, err
	})
	if err == nil && !hit {
		if err := cache.remember(ctx, digest, info); err != nil {
			return Info{}, err
		}
	}
	return info, err
}

func (cache *InspectionCache) lookup(digest [sha256.Size]byte) (Info, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	info, exists := cache.entries[digest]
	return info, exists
}

// The joined read and decoder have finished before this lock is acquired.
// Concurrent misses may validate independently; a duplicate never evicts entries.
func (cache *InspectionCache) remember(ctx context.Context, digest [sha256.Size]byte, info Info) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, exists := cache.entries[digest]; exists {
		return nil
	}
	if cache.entries == nil {
		cache.entries = make(map[[sha256.Size]byte]Info, inspectionCacheEntries)
	} else if len(cache.entries) == inspectionCacheEntries {
		delete(cache.entries, cache.order[cache.next])
	}
	cache.entries[digest] = info
	cache.order[cache.next] = digest
	cache.next = (cache.next + 1) % inspectionCacheEntries
	return nil
}
