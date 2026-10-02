//go:build linux

package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
)

func generatedAVCaptureEffectiveJSON(ctx context.Context, data []byte, inputs []*os.File) (failure error) {
	if ctx == nil {
		return ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := generatedAVEffectiveCaptureFile(ctx, data)
	if err != nil {
		if file != nil {
			return errors.Join(err, file.Close())
		}
		return err
	}
	if file == nil {
		return nil
	}
	defer func() { failure = errors.Join(failure, file.Close()) }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != 0 {
		return ErrInvalidInput
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) {
		return ErrInvalidInput
	}
	offset, err := file.Seek(0, io.SeekCurrent)
	if err != nil || offset != 0 {
		return ErrInvalidInput
	}
	for _, input := range inputs {
		if input == nil {
			return ErrInvalidInput
		}
		prior, err := input.Stat()
		if err != nil || os.SameFile(info, prior) {
			return ErrInvalidInput
		}
	}
	// data came from the named bounded buffer after process/copier joins.
	// Direct Write cannot select a ReaderFrom fast path or enlarge that cap.
	written, err := file.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
