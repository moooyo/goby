package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"sync"
	"testing"
)

type videoSeekToolCountReader struct {
	reader io.Reader
	bytes  int
	reads  int
}

func (reader *videoSeekToolCountReader) Read(buffer []byte) (int, error) {
	n, err := reader.reader.Read(buffer)
	reader.bytes += n
	reader.reads++
	return n, err
}

func videoSeekToolTestReady(cache *videoSeekToolHashCache) int {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	count := 0
	for number := range cache.slots {
		if cache.slots[number].ready {
			count++
		}
	}
	return count
}

func videoSeekToolTestRead(cache *videoSeekToolHashCache, key string, size int64, input io.Reader, maximum int64, publish bool) (hash.Hash, bool, error) {
	attempt := cache.acquire(key, size)
	defer attempt.close()
	hit := attempt != nil && attempt.hit
	digest, err := videoSeekToolDigest(context.Background(), input, maximum, attempt)
	if err == nil && attempt != nil {
		attempt.publish = publish
	}
	return digest, hit, err
}

func TestVideoSeekToolCacheExactPrefixStateAndFullReads(t *testing.T) {
	const capacity = 2*videoSeekToolReadBytes + 16
	for _, size := range []int{0, 1, 55, 56, 63, 64, 65, videoSeekToolReadBytes - 1,
		videoSeekToolReadBytes, videoSeekToolReadBytes + 1, capacity, capacity + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			cache := &videoSeekToolHashCache{bufferBytes: capacity}
			data := make([]byte, size)
			for number := range data {
				data[number] = byte(number*37 + 11)
			}
			suffix := []byte("\x00video-seek-tool-v1\x00/controlled/tool\x00current-stamp\x00current-loader\x00version-output\n")
			message := append(append([]byte(nil), data...), suffix...)
			want := sha256.Sum256(message)
			for call := 0; call < 3; call++ {
				reader := &videoSeekToolCountReader{reader: bytes.NewReader(data)}
				attempt := cache.acquire("/controlled/tool", int64(len(data)))
				hit := attempt != nil && attempt.hit
				digest, err := videoSeekToolDigest(context.Background(), reader, int64(capacity+32), attempt)
				if err != nil {
					attempt.close()
					t.Fatal(err)
				}
				_, _ = digest.Write(suffix)
				if !bytes.Equal(digest.Sum(nil), want[:]) {
					attempt.close()
					t.Fatal("cached identity differs from fresh v1 digest")
				}
				if reader.bytes != len(data) || reader.reads == 0 {
					attempt.close()
					t.Fatal("identity skipped fresh input reads")
				}
				if call > 0 && size > 0 && size <= capacity && !hit {
					attempt.close()
					t.Fatal("verified prefix was not reusable")
				}
				if attempt != nil {
					attempt.publish = true
				}
				attempt.close()
			}
		})
	}
}

func TestVideoSeekToolCacheChangedBytesDoNotTrustIdentityHints(t *testing.T) {
	data := bytes.Repeat([]byte("content checked independently of metadata"), 5000)
	for _, position := range []int{0, videoSeekToolReadBytes - 1, videoSeekToolReadBytes, len(data) - 1} {
		t.Run(fmt.Sprint(position), func(t *testing.T) {
			cache := &videoSeekToolHashCache{bufferBytes: len(data) + 1}
			if _, _, err := videoSeekToolTestRead(cache, "unchanged-path-and-stat-hint", int64(len(data)), bytes.NewReader(data), int64(len(data)), true); err != nil {
				t.Fatal(err)
			}
			changed := append([]byte(nil), data...)
			changed[position] ^= 0x80
			reader := &videoSeekToolCountReader{reader: bytes.NewReader(changed)}
			digest, hit, err := videoSeekToolTestRead(cache, "unchanged-path-and-stat-hint", int64(len(changed)), reader, int64(len(changed)), true)
			want := sha256.Sum256(changed)
			if err != nil || !hit || reader.bytes != len(changed) || !bytes.Equal(digest.Sum(nil), want[:]) {
				t.Fatalf("same-size changed bytes reused stale content: hit=%v bytes=%d err=%v", hit, reader.bytes, err)
			}
		})
	}
}

type videoSeekToolDataErrorReader struct {
	data []byte
	err  error
	read bool
}

