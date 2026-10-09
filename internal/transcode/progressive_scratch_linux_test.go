//go:build linux

package transcode

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func progressiveScratchID3(size, declared int) []byte {
	prefix := make([]byte, size)
	copy(prefix, []byte{'I', 'D', '3', 4, 0, 0})
	for index := range 4 {
		prefix[6+index] = byte(declared >> (7 * (3 - index)) & 127)
	}
	return prefix
}

func newProgressiveScratchObserver(t *testing.T, prefix []byte) (*progressiveObserver, string) {
	t.Helper()
	directory := t.TempDir()
	observer, err := newProgressiveObserver(directory, nil, Plan{OutputMode: "progressive", Container: "mp3", AudioCodec: "mp3"}, nil, func() {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = observer.file.Close() })
	if _, err := observer.file.Write(prefix); err != nil {
		t.Fatal(err)
	}
	return observer, filepath.Join(directory, "stream.bin")
}

func TestProgressiveObserverScratchReusesStorageAndReadsFreshBytes(t *testing.T) {
	observer, path := newProgressiveScratchObserver(t, progressiveScratchID3(1024, 4096))
	if err := observer.inspect(false); err != nil || observer.last.Ready || len(observer.scratch) != 1024 {
		t.Fatalf("initial incomplete prefix: %v", err)
	}
	first := &observer.scratch[0]
	if err := observer.inspect(false); err != nil || &observer.scratch[0] != first {
		t.Fatalf("unchanged incomplete prefix did not reuse its storage: %v", err)
	}
	if _, err := observer.file.Write(make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	if err := observer.inspect(false); err != nil || len(observer.scratch) != 1124 || cap(observer.scratch) > MaxProgressivePrefixBytes {
		t.Fatalf("growing incomplete prefix exceeded its bounded storage: %v", err)
	}
	mutator, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutator.WriteAt([]byte("BAD"), 0); err != nil {
		_ = mutator.Close()
		t.Fatal(err)
	}
	if err := mutator.Close(); err != nil {
		t.Fatal(err)
	}
	if err := observer.inspect(false); !errors.Is(err, ErrInvalidProgressiveStream) || observer.scratch != nil {
		t.Fatalf("same-size corruption reused an old proof or retained failed scratch: %v", err)
	}
}

func TestProgressiveObserverScratchReleasesOnReadiness(t *testing.T) {
	observer, _ := newProgressiveScratchObserver(t, progressiveScratchID3(13, 3))
	if err := observer.inspect(false); err != nil || observer.scratch == nil || observer.last.Ready {
		t.Fatalf("metadata-only prefix established readiness: %v", err)
	}
	if _, err := observer.file.Write(progressiveReadyMP3([4]byte{0xff, 0xfb, 0x90, 0}, 417)); err != nil {
		t.Fatal(err)
	}
	if err := observer.inspect(false); err != nil || !observer.last.Ready || observer.scratch != nil {
		t.Fatalf("verified readiness did not release startup scratch: %v", err)
	}
}

func TestProgressiveObserverScratchRejectsPrefixCap(t *testing.T) {
	prefix := progressiveScratchID3(MaxProgressivePrefixBytes-1, MaxProgressivePrefixBytes-14)
	copy(prefix[MaxProgressivePrefixBytes-4:], []byte{0xff, 0xfb, 0x90})
	observer, _ := newProgressiveScratchObserver(t, prefix)
	if err := observer.inspect(false); err != nil || observer.last.Ready || cap(observer.scratch) > MaxProgressivePrefixBytes {
		t.Fatalf("bounded incomplete prefix: %v", err)
	}
	if _, err := observer.file.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	if err := observer.inspect(false); !errors.Is(err, ErrInvalidProgressiveStream) || observer.scratch != nil {
		t.Fatalf("exhausted prefix retained scratch or escaped rejection: %v", err)
	}
}

func TestProgressiveObserverScratchReleasesOnInspectionFailure(t *testing.T) {
	observer, _ := newProgressiveScratchObserver(t, progressiveScratchID3(1024, 4096))
	if err := observer.inspect(false); err != nil {
		t.Fatal(err)
	}
	if err := observer.file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := observer.inspect(false); !errors.Is(err, os.ErrClosed) || observer.scratch != nil {
		t.Fatalf("failed descriptor inspection retained scratch: %v", err)
	}
}

func TestProgressiveObserverScratchReleasesOnJoinedFinish(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancelled", true: "incomplete"}[success], func(t *testing.T) {
			observer, _ := newProgressiveScratchObserver(t, progressiveScratchID3(1024, 4096))
			if err := observer.inspect(false); err != nil {
				t.Fatal(err)
			}
			observer.start()
			err := observer.finish(success)
			if success && !errors.Is(err, ErrInvalidProgressiveStream) || !success && err != nil || observer.scratch != nil {
				t.Fatalf("joined finish retained scratch or changed its outcome: %v", err)
			}
		})
	}
}

func TestProgressiveObserverScratchReleasesWhenTickerFailsBeforeFinish(t *testing.T) {
	observer, path := newProgressiveScratchObserver(t, progressiveScratchID3(1024, 4096))
	if err := observer.inspect(false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, 1024), 0600); err != nil {
		t.Fatal(err)
	}
	observer.start()
	defer observer.finish(false)
	select {
	case <-observer.done:
	case <-time.After(3 * time.Second):
		t.Fatal("invalid output did not stop the inspection owner")
	}
	if !errors.Is(observer.err, ErrInvalidProgressiveStream) || observer.scratch != nil {
		t.Fatal("an exited observer retained scratch while process retirement could still wait")
	}
}
