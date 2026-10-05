package artwork

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"sync"
	"testing"
	"time"
)

func inspectionCacheConcurrentPNG(t *testing.T) []byte {
	t.Helper()
	frame := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	frame.SetNRGBA(0, 0, color.NRGBA{R: 47, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, frame); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestInspectionCacheConcurrentHitsMissesAndCapacity(t *testing.T) {
	data := inspectionCacheConcurrentPNG(t)
	var cache InspectionCache
	if _, err := cache.InspectJoined(context.Background(), bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	const workers = 8
	var joined sync.WaitGroup
	failures := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		joined.Add(1)
		go func() {
			defer joined.Done()
			for step := 0; step < 160; step++ {
				index := worker*160 + step + 1
				payload := data
				if step%4 != 0 {
					payload = append(append([]byte(nil), data...), byte(index), byte(index>>8))
				}
				ctx := context.Background()
				canceled := step%17 == 0
				if canceled {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				info, err := cache.InspectJoined(ctx, bytes.NewReader(payload))
				if canceled {
					if !errors.Is(err, context.Canceled) || info != (Info{}) {
						failures <- fmt.Errorf("canceled concurrent inspection returned %+v: %v", info, err)
						return
					}
				} else if err != nil || info.Tag != contentHash(payload) || info.Width != 1 || info.Height != 1 || info.MIMEType != "image/png" {
					failures <- fmt.Errorf("concurrent inspection lost content metadata: %+v: %v", info, err)
					return
				}
			}
		}()
	}
	joined.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.entries) == 0 || len(cache.entries) > inspectionCacheEntries {
		t.Fatalf("concurrent successful population exceeded its bound: %d", len(cache.entries))
	}
	for digest, info := range cache.entries {
		if info.Tag != hex.EncodeToString(digest[:]) {
			t.Fatal("concurrent cache population confused content digests")
		}
	}
}

type inspectionCacheBlockedReader struct {
	reader  io.Reader
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (reader *inspectionCacheBlockedReader) Read(output []byte) (int, error) {
	reader.once.Do(func() {
		close(reader.entered)
		<-reader.release
	})
	return reader.reader.Read(output)
}

func TestInspectionCacheBlockedReaderDoesNotHoldMetadataLock(t *testing.T) {
	data := inspectionCacheConcurrentPNG(t)
	var cache InspectionCache
	want, err := cache.InspectJoined(context.Background(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	reader := &inspectionCacheBlockedReader{reader: bytes.NewReader(data), entered: make(chan struct{}), release: release}
	firstDone, secondDone := make(chan struct{}), make(chan struct{})
	var firstInfo, secondInfo Info
	var firstErr, secondErr error
	t.Cleanup(func() {
		cancel()
		unblock()
		for _, done := range []<-chan struct{}{firstDone, secondDone} {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("cache inspection did not join after its reader was released")
			}
		}
	})
	go func() {
		defer close(firstDone)
		firstInfo, firstErr = cache.InspectJoined(ctx, reader)
	}()
	go func() {
		defer close(secondDone)
		select {
		case <-reader.entered:
			secondInfo, secondErr = cache.InspectJoined(ctx, bytes.NewReader(data))
		case <-ctx.Done():
			secondErr = ctx.Err()
		}
	}()
	select {
	case <-reader.entered:
	case <-ctx.Done():
		t.Fatal("cached inspection did not start its fresh reader")
	}
	select {
	case <-secondDone:
		if secondErr != nil || secondInfo != want {
			t.Fatalf("independent cached inspection failed beside a blocked reader: %+v %v", secondInfo, secondErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a blocked reader held the cache metadata lock")
	}
	unblock()
	select {
	case <-firstDone:
		if firstErr != nil || firstInfo != want {
			t.Fatalf("released cached inspection changed its result: %+v %v", firstInfo, firstErr)
		}
	case <-ctx.Done():
		t.Fatal("released cached inspection did not finish")
	}
}

func TestInspectionCacheCanceledInsertionAndDuplicateDoNotEvict(t *testing.T) {
	data := inspectionCacheConcurrentPNG(t)
	var cache InspectionCache
	var firstDigest [sha256.Size]byte
	var firstInfo Info
	for index := 0; index < inspectionCacheEntries; index++ {
		payload := append(append([]byte(nil), data...), byte(index), byte(index>>8))
		info, err := cache.InspectJoined(context.Background(), bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			firstDigest, firstInfo = sha256.Sum256(payload), info
		}
	}
	// Reproduce another successful miss reaching insertion after its peer has
	// already installed the same digest in a full cache.
	if err := cache.remember(context.Background(), firstDigest, firstInfo); err != nil {
		t.Fatal(err)
	}
	cache.mu.Lock()
	full := len(cache.entries) == inspectionCacheEntries
	cache.mu.Unlock()
	if !full {
		t.Fatal("a duplicate successful insertion evicted the full population")
	}
	payload := append(append([]byte(nil), data...), 0xfa, 0xce)
	info, err := InspectJoined(context.Background(), bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, finished := make(chan struct{}), make(chan error, 1)
	cache.mu.Lock()
	go func() {
		close(started)
		finished <- cache.remember(ctx, digest, info)
	}()
	<-started
	cancel()
	cache.mu.Unlock()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("insertion ignored cancellation while its mutex was unavailable: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled insertion did not finish after lock release")
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, exists := cache.entries[digest]; exists || len(cache.entries) != inspectionCacheEntries || cache.entries[firstDigest] != firstInfo {
		t.Fatal("canceled insertion changed or evicted successful metadata")
	}
}
