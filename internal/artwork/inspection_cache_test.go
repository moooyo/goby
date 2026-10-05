package artwork_test

import (
	"bytes"
	"context"
	"errors"
	"image/gif"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/artwork"
)

func TestInspectionCacheReadsEverySourceAndReturnsIndependentInfo(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(12, 8))
	ctx := context.Background()
	want, err := artwork.InspectJoined(ctx, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var cache artwork.InspectionCache
	first, err := cache.InspectJoined(ctx, bytes.NewReader(data))
	if err != nil || first != want {
		t.Fatalf("initial cached inspection differs: %+v %v", first, err)
	}
	first.Tag, first.MIMEType, first.Width = "caller replacement", "unknown", 1
	input := bytes.NewReader(data)
	reader := &countingReader{source: input}
	current, err := cache.InspectJoined(ctx, reader)
	if err != nil || current != want || input.Len() != 0 || reader.reads.Load() == 0 {
		t.Fatalf("cached inspection skipped the fresh input or retained caller mutation: %+v %v remaining=%d reads=%d", current, err, input.Len(), reader.reads.Load())
	}
	wantErr := errors.New("fresh source failed after its image bytes")
	if info, err := cache.InspectJoined(ctx, io.MultiReader(bytes.NewReader(data), errorReader{err: wantErr})); !errors.Is(err, wantErr) || info != (artwork.Info{}) {
		t.Fatalf("cached digest hid a later source error: %+v %v", info, err)
	}
	changed := append(append([]byte(nil), data...), 'x')
	wantChanged, err := artwork.InspectJoined(ctx, bytes.NewReader(changed))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := cache.InspectJoined(ctx, bytes.NewReader(changed)); err != nil || info != wantChanged || info.Tag == want.Tag {
		t.Fatalf("complete digest did not include trailing source bytes: %+v %v", info, err)
	}
}

func TestInspectionCachePreservesCancellationOnHitsAndMisses(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(12, 8))
	for _, warm := range []bool{false, true} {
		for _, stage := range []string{"before read", "during read", "reader failure"} {
			t.Run(stage+map[bool]string{false: "/miss", true: "/hit"}[warm], func(t *testing.T) {
				var cache artwork.InspectionCache
				if warm {
					if _, err := cache.InspectJoined(context.Background(), bytes.NewReader(data)); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				reader := &countingReader{source: bytes.NewReader(data)}
				var input io.Reader = reader
				if stage == "before read" {
					cancel()
				} else {
					if stage == "reader failure" {
						input = errorReader{err: errors.New("source failure after cancellation")}
					}
					input = &cancelReader{source: input, cancel: cancel}
				}
				if info, err := cache.InspectJoined(ctx, input); !errors.Is(err, context.Canceled) || info != (artwork.Info{}) {
					t.Fatalf("cache changed cancellation precedence or exposed metadata: %+v %v", info, err)
				}
				if stage == "before read" && reader.reads.Load() != 0 {
					t.Fatal("pre-canceled cache operation read its source")
				}
			})
		}
	}
}

func TestInspectionCacheCanceledHitRetainsSharedSlotsAndReader(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(12, 8))
	var cache, queuedCache artwork.InspectionCache
	for _, target := range []*artwork.InspectionCache{&cache, &queuedCache} {
		if _, err := target.InspectJoined(context.Background(), bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() {
		cancel()
		unblock()
		awaitArtworkDecodeSlots(t, data)
	})
	type outcome struct {
		info artwork.Info
		err  error
	}
	finished := make(chan outcome, 2)
	readers := []*joinedBlockingReadCloser{newJoinedBlockingReadCloser(data, release), newJoinedBlockingReadCloser(data, release)}
	for index, reader := range readers {
		go func() {
			var info artwork.Info
			var err error
			if index == 0 {
				info, err = cache.InspectJoined(ctx, reader)
			} else {
				info, err = artwork.InspectJoined(ctx, reader)
			}
			finished <- outcome{info, err}
		}()
	}
	for _, reader := range readers {
		select {
		case <-reader.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("cached and ordinary inspection did not occupy both shared slots")
		}
	}
	cancel()
	select {
	case result := <-finished:
		t.Fatalf("canceled inspection abandoned its blocked reader: %+v", result)
	case <-time.After(100 * time.Millisecond):
	}
	queuedCtx, cancelQueued := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelQueued()
	queuedReader := &countingReader{source: bytes.NewReader(data)}
	if info, err := queuedCache.InspectJoined(queuedCtx, queuedReader); !errors.Is(err, context.DeadlineExceeded) || info != (artwork.Info{}) || queuedReader.reads.Load() != 0 {
		t.Fatalf("cached hit bypassed the two retained decode slots: %+v %v reads=%d", info, err, queuedReader.reads.Load())
	}
	unblock()
	for range readers {
		select {
		case result := <-finished:
			if !errors.Is(result.err, context.Canceled) || result.info != (artwork.Info{}) {
				t.Errorf("canceled joined inspection returned metadata: %+v", result)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("inspection did not join its released reader")
		}
	}
	for _, reader := range readers {
		select {
		case <-reader.readExited:
		default:
			t.Error("inspection returned before the read completed")
		}
		if reader.closes.Load() != 0 {
			t.Error("inspection cache closed its caller-owned reader")
		}
	}
}

func TestInspectionCachePreservesImageAndGIFValidation(t *testing.T) {
	var cache artwork.InspectionCache
	ctx := context.Background()
	for _, format := range []string{"jpeg", "png", "gif"} {
		data := encodeImage(t, format, sampleImage(12, 8))
		want, err := artwork.InspectJoined(ctx, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if info, err := cache.InspectJoined(ctx, bytes.NewReader(data)); err != nil || info != want {
				t.Fatalf("cached %s metadata differs from complete validation: %+v %v", format, info, err)
			}
		}
	}
	validGIF := repeatedGIF(t, 2, 2, 2)
	if _, err := cache.InspectJoined(ctx, bytes.NewReader(validGIF)); err != nil {
		t.Fatal(err)
	}
	brokenGIF := validGIF[:len(validGIF)-3]
	if _, err := gif.Decode(bytes.NewReader(brokenGIF)); err != nil {
		t.Fatalf("damaged GIF must retain a valid first frame: %v", err)
	}
	for range 2 {
		if info, err := cache.InspectJoined(ctx, bytes.NewReader(brokenGIF)); !errors.Is(err, artwork.ErrInvalidImage) || info != (artwork.Info{}) {
			t.Fatalf("cache accepted a damaged later GIF frame: %+v %v", info, err)
		}
	}
	for _, data := range [][]byte{pngHeader(16385, 1), pngHeader(6000, 5000), repeatedGIF(t, 1, 1, 1001)} {
		if info, err := cache.InspectJoined(ctx, bytes.NewReader(data)); !errors.Is(err, artwork.ErrLimitExceeded) || info != (artwork.Info{}) {
			t.Fatalf("cache bypassed an image/GIF resource limit: %+v %v", info, err)
		}
	}
	data := encodeImage(t, "png", sampleImage(4, 4))
	if info, err := cache.InspectJoined(ctx, io.MultiReader(bytes.NewReader(data), io.LimitReader(zeroReader{}, 20<<20))); !errors.Is(err, artwork.ErrLimitExceeded) || info != (artwork.Info{}) {
		t.Fatalf("cache bypassed the bounded full-source read: %+v %v", info, err)
	}
	if info, err := cache.InspectJoined(ctx, nil); !errors.Is(err, artwork.ErrInvalidImage) || info != (artwork.Info{}) {
		t.Fatalf("cache changed nil-reader validation: %+v %v", info, err)
	}
}
