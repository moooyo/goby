package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding"
	"fmt"
	"hash"
	"io"
	"sync"
)

const (
	videoSeekToolCacheSlots = 2
	// Reserve space in each 32-MiB allowance for state, leases and metadata.
	// A slot's byte buffer is allocated once and is never replaced or grown.
	videoSeekToolCacheBufferBytes = (32 << 20) - (8 << 10)
	videoSeekToolCacheStateBytes  = 256
	videoSeekToolReadBytes        = 64 << 10
)

// The cache saves SHA work, not reads. Every attempted reuse compares all fresh
// bytes, and the caller still runs version and all final identity checks.
var videoSeekToolHashes = videoSeekToolHashCache{bufferBytes: videoSeekToolCacheBufferBytes}

type videoSeekToolHashSlot struct {
	data      []byte
	key       [sha256.Size]byte
	size      int64
	state     [videoSeekToolCacheStateBytes]byte
	stateSize int
	sum       [sha256.Size]byte
	ready     bool
	busy      bool
}

type videoSeekToolHashCache struct {
	mu          sync.Mutex
	bufferBytes int
	next        int
	slots       [videoSeekToolCacheSlots]videoSeekToolHashSlot
}

type videoSeekToolHashAttempt struct {
	cache   *videoSeekToolHashCache
	slot    *videoSeekToolHashSlot
	size    int64
	hit     bool
	state   []byte
	sum     [sha256.Size]byte
	publish bool
	closed  bool
}

// Contention and exhausted slots fall back immediately. A reserved slot stays
// exclusively owned through version and post-stat checks without holding mu.
// No active or evicted-but-held buffer can be replaced by a third allocation.
func (cache *videoSeekToolHashCache) acquire(path string, size int64) *videoSeekToolHashAttempt {
	if cache == nil || cache.bufferBytes <= 0 || cache.bufferBytes > videoSeekToolCacheBufferBytes ||
		size <= 0 || size > int64(cache.bufferBytes) || !cache.mu.TryLock() {
		return nil
	}
	key := sha256.Sum256([]byte(path))
	selected, hit := -1, false
	for number := range cache.slots {
		slot := &cache.slots[number]
		if slot.key == key && slot.busy {
			cache.mu.Unlock()
			return nil
		}
		if !slot.busy && slot.ready && slot.key == key && slot.size == size &&
			len(slot.data) == cache.bufferBytes && slot.stateSize > 0 && slot.stateSize <= len(slot.state) {
			selected, hit = number, true
			break
		}
	}
	if selected < 0 {
		for number := range cache.slots {
			if !cache.slots[number].busy && !cache.slots[number].ready {
				selected = number
				break
			}
		}
	}
	if selected < 0 {
		for offset := range cache.slots {
			number := (cache.next + offset) % len(cache.slots)
			if !cache.slots[number].busy {
				selected = number
				break
			}
		}
	}
	if selected < 0 {
		cache.mu.Unlock()
		return nil
	}
	slot := &cache.slots[selected]
	slot.busy, slot.ready, slot.key = true, false, key
	cache.next = (selected + 1) % len(cache.slots)
	cache.mu.Unlock()
	if slot.data == nil {
		slot.data = make([]byte, cache.bufferBytes)
	}
	return &videoSeekToolHashAttempt{cache: cache, slot: slot, size: size, hit: hit}
}

func (attempt *videoSeekToolHashAttempt) close() {
	if attempt == nil || attempt.closed {
		return
	}
	attempt.closed = true
	attempt.cache.mu.Lock()
	defer attempt.cache.mu.Unlock()
	slot := attempt.slot
	if attempt.publish && len(attempt.state) > 0 && len(attempt.state) <= len(slot.state) {
		copy(slot.state[:], attempt.state)
		slot.stateSize, slot.size, slot.sum = len(attempt.state), attempt.size, attempt.sum
		slot.ready = true
	}
	slot.busy = false
}

func videoSeekToolHashState(digest hash.Hash) []byte {
	marshaler, ok := digest.(encoding.BinaryMarshaler)
	if !ok {
		return nil
	}
	state, err := marshaler.MarshalBinary()
	if err != nil || len(state) == 0 || len(state) > videoSeekToolCacheStateBytes {
		return nil
	}
	return state
}

func videoSeekToolHashPrefix(ctx context.Context, digest hash.Hash, data []byte) error {
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		length := min(len(data), videoSeekToolReadBytes)
		_, _ = digest.Write(data[:length])
		data = data[length:]
	}
	return nil
}

// A cache hint is never content evidence: every read, including a final EOF
// read and any read error, still occurs. A mismatch hashes only the prefix that
// was freshly compared equal, then the current and remaining freshly read bytes.
func videoSeekToolDigest(ctx context.Context, reader io.Reader, maximum int64, attempt *videoSeekToolHashAttempt) (hash.Hash, error) {
	digest := sha256.New()
	buffer := make([]byte, videoSeekToolReadBytes)
	remaining, count := maximum+1, int64(0)
	matching := attempt != nil && attempt.hit
	cacheable := attempt != nil
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, readErr := reader.Read(buffer)
		remaining -= int64(n)
		if remaining <= 0 {
			return nil, fmt.Errorf("video seek executable exceeds its byte budget")
		}
		// Preserve read-error precedence if cancellation arrives during Read.
		// No partial digest or cache entry can be used after a non-EOF error.
		if readErr != nil && readErr != io.EOF {
			return nil, readErr
		}
		next := count + int64(n)
		if matching && (next > attempt.slot.size || !bytes.Equal(attempt.slot.data[int(count):int(next)], buffer[:n])) {
			if err := videoSeekToolHashPrefix(ctx, digest, attempt.slot.data[:int(count)]); err != nil {
				return nil, err
			}
			matching = false
		}
		if !matching {
			_, _ = digest.Write(buffer[:n])
		}
		if cacheable {
			if next > int64(len(attempt.slot.data)) {
				cacheable = false
			} else if !matching {
				copy(attempt.slot.data[int(count):int(next)], buffer[:n])
			}
		}
		count = next
		if readErr == io.EOF {
			break
		}
	}
	if matching {
		restored := sha256.New()
		decoder, ok := restored.(encoding.BinaryUnmarshaler)
		if count == attempt.slot.size && ok &&
			decoder.UnmarshalBinary(attempt.slot.state[:attempt.slot.stateSize]) == nil &&
			bytes.Equal(restored.Sum(nil), attempt.slot.sum[:]) {
			digest = restored
		} else if err := videoSeekToolHashPrefix(ctx, digest, attempt.slot.data[:int(count)]); err != nil {
			return nil, err
		}
	}
	if cacheable && count == attempt.size {
		attempt.state = videoSeekToolHashState(digest)
		copy(attempt.sum[:], digest.Sum(nil))
	}
	return digest, nil
}
