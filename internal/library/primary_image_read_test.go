package library

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/primaryio"
)

func TestPrimaryImageContentBoundsReadsPreservesEOFAndJoinsDescriptor(t *testing.T) {
	operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
	file, err := os.CreateTemp(t.TempDir(), "primary-image-*")
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("bounded artwork source"), 4096)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	content := newPrimaryImageContent(context.Background(), operation, "root-0", file, int64(len(data)))
	t.Cleanup(func() { _ = content.Close() })
	if _, ok := any(content).(io.ReaderAt); ok {
		t.Fatal("artwork exposed ReaderAt")
	}
	if _, ok := any(content).(io.WriterTo); ok {
		t.Fatal("artwork exposed WriterTo")
	}
	buffer := make([]byte, 2*primaryio.MaxReadChunkBytes)
	n, err := content.Read(buffer)
	if err != nil || n != primaryio.MaxReadChunkBytes || !bytes.Equal(buffer[:n], data[:n]) {
		t.Fatalf("bounded source read: n=%d error=%v", n, err)
	}
	if stats := governor.Stats(); stats.Active != 0 || stats.Queued != 0 {
		t.Fatalf("read retained an actual phase between calls: %+v", stats)
	}
	if stats := owners.Stats(); stats.RegisteredOwners != 1 {
		t.Fatalf("idle descriptor lost its retained owner: %+v", stats)
	}
	rest, err := io.ReadAll(content)
	if err != nil || !bytes.Equal(rest, data[n:]) {
		t.Fatalf("normal EOF did not terminate ReadAll: bytes=%d error=%v", len(rest), err)
	}
	if n, err := content.Read(buffer); n != 0 || err != io.EOF {
		t.Fatalf("EOF was wrapped or changed: n=%d error=%v", n, err)
	}
	if err := content.Close(); err != nil {
		t.Fatal(err)
	}
	primaryRootIOTestWait(t, finished, "artwork descriptor and registration retirement")
	if err := file.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("artwork retired without closing its exact descriptor: %v", err)
	}
	if stats := owners.Stats(); stats.RegisteredOwners != 0 {
		t.Fatalf("artwork descriptor registration remained: %+v", stats)
	}
}

type primaryArtworkBlockingReader struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (reader *primaryArtworkBlockingReader) Read([]byte) (int, error) {
	reader.once.Do(func() { close(reader.started) })
	<-reader.release
	return 0, io.EOF
}

func TestPrimaryArtworkJoinedWorkerRetainsPhaseAfterCancellationAndHandleClose(t *testing.T) {
	operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
	reader := &primaryArtworkBlockingReader{started: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(reader.release) }) }
	t.Cleanup(release)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	returned := make(chan error, 1)
	go func() {
		returned <- operation.Run(ctx, "root-0", primaryio.Background, func(work context.Context) error {
			_, err := artwork.InspectJoined(work, reader)
			return err
		})
	}()
	primaryRootIOTestWait(t, reader.started, "actual artwork reader entry")
	cancel()
	if err := operation.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-returned:
		t.Fatalf("cancellation abandoned the actual artwork worker: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	select {
	case <-finished:
		t.Fatal("handle close retired the Store before its actual reader")
	default:
	}
	if stats := governor.Stats(); stats.Active != 1 || stats.Background != 1 {
		t.Fatalf("canceled artwork released actual root capacity: %+v", stats)
	}
	if stats := owners.Stats(); stats.RegisteredOwners != 1 {
		t.Fatalf("canceled artwork released retained ownership: %+v", stats)
	}
	release()
	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("joined canceled artwork error=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("released artwork did not join")
	}
	primaryRootIOTestWait(t, finished, "actual artwork worker and phase retirement")
	if stats := governor.Stats(); stats.Active != 0 || stats.Queued != 0 {
		t.Fatalf("joined artwork did not retire actual capacity: %+v", stats)
	}
}
