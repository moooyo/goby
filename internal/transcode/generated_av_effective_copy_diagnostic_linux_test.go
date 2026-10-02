//go:build linux

package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
)

// A real pipe file exercises os.File.WriteTo and io.Copy's optional fast paths,
// rather than calling the destination's Write method directly. Its tiny payload
// fits in the pipe before copying, so this fixture owns no blocking goroutine.
func generatedAVDiagnosticCopyFromFD(t *testing.T, destination io.Writer, data []byte) (int64, error) {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	if _, err := write.Write(data); err != nil {
		_ = write.Close()
		t.Fatal(err)
	}
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	return io.Copy(destination, read)
}

func TestGeneratedAVDiagnosticBufferCopyFromFDEnforcesBudgetAndCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	buffer := &generatedAVDiagnosticBuffer{limit: 8, cancel: cancel}
	if n, err := generatedAVDiagnosticCopyFromFD(t, buffer, []byte("abc")); err != nil || n != 3 || buffer.Len() != 3 || ctx.Err() != nil {
		t.Fatal("within-budget real FD copy did not finish")
	}
	if _, err := generatedAVDiagnosticCopyFromFD(t, buffer, []byte("defghi")); !errors.Is(err, ErrTimelineLimit) {
		t.Fatal("io.Copy bypassed the actual byte limit")
	}
	if buffer.Len() > 8 || string(buffer.Bytes()) != "abc" || !errors.Is(buffer.err, ErrTimelineLimit) {
		t.Fatal("over-budget copy retained bytes outside the bounded prefix")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("real copy failure did not promptly cancel")
	}
	if _, ok := any(buffer).(io.ReaderFrom); ok {
		t.Fatal("bounded writer exposes a bypassing ReaderFrom")
	}
	if _, ok := any(buffer).(io.WriterTo); ok {
		t.Fatal("bounded writer exposes the private backing buffer")
	}
}

func TestGeneratedAVDiagnosticBufferCopyFromFDFailNonempty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	buffer := &generatedAVDiagnosticBuffer{limit: 64 << 10, cancel: cancel, failNonempty: true}
	if _, err := generatedAVDiagnosticCopyFromFD(t, buffer, []byte("decoder diagnostic")); !errors.Is(err, ErrTimelineProbe) {
		t.Fatal("io.Copy silently accepted error-level diagnostics")
	}
	if buffer.Len() != 0 || !errors.Is(buffer.err, ErrTimelineProbe) {
		t.Fatal("diagnostics became accepted bounded output")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("nonempty diagnostic did not promptly cancel")
	}
}