func (reader *videoSeekToolDataErrorReader) Read(buffer []byte) (int, error) {
	if reader.read {
		return 0, io.EOF
	}
	reader.read = true
	return copy(buffer, reader.data), reader.err
}

type videoSeekToolCancelReader struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (reader *videoSeekToolCancelReader) Read(buffer []byte) (int, error) {
	n, err := reader.reader.Read(buffer)
	reader.cancel()
	return n, err
}

type videoSeekToolCancelOnErrorReader struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (reader *videoSeekToolCancelOnErrorReader) Read(buffer []byte) (int, error) {
	n, err := reader.reader.Read(buffer)
	if err != nil && err != io.EOF {
		reader.cancel()
	}
	return n, err
}

func TestVideoSeekToolCachePreservesReadErrorsCancellationAndBudget(t *testing.T) {
	data := bytes.Repeat([]byte("a"), videoSeekToolReadBytes+19)
	failure := errors.New("controlled fresh read failure")
	cases := []struct {
		name         string
		input        func(context.CancelFunc) io.Reader
		maximum      int64
		beforeCancel bool
		want         error
		budget       bool
	}{
		{"partial-read-error", func(_ context.CancelFunc) io.Reader {
			return &videoSeekToolDataErrorReader{data: data[:11], err: failure}
		}, int64(len(data)), false, failure, false},
		{"read-error-wins-over-concurrent-cancellation", func(cancel context.CancelFunc) io.Reader {
			return &videoSeekToolCancelOnErrorReader{reader: io.MultiReader(bytes.NewReader(data[:videoSeekToolReadBytes]),
				&videoSeekToolDataErrorReader{data: []byte("different"), err: failure}), cancel: cancel}
		}, int64(len(data)), false, failure, false},
		{"mid-read-cancellation", func(cancel context.CancelFunc) io.Reader {
			return &videoSeekToolCancelReader{reader: bytes.NewReader(data), cancel: cancel}
		}, int64(len(data)), false, context.Canceled, false},
		{"already-canceled", func(_ context.CancelFunc) io.Reader { return bytes.NewReader(data) }, int64(len(data)), true, context.Canceled, false},
		{"byte-budget", func(_ context.CancelFunc) io.Reader { return bytes.NewReader(data) }, int64(len(data) - 1), false, nil, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cache := &videoSeekToolHashCache{bufferBytes: len(data)}
			if _, _, err := videoSeekToolTestRead(cache, "tool", int64(len(data)), bytes.NewReader(data), int64(len(data)), true); err != nil {
				t.Fatal(err)
			}
			attempt := cache.acquire("tool", int64(len(data)))
			if attempt == nil || !attempt.hit {
				t.Fatal("test did not acquire a warm slot")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.beforeCancel {
				cancel()
			}
			_, err := videoSeekToolDigest(ctx, test.input(cancel), test.maximum, attempt)
			attempt.close()
			if test.budget {
				if err == nil || err.Error() != "video seek executable exceeds its byte budget" {
					t.Fatalf("wrong byte-bound error: %v", err)
				}
			} else if !errors.Is(err, test.want) {
				t.Fatalf("fresh failure was hidden: %v", err)
			}
			if videoSeekToolTestReady(cache) != 0 {
				t.Fatal("a failed call published a cache entry")
			}
		})
	}
}

func TestVideoSeekToolCachePreservesEOFWithDataAndShortRead(t *testing.T) {
	full := []byte("the same advertised executable size")
	for _, length := range []int{len(full), len(full) - 7} {
		cache := &videoSeekToolHashCache{bufferBytes: 128}
		if _, _, err := videoSeekToolTestRead(cache, "tool", int64(len(full)), bytes.NewReader(full), 128, true); err != nil {
			t.Fatal(err)
		}
		data := full[:length]
		digest, hit, err := videoSeekToolTestRead(cache, "tool", int64(len(full)), &videoSeekToolDataErrorReader{data: data, err: io.EOF}, 128, true)
		want := sha256.Sum256(data)
		if err != nil || !hit || !bytes.Equal(digest.Sum(nil), want[:]) {
			t.Fatalf("EOF payload changed: %v", err)
		}
		if length != len(full) && videoSeekToolTestReady(cache) != 0 {
			t.Fatal("a short read published the advertised full-size prefix")
		}
	}
}

