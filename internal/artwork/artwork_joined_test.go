package artwork_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/artwork"
)

type artworkCall struct {
	name string
	run  func(context.Context, io.Reader) error
}

func joinedArtworkCalls() []artworkCall {
	return []artworkCall{
		{"InspectJoined", func(ctx context.Context, reader io.Reader) error {
			_, err := artwork.InspectJoined(ctx, reader)
			return err
		}},
		{"RenderJoined", func(ctx context.Context, reader io.Reader) error {
			_, err := artwork.RenderJoined(ctx, reader, artwork.Options{})
			return err
		}},
	}
}

func asynchronousArtworkCalls() []artworkCall {
	return []artworkCall{
		{"InspectContext", func(ctx context.Context, reader io.Reader) error {
			_, err := artwork.InspectContext(ctx, reader)
			return err
		}},
		{"Render", func(ctx context.Context, reader io.Reader) error {
			_, err := artwork.Render(ctx, reader, artwork.Options{})
			return err
		}},
	}
}

func TestJoinedArtworkCancellationRetainsSharedSlotsUntilReadReturns(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(12, 8))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		cancel()
		unblock()
		awaitArtworkDecodeSlots(t, data)
	})
	finished := make(chan error, 2)
	readers := make([]*joinedBlockingReadCloser, 0, 2)
	for _, call := range joinedArtworkCalls() {
		reader := newJoinedBlockingReadCloser(data, release)
		readers = append(readers, reader)
		go func() { finished <- call.run(ctx, reader) }()
	}
	for _, reader := range readers {
		select {
		case <-reader.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("joined operations did not enter both shared decode slots")
		}
	}
	cancel()
	select {
	case err := <-finished:
		t.Fatalf("joined operation returned before its blocked read ended: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	for _, call := range append(joinedArtworkCalls(), asynchronousArtworkCalls()...) {
		t.Run(call.name+" queued", func(t *testing.T) {
			queuedCtx, cancelQueued := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancelQueued()
			reader := &countingReader{source: bytes.NewReader(data)}
			queuedFinished := make(chan error, 1)
			go func() { queuedFinished <- call.run(queuedCtx, reader) }()
			select {
			case err := <-queuedFinished:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("queued operation returned %v, want context.DeadlineExceeded", err)
				}
				if reader.reads.Load() != 0 {
					t.Fatal("queued operation read input while two canceled joined reads retained their slots")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("queued operation waited for blocked reads instead of returning cancellation")
			}
		})
	}
	select {
	case err := <-finished:
		t.Fatalf("joined operation returned while its read was still blocked: %v", err)
	default:
	}
	unblock()
	for range readers {
		select {
		case err := <-finished:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("joined operation returned %v, want context.Canceled", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("joined operation did not finish after its read was released")
		}
	}
	for _, reader := range readers {
		select {
		case <-reader.readExited:
		default:
			t.Error("joined operation returned before its reader exited")
		}
		if reader.closes.Load() != 0 {
			t.Error("joined operation closed its caller-owned reader")
		}
	}
}

func TestAsynchronousArtworkCancellationStillReturnsBeforeReadCompletes(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(12, 8))
	for _, call := range asynchronousArtworkCalls() {
		t.Run(call.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(func() {
				cancel()
				unblock()
				awaitArtworkDecodeSlots(t, data)
			})
			reader := newJoinedBlockingReadCloser(data, release)
			finished := make(chan error, 1)
			go func() { finished <- call.run(ctx, reader) }()
			select {
			case <-reader.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("asynchronous operation did not start reading its input")
			}
			cancel()
			select {
			case err := <-finished:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("asynchronous operation returned %v, want context.Canceled", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("asynchronous cancellation waited for the blocked reader")
			}
			if reader.closes.Load() != 0 {
				t.Fatal("asynchronous cancellation closed its caller-owned reader")
			}
			select {
			case <-reader.readExited:
				t.Fatal("asynchronous reader exited before it was released")
			default:
			}
			unblock()
			select {
			case <-reader.readExited:
			case <-time.After(3 * time.Second):
				t.Fatal("asynchronous reader did not exit after release")
			}
		})
	}
}

func TestJoinedArtworkQueuesBehindCanceledAsynchronousReaders(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(12, 8))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		cancel()
		unblock()
		awaitArtworkDecodeSlots(t, data)
	})
	finished := make(chan error, 2)
	readers := make([]*joinedBlockingReadCloser, 0, 2)
	for _, call := range asynchronousArtworkCalls() {
		reader := newJoinedBlockingReadCloser(data, release)
		readers = append(readers, reader)
		go func() { finished <- call.run(ctx, reader) }()
	}
	for _, reader := range readers {
		select {
		case <-reader.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("asynchronous operations did not occupy both shared decode slots")
		}
	}
	cancel()
	for range readers {
		select {
		case err := <-finished:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("asynchronous operation returned %v, want context.Canceled", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("asynchronous operation waited for its blocked reader")
		}
	}
	for _, call := range joinedArtworkCalls() {
		t.Run(call.name, func(t *testing.T) {
			queuedCtx, cancelQueued := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancelQueued()
			reader := &countingReader{source: bytes.NewReader(data)}
			queuedFinished := make(chan error, 1)
			go func() { queuedFinished <- call.run(queuedCtx, reader) }()
			select {
			case err := <-queuedFinished:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("joined operation queued behind canceled workers returned %v, want context.DeadlineExceeded", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("queued joined operation ignored cancellation")
			}
			if reader.reads.Load() != 0 {
				t.Fatal("joined operation acquired a slot before a canceled asynchronous reader exited")
			}
		})
	}
	unblock()
	for _, reader := range readers {
		select {
		case <-reader.readExited:
		case <-time.After(3 * time.Second):
			t.Fatal("asynchronous reader did not exit after release")
		}
	}
}

func TestJoinedArtworkCancellationTakesPriorityAndReturnsZeroResult(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(12, 8))
	for _, test := range []struct {
		name   string
		source func() io.Reader
	}{
		{"successful read", func() io.Reader { return bytes.NewReader(data) }},
		{"failed read", func() io.Reader { return errorReader{err: errors.New("source failed after cancellation")} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			info, err := artwork.InspectJoined(ctx, &cancelReader{source: test.source(), cancel: cancel})
			if !errors.Is(err, context.Canceled) || info != (artwork.Info{}) {
				t.Errorf("canceled InspectJoined returned %+v, %v; want zero metadata and context.Canceled", info, err)
			}
			ctx, cancel = context.WithCancel(context.Background())
			defer cancel()
			result, err := artwork.RenderJoined(ctx, &cancelReader{source: test.source(), cancel: cancel}, artwork.Options{})
			if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(result, artwork.Result{}) {
				t.Errorf("canceled RenderJoined returned %+v, %v; want zero result and context.Canceled", result, err)
			}
		})
	}
}

