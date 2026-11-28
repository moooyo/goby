package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func subtitleCacheFixture(t *testing.T) (*subtitleExtractionCache, string, os.FileInfo) {
	t.Helper()
	cache := newSubtitleExtractionCache()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cache.close(ctx); err != nil {
			t.Errorf("close subtitle cache: %v", err)
		}
	})
	tool := filepath.Join(t.TempDir(), "subtitle-tool")
	if err := os.WriteFile(tool, []byte("subtitle tool"), 0700); err != nil {
		t.Fatal(err)
	}
	return cache, tool, subtitleCacheToolInfo(t, tool)
}

func subtitleCacheToolInfo(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func waitSubtitleCache(t *testing.T, ready <-chan struct{}) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("subtitle work did not reach its expected state")
	}
}

func TestSubtitleExtractionCacheSharesDetachedWorkAndWitnesses(t *testing.T) {
	cache, path, tool := subtitleCacheFixture(t)
	key := subtitleExtractionKey{item: "item", source: "source", index: 5, codec: "ass", format: "ass", tool: path}
	start, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	raw := []byte("an immutable result")
	load := func(ctx context.Context) ([]byte, error) {
		if calls.Add(1) != 1 {
			return nil, errors.New("duplicate extraction")
		}
		close(start)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return raw, nil
		}
	}
	if _, err := cache.read(context.Background(), key, tool, subtitleExtractionFastWait, nil); !errors.Is(err, errSubtitlePreparing) || calls.Load() != 0 {
		t.Fatal("a cold cache observation admitted extraction")
	}
	request, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := cache.read(request, key, tool, subtitleExtractionFastWait, load); result <- err }()
	waitSubtitleCache(t, start)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled waiter: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled client held a request slot until extraction finished")
	}
	entry, err := cache.findOrQueue(key, tool, load)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.read(context.Background(), key, tool, subtitleExtractionFastWait, nil); !errors.Is(err, errSubtitlePreparing) {
		t.Fatal("a pending cache observation blocked on the worker or served partial data")
	}
	close(release)
	waitSubtitleCache(t, entry.ready)
	raw[0] = 'X'
	data, err := cache.read(context.Background(), key, tool, subtitleExtractionFastWait, load)
	if err != nil || string(data) != "an immutable result" || calls.Load() != 1 {
		t.Fatal("shared result did not survive the caller or was aliased to the worker buffer")
	}
	for _, other := range []subtitleExtractionKey{
		{item: key.item, source: key.source, index: 6, codec: key.codec, format: key.format, tool: key.tool},
		{item: key.item, source: key.source, index: key.index, codec: key.codec, format: "vtt", tool: key.tool},
		{item: key.item, source: "other", index: key.index, codec: key.codec, format: key.format, tool: key.tool},
	} {
		if _, err := cache.read(context.Background(), other, tool, subtitleExtractionFastWait, nil); !errors.Is(err, errSubtitlePreparing) {
			t.Fatal("an independently versioned track, format or source reused the cached result")
		}
	}
	if err := os.WriteFile(path, []byte("replacement subtitle tool"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.read(context.Background(), key, subtitleCacheToolInfo(t, path), subtitleExtractionFastWait, nil); !errors.Is(err, errSubtitlePreparing) {
		t.Fatal("a changed executable reused the cached result")
	}
}

func TestSubtitleExtractionCacheBoundsQueueAndJoinsRetirement(t *testing.T) {
	cache, path, tool := subtitleCacheFixture(t)
	key := subtitleExtractionKey{item: "matching", source: "source", tool: path}
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
	load := func(ctx context.Context) ([]byte, error) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-release
		return []byte("late result"), nil
	}
	running, err := cache.findOrQueue(key, tool, load)
	if err != nil {
		t.Fatal(err)
	}
	waitSubtitleCache(t, entered)
	ready := func(context.Context) ([]byte, error) { return []byte("unaffected"), nil }
	var entries []*subtitleExtractionEntry
	for index := 1; index <= subtitleExtractionQueueSize; index++ {
		queued := key
		queued.index = index
		if index == 1 {
			queued.item = "unrelated"
		}
		entry, err := cache.findOrQueue(queued, tool, ready)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	if _, err := cache.findOrQueue(subtitleExtractionKey{item: "overflow", tool: path}, tool, ready); !errors.Is(err, errSubtitleExtractionBusy) {
		t.Fatal("a saturated extraction queue accepted another unique job")
	}
	if duplicate, err := cache.findOrQueue(key, tool, load); err != nil || duplicate != running {
		t.Fatal("a saturated queue did not coalesce an admitted track")
	}
	join := cache.retireSource(key.item, key.source)
	waitSubtitleCache(t, canceled)
	if join != running {
		t.Fatal("publication did not join the exact active worker")
	}
	for _, entry := range entries[1:] {
		waitSubtitleCache(t, entry.ready)
	}
	select {
	case <-entries[0].ready:
		t.Fatal("unrelated queued work overtook the unfinished reader")
	case <-running.ready:
		t.Fatal("the source retired before actual worker return")
	default:
	}
	if _, err := cache.findOrQueue(key, tool, load); !errors.Is(err, library.ErrSourceChanged) {
		t.Fatal("an extraction reopened while the retired worker had not joined")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := cache.close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded close did not wait for real cleanup: %v", err)
	}
	if _, err := cache.findOrQueue(key, tool, load); !errors.Is(err, library.ErrUnavailable) {
		t.Fatal("closed cache admitted a worker")
	}
}

func TestSubtitleExtractionCacheFencesUncertainRetirement(t *testing.T) {
	cache := newSubtitleExtractionCache()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() { _ = cache.close(ctx) }()
	path := filepath.Join(t.TempDir(), "subtitle-tool")
	if err := os.WriteFile(path, []byte("subtitle tool"), 0700); err != nil {
		t.Fatal(err)
	}
	tool := subtitleCacheToolInfo(t, path)
	key := subtitleExtractionKey{item: "item", source: "source", tool: path}
	started := make(chan struct{})
	active, err := cache.findOrQueue(key, tool, func(ctx context.Context) ([]byte, error) {
		close(started)
		<-ctx.Done()
		return []byte("must not publish"), media.ErrProcessRetirementUnknown
	})
	if err != nil {
		t.Fatal(err)
	}
	waitSubtitleCache(t, started)
	join := cache.retireSource(key.item, key.source)
	if join != active || !errors.Is(join.waitRetirement(ctx), media.ErrProcessRetirementUnknown) {
		t.Fatal("source publication accepted an uncertain worker as retired")
	}
	if _, err := cache.read(ctx, subtitleExtractionKey{item: "next", tool: path}, tool, subtitleExtractionFastWait, func(context.Context) ([]byte, error) {
		return []byte("must not run"), nil
	}); !errors.Is(err, library.ErrUnavailable) {
		t.Fatal("an uncertain retirement allowed a replacement worker")
	}
	if !errors.Is(cache.close(ctx), media.ErrProcessRetirementUnknown) {
		t.Fatal("shutdown did not retain the worker retirement fault")
	}
}

func TestSubtitleExtractionCacheLimitsRetentionAndNeverCachesAuthorityErrors(t *testing.T) {
	cache, path, tool := subtitleCacheFixture(t)
	load := func(context.Context) ([]byte, error) {
		return bytes.Repeat([]byte("x"), media.MaxSubtitleExtractionBytes), nil
	}
	key := subtitleExtractionKey{item: "item", source: "source", tool: path}
	for index := 0; index <= subtitleExtractionBytes/media.MaxSubtitleExtractionBytes; index++ {
		key.index = index
		entry, err := cache.findOrQueue(key, tool, load)
		if err != nil {
			t.Fatal(err)
		}
		waitSubtitleCache(t, entry.ready)
	}
	key.index = 0
	if _, err := cache.read(context.Background(), key, tool, subtitleExtractionFastWait, nil); !errors.Is(err, errSubtitlePreparing) {
		t.Fatal("the oldest track was not evicted to respect the byte cap")
	}
	cache.mu.Lock()
	retained := cache.bytes
	cache.mu.Unlock()
	if retained != subtitleExtractionBytes {
		t.Fatalf("cache retained %d bytes, want %d", retained, subtitleExtractionBytes)
	}
	load = func(context.Context) ([]byte, error) { return []byte("ready"), nil }
	for index := 0; index <= subtitleExtractionEntries; index++ {
		entry, err := cache.findOrQueue(subtitleExtractionKey{item: fmt.Sprintf("item-%d", index), tool: path}, tool, load)
		if err != nil {
			t.Fatal(err)
		}
		waitSubtitleCache(t, entry.ready)
	}
	cache.mu.Lock()
	retainedEntries := len(cache.entries)
	cache.mu.Unlock()
	if retainedEntries != subtitleExtractionEntries {
		t.Fatalf("cache retained %d entries, want %d", retainedEntries, subtitleExtractionEntries)
	}
	key.item = "authorization-revoked"
	entry, err := cache.findOrQueue(key, tool, func(context.Context) ([]byte, error) { return nil, library.ErrForbidden })
	if err != nil {
		t.Fatal(err)
	}
	waitSubtitleCache(t, entry.ready)
	if _, err := cache.read(context.Background(), key, tool, subtitleExtractionFastWait, nil); !errors.Is(err, errSubtitlePreparing) {
		t.Fatal("a revoked caller contaminated shared state for a later authorized caller")
	}
}
