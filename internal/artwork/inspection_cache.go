package artwork

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

const inspectionCacheEntries = 512

// InspectionCache retains only successful image metadata for one serial owner.
// Its zero value is ready for use. It must not be shared by concurrent callers
// or copied after first use. It retains no readers, descriptors or image pixels.
type InspectionCache struct {
	entries map[[sha256.Size]byte]Info
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
		if cached, exists := cache.entries[digest]; exists {
			hit = true
			return cached, nil
		}
		source, err := decodeImageData(ctx, data, hex.EncodeToString(digest[:]))
		return source.info, err
	})
	if err == nil && !hit {
		if cache.entries == nil {
			cache.entries = make(map[[sha256.Size]byte]Info)
		} else if len(cache.entries) == inspectionCacheEntries {
			clear(cache.entries)
		}
		cache.entries[digest] = info
	}
	return info, err
}