func TestJoinedArtworkPreCanceledContextDoesNotRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, call := range joinedArtworkCalls() {
		t.Run(call.name, func(t *testing.T) {
			reader := &countingReader{source: bytes.NewReader([]byte("unread input"))}
			if err := call.run(ctx, reader); !errors.Is(err, context.Canceled) {
				t.Fatalf("pre-canceled joined operation returned %v, want context.Canceled", err)
			}
			if reader.reads.Load() != 0 {
				t.Fatal("pre-canceled joined operation read input")
			}
		})
	}
}

func TestJoinedArtworkPreservesValidationAndRenderResults(t *testing.T) {
	for _, format := range []string{"jpeg", "png", "gif"} {
		t.Run(format, func(t *testing.T) {
			data := encodeImage(t, format, sampleImage(12, 8))
			wantInfo, err := artwork.Inspect(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			info, err := artwork.InspectJoined(context.Background(), bytes.NewReader(data))
			if err != nil || info != wantInfo {
				t.Fatalf("joined metadata = %+v, %v; want %+v", info, err, wantInfo)
			}
			for _, options := range []artwork.Options{{}, {Width: 6}, {Format: "jpeg", Quality: 50}, {Format: "png", Height: 4}} {
				want := renderImage(t, data, options)
				result, err := artwork.RenderJoined(context.Background(), bytes.NewReader(data), options)
				if err != nil || !reflect.DeepEqual(result, want) {
					t.Fatalf("joined result for %+v = %+v, %v; want %+v", options, result, err, want)
				}
			}
		})
	}
	for _, call := range joinedArtworkCalls() {
		if err := call.run(context.Background(), bytes.NewReader(pngHeader(16385, 1))); !errors.Is(err, artwork.ErrLimitExceeded) {
			t.Errorf("%s accepted excessive dimensions: %v", call.name, err)
		}
		want := errors.New("joined source read failed")
		if err := call.run(context.Background(), errorReader{err: want}); !errors.Is(err, want) {
			t.Errorf("%s lost its reader failure: %v", call.name, err)
		}
		brokenGIF := repeatedGIF(t, 2, 2, 2)
		if err := call.run(context.Background(), bytes.NewReader(brokenGIF[:len(brokenGIF)-3])); err == nil {
			t.Errorf("%s accepted a GIF with a damaged second frame", call.name)
		}
	}
	reader := &countingReader{source: bytes.NewReader([]byte("unread input"))}
	if _, err := artwork.RenderJoined(context.Background(), reader, artwork.Options{Width: 4097}); !errors.Is(err, artwork.ErrInvalidOptions) {
		t.Errorf("RenderJoined accepted invalid options: %v", err)
	}
	if reader.reads.Load() != 0 {
		t.Error("RenderJoined read input before rejecting invalid options")
	}
}

// Admission of two blocked joined reads proves that all previous workers have
// released their shared slots. A reader's return alone does not prove this for
// the asynchronous APIs, whose worker still needs to observe cancellation.
func awaitArtworkDecodeSlots(t *testing.T, data []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	finished := make(chan error, 2)
	defer func() {
		unblock()
		for range 2 {
			select {
			case <-finished:
			case <-time.After(3 * time.Second):
				t.Error("decode slot cleanup did not finish after release")
			}
		}
	}()
	readers := make([]*blockingReader, 0, 2)
	for range 2 {
		reader := &blockingReader{source: bytes.NewReader(data), entered: make(chan struct{}), release: release}
		readers = append(readers, reader)
		go func() {
			_, err := artwork.InspectJoined(ctx, reader)
			finished <- err
		}()
	}
	for _, reader := range readers {
		select {
		case <-reader.entered:
		case <-ctx.Done():
			t.Error("previous artwork workers did not release both decode slots")
			return
		}
	}
}

type joinedBlockingReadCloser struct {
	*blockingReader
	readExited chan struct{}
	exitOnce   sync.Once
	closes     atomic.Int32
}

func newJoinedBlockingReadCloser(data []byte, release <-chan struct{}) *joinedBlockingReadCloser {
	return &joinedBlockingReadCloser{
		blockingReader: &blockingReader{source: bytes.NewReader(data), entered: make(chan struct{}), release: release},
		readExited:     make(chan struct{}),
	}
}

func (reader *joinedBlockingReadCloser) Read(p []byte) (int, error) {
	n, err := reader.blockingReader.Read(p)
	reader.exitOnce.Do(func() { close(reader.readExited) })
	return n, err
}

func (reader *joinedBlockingReadCloser) Close() error {
	reader.closes.Add(1)
	return nil
}