type videoSeekToolMarshalFailure struct{ hash.Hash }

func (videoSeekToolMarshalFailure) MarshalBinary() ([]byte, error) {
	return nil, errors.New("controlled state failure")
}

type videoSeekToolOversizedState struct{ hash.Hash }

func (videoSeekToolOversizedState) MarshalBinary() ([]byte, error) {
	return make([]byte, videoSeekToolCacheStateBytes+1), nil
}

func TestVideoSeekToolCacheStateFailuresFallBack(t *testing.T) {
	for _, digest := range []hash.Hash{struct{ hash.Hash }{sha256.New()}, videoSeekToolMarshalFailure{sha256.New()}, videoSeekToolOversizedState{sha256.New()}} {
		_, _ = digest.Write([]byte("content still hashable"))
		before := append([]byte(nil), digest.Sum(nil)...)
		if videoSeekToolHashState(digest) != nil || !bytes.Equal(before, digest.Sum(nil)) {
			t.Fatal("unavailable cache state changed the hash")
		}
	}
	data := []byte("fresh bytes remain authoritative")
	for _, corrupt := range []bool{false, true} {
		cache := &videoSeekToolHashCache{bufferBytes: 128}
		if _, _, err := videoSeekToolTestRead(cache, "tool", int64(len(data)), bytes.NewReader(data), 128, true); err != nil {
			t.Fatal(err)
		}
		cache.mu.Lock()
		for number := range cache.slots {
			slot := &cache.slots[number]
			if !slot.ready {
				continue
			}
			state := []byte("invalid serialized state")
			if corrupt {
				other := sha256.New()
				_, _ = other.Write(bytes.Repeat([]byte("x"), len(data)))
				state = videoSeekToolHashState(other)
			}
			copy(slot.state[:], state)
			slot.stateSize = len(state)
		}
		cache.mu.Unlock()
		digest, hit, err := videoSeekToolTestRead(cache, "tool", int64(len(data)), bytes.NewReader(data), 128, true)
		want := sha256.Sum256(data)
		if err != nil || !hit || !bytes.Equal(digest.Sum(nil), want[:]) {
			t.Fatalf("bad cache state escaped fallback: %v", err)
		}
	}
}

