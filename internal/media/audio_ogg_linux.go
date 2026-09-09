package media

import (
	"context"
	"fmt"
	"io"
	"os"
	"syscall"
)

var oggEvidenceWorkers = make(chan struct{}, 2)

// A duplicate descriptor keeps a potentially blocked storage read independent
// of the caller's lifetime and offset. Cancelled workers retain their bounded
// slot until their own descriptor has been closed; no result channel owns it.
func probeOggAudioEvidence(ctx context.Context, source *os.File, streams []Stream) (*oggAudioEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case oggEvidenceWorkers <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	connection, err := source.SyscallConn()
	if err != nil {
		<-oggEvidenceWorkers
		return nil, err
	}
	var fd uintptr
	var duplicateErr error
	err = connection.Control(func(original uintptr) {
		var errno syscall.Errno
		fd, _, errno = syscall.Syscall(syscall.SYS_FCNTL, original, syscall.F_DUPFD_CLOEXEC, 0)
		if errno != 0 {
			duplicateErr = errno
		}
	})
	if err != nil || duplicateErr != nil {
		<-oggEvidenceWorkers
		if err != nil {
			return nil, fmt.Errorf("duplicate Ogg descriptor: %w", err)
		}
		return nil, fmt.Errorf("duplicate Ogg descriptor: %w", duplicateErr)
	}
	file := os.NewFile(fd, "Ogg audio evidence")
	type result struct {
		evidence *oggAudioEvidence
		err      error
	}
	results := make(chan result)
	go func() {
		defer func() { _ = file.Close(); <-oggEvidenceWorkers }()
		evidence, err := readOggAudioEvidence(ctx, io.NewSectionReader(file, 0, 1<<63-1), streams)
		select {
		case results <- result{evidence: evidence, err: err}:
		case <-ctx.Done():
		}
	}()
	select {
	case value := <-results:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return value.evidence, value.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
