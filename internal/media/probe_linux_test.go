package media

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
)

func TestProbeRejectsFIFOWithoutWaitingForWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unexpected-fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := (Prober{FFprobePath: "/missing/ffprobe"}).Probe(context.Background(), path)
	if err == nil || err.Error() != "media input is not a regular file" {
		t.Fatalf("FIFO was not rejected: %v", err)
	}
}