func TestVideoSeekToolCacheProductionCapacityBoundary(t *testing.T) {
	cache := &videoSeekToolHashCache{bufferBytes: videoSeekToolCacheBufferBytes}
	data := bytes.Repeat([]byte{0xa5}, videoSeekToolCacheBufferBytes+1)
	suffix := []byte("\x00v1 suffix after a production-size binary\x00")
	for call := 0; call < 2; call++ {
		reader := &videoSeekToolCountReader{reader: bytes.NewReader(data[:videoSeekToolCacheBufferBytes])}
		attempt := cache.acquire("large", int64(videoSeekToolCacheBufferBytes))
		if attempt == nil || call > 0 && !attempt.hit {
			t.Fatal("production-sized prefix was not reserved or reused")
		}
		digest, err := videoSeekToolDigest(context.Background(), reader, int64(len(data)), attempt)
		if err != nil {
			attempt.close()
			t.Fatal(err)
		}
		_, _ = digest.Write(suffix)
		fresh := sha256.New()
		_, _ = fresh.Write(data[:videoSeekToolCacheBufferBytes])
		_, _ = fresh.Write(suffix)
		if reader.bytes != videoSeekToolCacheBufferBytes || !bytes.Equal(digest.Sum(nil), fresh.Sum(nil)) {
			attempt.close()
			t.Fatal("production-sized hit changed content or skipped bytes")
		}
		attempt.publish = true
		attempt.close()
	}
	first, second := cache.acquire("large", int64(videoSeekToolCacheBufferBytes)), cache.acquire("other-large", int64(videoSeekToolCacheBufferBytes))
	if first == nil || second == nil {
		t.Fatal("two bounded production-sized leases were not available")
	}
	if cache.acquire("third-large", int64(videoSeekToolCacheBufferBytes)) != nil {
		t.Fatal("a third in-flight buffer was allocated")
	}
	if cap(first.slot.data)+cap(second.slot.data) != videoSeekToolCacheSlots*videoSeekToolCacheBufferBytes {
		t.Fatal("wrong production raw-byte bound")
	}
	first.close()
	second.close()
	if cache.acquire("over-capacity", int64(len(data))) != nil {
		t.Fatal("over-capacity input reserved a buffer")
	}
	digest, err := videoSeekToolDigest(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	want := sha256.Sum256(data)
	if err != nil || !bytes.Equal(digest.Sum(nil), want[:]) {
		t.Fatalf("large fallback did not keep the full algorithm: %v", err)
	}
}

func TestVideoSeekToolCacheFailedCallDoesNotPublish(t *testing.T) {
	cache := &videoSeekToolHashCache{bufferBytes: 128}
	data := []byte("complete read followed by a failed version or post-stat")
	if _, _, err := videoSeekToolTestRead(cache, "tool", int64(len(data)), bytes.NewReader(data), 128, false); err != nil {
		t.Fatal(err)
	}
	if videoSeekToolTestReady(cache) != 0 {
		t.Fatal("unverified bytes were published")
	}
}

func TestVideoSeekToolCacheReservationsBoundMemoryAndNeverWaitForIO(t *testing.T) {
	cache := &videoSeekToolHashCache{bufferBytes: 4096}
	first, second := cache.acquire("first", 1), cache.acquire("second", 1)
	if first == nil || second == nil || first.slot == second.slot {
		t.Fatal("independent bounded slots unavailable")
	}
	defer first.close()
	defer second.close()
	if !cache.mu.TryLock() {
		t.Fatal("a lease holds the cache mutex across caller work")
	}
	cache.mu.Unlock()
	if cache.acquire("third", 1) != nil || cache.acquire("first", 1) != nil {
		t.Fatal("busy slots allocated or waited for replacement storage")
	}
	cache.mu.Lock()
	contended := cache.acquire("lock-contention", 1)
	cache.mu.Unlock()
	if contended != nil {
		contended.close()
		t.Fatal("lock contention did not use full-hash fallback")
	}
	pointers := [videoSeekToolCacheSlots]*byte{&first.slot.data[0], &second.slot.data[0]}
	first.close()
	second.close()
	for number := 0; number < 30; number++ {
		attempt := cache.acquire(fmt.Sprint(number), 1)
		if attempt == nil {
			t.Fatal("idle slot unavailable")
		}
		if &attempt.slot.data[0] != pointers[0] && &attempt.slot.data[0] != pointers[1] {
			t.Fatal("eviction allocated a replacement byte buffer")
		}
		attempt.close()
	}
	total := 0
	for number := range cache.slots {
		total += cap(cache.slots[number].data)
	}
	if total != videoSeekToolCacheSlots*cache.bufferBytes {
		t.Fatalf("unbounded cache buffers: %d", total)
	}
	if cache.acquire("oversized", int64(cache.bufferBytes+1)) != nil {
		t.Fatal("over-capacity tool entered the cache")
	}
}

func TestVideoSeekToolCacheConcurrentReads(t *testing.T) {
	cache := &videoSeekToolHashCache{bufferBytes: 8192}
	var workers sync.WaitGroup
	failures := make(chan error, 12)
	for worker := 0; worker < 12; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for iteration := 0; iteration < 40; iteration++ {
				data := bytes.Repeat([]byte{byte(worker), byte(iteration)}, 2048)
				reader := &videoSeekToolCountReader{reader: bytes.NewReader(data)}
				digest, _, err := videoSeekToolTestRead(cache, fmt.Sprintf("tool-%d", worker%3), int64(len(data)), reader, 8192, iteration%5 != 0)
				want := sha256.Sum256(data)
				if err != nil || reader.bytes != len(data) || !bytes.Equal(digest.Sum(nil), want[:]) {
					failures <- fmt.Errorf("concurrent fresh read changed: worker=%d iteration=%d err=%v", worker, iteration, err)
					return
				}
			}
		}(worker)
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	total := 0
	for number := range cache.slots {
		if cache.slots[number].busy {
			t.Fatal("completed call retained a slot")
		}
		total += cap(cache.slots[number].data)
	}
	if total > videoSeekToolCacheSlots*cache.bufferBytes {
		t.Fatal("concurrent builders exceeded the fixed raw-byte budget")
	}
}
