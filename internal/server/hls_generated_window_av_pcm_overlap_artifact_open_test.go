//go:build linux

package server

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/transcode"
)

func TestGeneratedAVPCMArtifactOverlapRejectsFIFOWithoutBlockingOrRetainingFD(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "source.pcm")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	fifo, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		value hlsGeneratedAVPCMOverlapOwned
		err   error
	}
	result := make(chan outcome, 1)
	go func() {
		value, err := hlsGeneratedAVPCMOverlapOpen(directory, "source.pcm", 24<<20)
		result <- outcome{value: value, err: err}
	}()
	var got outcome
	select {
	case got = <-result:
	case <-time.After(2 * time.Second):
		// If a regression opens the FIFO without NONBLOCK, release the pending
		// reader and join it before failing, rather than leaving a blocked task.
		writer, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if err == nil {
			_ = syscall.Close(writer)
		}
		select {
		case late := <-result:
			if late.value.file != nil {
				_ = late.value.file.Close()
			}
		case <-time.After(2 * time.Second):
			t.Fatal("FIFO regression failed to join after releasing the reader")
		}
		t.Fatal("artifact open blocked before rejecting a nonregular FIFO")
	}
	if !errors.Is(got.err, transcode.ErrInvalidInput) || got.value.file != nil {
		if got.value.file != nil {
			_ = got.value.file.Close()
		}
		t.Fatal("FIFO supplied an artifact descriptor instead of rejection")
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := os.Stat(filepath.Join("/proc/self/fd", entry.Name()))
		if err == nil && os.SameFile(fifo, info) {
			t.Fatal("rejected FIFO descriptor remained held after the helper returned")
		}
	}
}
