//go:build linux

package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

// A real file exercises io.Copy's optional-interface dispatch. Anonymous
// bytes.Buffer embedding would promote ReadFrom and bypass the bounded Write.
func TestGeneratedAVSourceBufferActualFileCopyCannotBypassLimit(t *testing.T) {
	name := filepath.Join(t.TempDir(), "bounded-source-output")
	if err := os.WriteFile(name, bytes.Repeat([]byte{0x61}, 256<<10), 0600); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := &hlsGeneratedAVSourceCommandBuffer{cancel: cancel}
	if _, fast := any(output).(io.ReaderFrom); fast {
		t.Fatal("source output exposes a fast path that bypasses its byte fence")
	}
	if _, fast := any(output).(io.WriterTo); fast {
		t.Fatal("source output exposes the underlying unbounded buffer")
	}
	written, copyErr := io.Copy(output, input)
	if !errors.Is(copyErr, transcode.ErrTimelineLimit) || written > 64<<10 || output.Len() > 64<<10 || ctx.Err() == nil {
		t.Fatal("actual file copy exceeded its source-output budget or failed to cancel")
	}
	before := output.Len()
	if _, err := output.Write([]byte("late")); !errors.Is(err, transcode.ErrTimelineLimit) || output.Len() != before {
		t.Fatal("a later write grew source output after its first budget failure")
	}
}
